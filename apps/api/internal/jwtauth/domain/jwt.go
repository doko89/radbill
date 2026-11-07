package domain

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"
)

// jwtHeader is the fixed HS256 header.
const jwtHeader = `{"alg":"HS256","typ":"JWT"}`

// jwtPayload is the wire shape of Claims (customer_id null when empty).
type jwtPayload struct {
	Sub        string  `json:"sub"`
	Role       string  `json:"role"`
	CustomerID *string `json:"customer_id"`
	Exp        int64   `json:"exp"`
	Iat        int64   `json:"iat"`
}

// SignAccessToken issues an HS256 JWT for claims with secret.
func SignAccessToken(secret []byte, c Claims) (string, error) {
	p := jwtPayload{
		Sub:  c.Subject,
		Role: c.Role,
		Exp:  c.ExpiresAt.Unix(),
		Iat:  c.IssuedAt.Unix(),
	}
	if c.CustomerID != "" {
		p.CustomerID = &c.CustomerID
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	head := base64.RawURLEncoding.EncodeToString([]byte(jwtHeader))
	body := base64.RawURLEncoding.EncodeToString(raw)
	payload := head + "." + body
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(payload))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return payload + "." + sig, nil
}

// VerifyAccessToken checks the HS256 signature and expiry, returning claims.
func VerifyAccessToken(secret []byte, token string, now time.Time) (Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return Claims{}, ErrTokenInvalid
	}
	payload := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(payload))
	want := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(parts[2]), []byte(want)) {
		return Claims{}, ErrTokenInvalid
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Claims{}, ErrTokenInvalid
	}
	var p jwtPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return Claims{}, ErrTokenInvalid
	}
	if p.Sub == "" || p.Role == "" || p.Exp <= 0 {
		return Claims{}, ErrTokenInvalid
	}
	exp := time.Unix(p.Exp, 0).UTC()
	if !now.Before(exp) {
		return Claims{}, ErrTokenInvalid
	}
	c := Claims{
		Subject:   p.Sub,
		Role:      p.Role,
		ExpiresAt: exp,
		IssuedAt:  time.Unix(p.Iat, 0).UTC(),
	}
	if p.CustomerID != nil {
		c.CustomerID = *p.CustomerID
	}
	return c, nil
}
