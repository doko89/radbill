package infrastructure

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/url"
	"os"
	"time"

	"github.com/go-ldap/ldap/v3"

	authapp "boilerplate/api/internal/auth/application"
	"boilerplate/api/internal/shared/config"
)

// LDAPAuthenticator implements application.LDAPAuthenticator with go-ldap.
// It dials per login (no pooling) with 10s timeouts and contacts the
// directory lazily — construction never touches the network.
type LDAPAuthenticator struct {
	cfg config.LDAPConfig
}

// NewLDAPAuthenticator builds an LDAPAuthenticator. No network I/O happens here.
func NewLDAPAuthenticator(cfg config.LDAPConfig) *LDAPAuthenticator {
	return &LDAPAuthenticator{cfg: cfg}
}

// Authenticate verifies directory credentials via bind. Unknown users and
// wrong passwords both map to application.ErrInvalidCredentials.
func (a *LDAPAuthenticator) Authenticate(ctx context.Context, username, password string) (authapp.LDAPIdentity, error) {
	var none authapp.LDAPIdentity
	if username == "" || password == "" {
		// Empty passwords must never reach bind: some servers accept them
		// as "unauthenticated bind" success.
		return none, authapp.ErrInvalidCredentials
	}
	tlsCfg, err := a.tlsConfig()
	if err != nil {
		return none, err
	}
	conn, err := dialLDAP(ctx, a.cfg.URL, tlsCfg, a.cfg.StartTLS)
	if err != nil {
		return none, fmt.Errorf("ldap dial: %w", err)
	}
	defer conn.Close()
	conn.SetTimeout(10 * time.Second)

	if a.cfg.UserDNTemplate != "" && a.cfg.BindDN == "" {
		return a.directBind(conn, username, password)
	}
	return a.searchBind(conn, username, password)
}

// directBind binds a templated DN. Usernames are charset-restricted because
// DN templates cannot be escaped safely; prefer search mode.
func (a *LDAPAuthenticator) directBind(conn *ldap.Conn, username, password string) (authapp.LDAPIdentity, error) {
	var none authapp.LDAPIdentity
	if !safeDNUsername(username) {
		return none, authapp.ErrInvalidCredentials
	}
	if err := conn.Bind(fmt.Sprintf(a.cfg.UserDNTemplate, username), password); err != nil {
		return none, authapp.ErrInvalidCredentials
	}
	return authapp.LDAPIdentity{Username: username}, nil
}

// searchBind finds the user DN with the service account, then binds as the user.
func (a *LDAPAuthenticator) searchBind(conn *ldap.Conn, username, password string) (authapp.LDAPIdentity, error) {
	var none authapp.LDAPIdentity
	if err := conn.Bind(a.cfg.BindDN, a.cfg.BindPassword); err != nil {
		return none, fmt.Errorf("ldap service bind: %w", err)
	}
	filter := fmt.Sprintf(a.cfg.UserFilter, ldap.EscapeFilter(username))
	req := ldap.NewSearchRequest(
		a.cfg.BaseDN, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases,
		2, 10, false, filter,
		[]string{a.cfg.MailAttr, a.cfg.NameAttr}, nil,
	)
	res, err := conn.Search(req)
	if err != nil {
		return none, fmt.Errorf("ldap search: %w", err)
	}
	if len(res.Entries) != 1 {
		return none, authapp.ErrInvalidCredentials
	}
	entry := res.Entries[0]
	if err := conn.Bind(entry.DN, password); err != nil {
		return none, authapp.ErrInvalidCredentials
	}
	return authapp.LDAPIdentity{
		Username: username,
		Email:    entry.GetAttributeValue(a.cfg.MailAttr),
		Name:     entry.GetAttributeValue(a.cfg.NameAttr),
	}, nil
}

func safeDNUsername(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for _, r := range s {
		isLower := r >= 'a' && r <= 'z'
		isDigit := r >= '0' && r <= '9'
		if !isLower && !isDigit && r != '.' && r != '_' && r != '-' {
			return false
		}
	}
	return true
}

func dialLDAP(ctx context.Context, rawURL string, tlsCfg *tls.Config, startTLS bool) (*ldap.Conn, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	addr := u.Host
	if _, _, err := net.SplitHostPort(addr); err != nil {
		port := "389"
		if u.Scheme == "ldaps" {
			port = "636"
		}
		addr = net.JoinHostPort(u.Hostname(), port)
	}
	switch u.Scheme {
	case "ldaps":
		tlsConn, err := tls.DialWithDialer(&net.Dialer{Timeout: 10 * time.Second}, "tcp", addr, tlsCfg)
		if err != nil {
			return nil, err
		}
		conn := ldap.NewConn(tlsConn, true)
		conn.Start()
		return conn, nil
	case "ldap":
		raw, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp", addr)
		if err != nil {
			return nil, err
		}
		conn := ldap.NewConn(raw, false)
		conn.Start()
		if startTLS {
			if err := conn.StartTLS(tlsCfg); err != nil {
				conn.Close()
				return nil, err
			}
		}
		return conn, nil
	default:
		return nil, fmt.Errorf("unsupported LDAP scheme %q (want ldap:// or ldaps://)", u.Scheme)
	}
}

func (a *LDAPAuthenticator) tlsConfig() (*tls.Config, error) {
	u, err := url.Parse(a.cfg.URL)
	if err != nil {
		return nil, err
	}
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: u.Hostname()}
	if a.cfg.CAFile == "" {
		return tlsCfg, nil
	}
	pem, err := os.ReadFile(a.cfg.CAFile)
	if err != nil {
		return nil, fmt.Errorf("read LDAP_CA_FILE: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("no certificates in LDAP_CA_FILE %s", a.cfg.CAFile)
	}
	tlsCfg.RootCAs = pool
	return tlsCfg, nil
}
