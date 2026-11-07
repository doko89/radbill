// Package config loads application configuration from the environment,
// with optional .env file support for local development.
package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Config is the full runtime configuration of the API.
type Config struct {
	Env           string // dev | prod | test
	Port          string
	DBDriver      string // sqlite | postgres
	SQLitePath    string
	DatabaseURL   string
	SessionSecret string
	SessionTTL    time.Duration
	AdminUser     string
	AdminPass     string
	OIDC          OIDCConfig
	LDAP          LDAPConfig
	UploadDir     string
	StorageDriver string // local | s3
	S3            S3Config
}

// OIDCConfig enables external login (Google or any OIDC provider).
// Disabled by default; set OIDC_ENABLED=true plus the client credentials
// to activate. RedirectURL must match the provider console entry, e.g.
// http://localhost:3000/api/auth/oidc/callback (dev) or the public URL.
type OIDCConfig struct {
	Enabled      bool
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	Label        string // button label, e.g. "Google"
}

// S3Config points avatar storage at an S3-compatible backend.
// Endpoint is host[:port] without scheme (e.g. "s3.amazonaws.com",
// "localhost:9000"); S3_USE_SSL=false is for local MinIO over plain HTTP.
type S3Config struct {
	Endpoint  string
	Region    string
	Bucket    string
	AccessKey string
	SecretKey string
	UseSSL    bool
}

// LDAPConfig enables directory login. Disabled by default.
// Two modes: search mode (LDAP_BIND_DN service account, recommended) or
// direct bind mode (LDAP_USER_DN_TEMPLATE with one %s, e.g.
// "uid=%s,ou=users,dc=company,dc=com"). Use ldaps:// URLs in production;
// LDAP_CA_FILE pins a private CA (PEM). The directory is contacted lazily
// on login, never at boot, so dev machines stay offline-safe.
type LDAPConfig struct {
	Enabled        bool
	URL            string
	StartTLS       bool
	CAFile         string
	BindDN         string
	BindPassword   string
	BaseDN         string
	UserFilter     string
	UserDNTemplate string
	MailAttr       string
	NameAttr       string
	Label          string // tab label, e.g. "Company Login"
}

// IsProd reports whether the app runs in production mode.
func (c Config) IsProd() bool { return c.Env == "prod" }

