package infrastructure

import (
	"context"
	"errors"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	authapp "boilerplate/api/internal/auth/application"
)

// OIDCProvider implements application.OIDCProvider with go-oidc:
// discovery, authorization URL, code exchange and ID token verification.
type OIDCProvider struct {
	issuer   string
	oauth    *oauth2.Config
	verifier *oidc.IDTokenVerifier
}

// NewOIDCProvider discovers the issuer and builds the provider.
// It fails fast so a misconfigured issuer breaks boot, not first login.
func NewOIDCProvider(ctx context.Context, issuer, clientID, clientSecret, redirectURL string) (*OIDCProvider, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, err
	}
	return &OIDCProvider{
		issuer: issuer,
		oauth: &oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			RedirectURL:  redirectURL,
			Endpoint:     provider.Endpoint(),
			Scopes:       []string{oidc.ScopeOpenID, "email", "profile"},
		},
		verifier: provider.Verifier(&oidc.Config{ClientID: clientID}),
	}, nil
}

// Issuer returns the provider issuer URL.
func (p *OIDCProvider) Issuer() string { return p.issuer }

// AuthURL builds the provider authorization URL for the state/nonce pair.
func (p *OIDCProvider) AuthURL(state, nonce string) string {
	return p.oauth.AuthCodeURL(state, oidc.Nonce(nonce))
}

// Exchange trades the code for verified claims, checking the nonce.
func (p *OIDCProvider) Exchange(ctx context.Context, code, nonce string) (authapp.OIDCClaims, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	token, err := p.oauth.Exchange(ctx, code)
	if err != nil {
		return authapp.OIDCClaims{}, err
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok || raw == "" {
		return authapp.OIDCClaims{}, errors.New("oidc: token response has no id_token")
	}
	idToken, err := p.verifier.Verify(ctx, raw)
	if err != nil {
		return authapp.OIDCClaims{}, err
	}
	if idToken.Nonce != nonce {
		return authapp.OIDCClaims{}, errors.New("oidc: nonce mismatch")
	}
	var claims struct {
		Email   string `json:"email"`
		Name    string `json:"name"`
		Picture string `json:"picture"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return authapp.OIDCClaims{}, err
	}
	return authapp.OIDCClaims{Subject: idToken.Subject, Email: claims.Email, Name: claims.Name, Picture: claims.Picture}, nil
}