// Load reads .env (if found upward from cwd) then resolves Config.
// Explicit environment variables always win over .env values.
func Load() (Config, error) {
	loadDotEnv()

	cfg := Config{
		Env:           get("APP_ENV", "dev"),
		Port:          get("APP_PORT", "3000"),
		DBDriver:      get("DB_DRIVER", "sqlite"),
		SQLitePath:    get("SQLITE_PATH", "./data/app.db"),
		DatabaseURL:   get("DATABASE_URL", ""),
		SessionSecret: get("SESSION_SECRET", ""),
		AdminUser:     get("ADMIN_USER", "admin"),
		AdminPass:     get("ADMIN_PASS", ""),
		UploadDir:     get("UPLOAD_DIR", "./data/uploads"),
		StorageDriver: get("STORAGE_DRIVER", "local"),
		S3: S3Config{
			Endpoint:  get("S3_ENDPOINT", ""),
			Region:    get("S3_REGION", "us-east-1"),
			Bucket:    get("S3_BUCKET", ""),
			AccessKey: get("S3_ACCESS_KEY", ""),
			SecretKey: get("S3_SECRET_KEY", ""),
			UseSSL:    get("S3_USE_SSL", "true") == "true",
		},
	}

	ttlHours, err := strconv.Atoi(get("SESSION_TTL_HOURS", "12"))
	if err != nil || ttlHours <= 0 {
		return Config{}, errors.New("SESSION_TTL_HOURS must be a positive integer")
	}
	cfg.SessionTTL = time.Duration(ttlHours) * time.Hour

	switch cfg.DBDriver {
	case "sqlite", "postgres":
	default:
		return Config{}, fmt.Errorf("DB_DRIVER must be sqlite or postgres, got %q", cfg.DBDriver)
	}
	if cfg.DBDriver == "postgres" && cfg.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required when DB_DRIVER=postgres")
	}
	if cfg.SessionSecret == "" {
		if cfg.IsProd() {
			return Config{}, errors.New("SESSION_SECRET is required in prod")
		}
		cfg.SessionSecret = "dev-only-insecure-secret"
		fmt.Fprintln(os.Stderr, "WARN: using insecure dev SESSION_SECRET, set it in .env")
	}
	if cfg.AdminPass == "" {
		if cfg.IsProd() {
			return Config{}, errors.New("ADMIN_PASS is required in prod (seed admin)")
		}
		cfg.AdminPass = "admin123"
		fmt.Fprintln(os.Stderr, "WARN: using default dev admin password, set ADMIN_PASS in .env")
	}
	cfg.OIDC = OIDCConfig{
		Enabled:      get("OIDC_ENABLED", "false") == "true",
		Issuer:       get("OIDC_ISSUER", "https://accounts.google.com"),
		ClientID:     get("OIDC_CLIENT_ID", ""),
		ClientSecret: get("OIDC_CLIENT_SECRET", ""),
		RedirectURL:  get("OIDC_REDIRECT_URL", ""),
		Label:        get("OIDC_LABEL", "Google"),
	}
	if cfg.OIDC.Enabled {
		if cfg.OIDC.ClientID == "" || cfg.OIDC.ClientSecret == "" || cfg.OIDC.RedirectURL == "" {
			return Config{}, errors.New("OIDC_CLIENT_ID, OIDC_CLIENT_SECRET and OIDC_REDIRECT_URL are required when OIDC_ENABLED=true")
		}
	}
	cfg.LDAP = LDAPConfig{
		Enabled:        get("LDAP_ENABLED", "false") == "true",
		URL:            get("LDAP_URL", ""),
		StartTLS:       get("LDAP_STARTTLS", "false") == "true",
		CAFile:         get("LDAP_CA_FILE", ""),
		BindDN:         get("LDAP_BIND_DN", ""),
		BindPassword:   get("LDAP_BIND_PASSWORD", ""),
		BaseDN:         get("LDAP_BASE_DN", ""),
		UserFilter:     get("LDAP_USER_FILTER", "(uid=%s)"),
		UserDNTemplate: get("LDAP_USER_DN_TEMPLATE", ""),
		MailAttr:       get("LDAP_MAIL_ATTR", "mail"),
		NameAttr:       get("LDAP_NAME_ATTR", "displayName"),
		Label:          get("LDAP_LABEL", "Company Login"),
	}
	if cfg.LDAP.Enabled {
		if cfg.LDAP.URL == "" || cfg.LDAP.BaseDN == "" {
			return Config{}, errors.New("LDAP_URL and LDAP_BASE_DN are required when LDAP_ENABLED=true")
		}
		if cfg.LDAP.BindDN == "" && cfg.LDAP.UserDNTemplate == "" {
			return Config{}, errors.New("set LDAP_BIND_DN (search mode, recommended) or LDAP_USER_DN_TEMPLATE (direct bind mode)")
		}
		if !strings.Contains(cfg.LDAP.UserFilter, "%s") {
			return Config{}, errors.New("LDAP_USER_FILTER must contain one %s for the username")
		}
	}
	if cfg.StorageDriver != "local" && cfg.StorageDriver != "s3" {
		return Config{}, fmt.Errorf("STORAGE_DRIVER must be local or s3, got %q", cfg.StorageDriver)
	}
	if cfg.StorageDriver == "s3" && (cfg.S3.Endpoint == "" || cfg.S3.Bucket == "" || cfg.S3.AccessKey == "" || cfg.S3.SecretKey == "") {
		return Config{}, errors.New("S3_ENDPOINT, S3_BUCKET, S3_ACCESS_KEY and S3_SECRET_KEY are required when STORAGE_DRIVER=s3")
	}
	return cfg, nil
}

func get(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

// loadDotEnv parses the nearest .env walking upward from cwd.
// It never overrides variables already present in the environment.
func loadDotEnv() {
	path := findDotEnv()
	if path == "" {
		return
	}
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		_ = os.Setenv(key, unquote(strings.TrimSpace(val)))
	}
}

func findDotEnv() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for range 6 {
		candidate := filepath.Join(dir, ".env")
		if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
	return ""
}

func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
