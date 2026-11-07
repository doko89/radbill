// Package session issues and verifies HMAC-signed session tokens carried
// in an HttpOnly cookie. No server-side session storage needed.
package session

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
)

// CookieName is the session cookie. It must always be HttpOnly.
const CookieName = "session"

// ctxKey is the Locals key where AuthRequired stores the user id.
const ctxKey = "user_id"

var (
	// ErrInvalid is returned for malformed or forged tokens.
	ErrInvalid = errors.New("invalid session token")
	// ErrExpired is returned for tokens past their expiry.
	ErrExpired = errors.New("session token expired")
)

// Manager signs and verifies session tokens.
type Manager struct {
	secret []byte
	ttl    time.Duration
	secure bool
}

// New builds a Manager. secure=true marks cookies Secure (https only).
func New(secret string, ttl time.Duration, secure bool) *Manager {
	return &Manager{secret: []byte(secret), ttl: ttl, secure: secure}
}

// Issue signs a token for userID valid for the manager TTL.
func (m *Manager) Issue(userID string) string {
	exp := time.Now().Add(m.ttl).Unix()
	return m.issueWithExp(userID, exp)
}

func (m *Manager) issueWithExp(userID string, exp int64) string {
	uid := base64.RawURLEncoding.EncodeToString([]byte(userID))
	e := base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(exp, 10)))
	payload := "v1." + uid + "." + e
	mac := hmac.New(sha256.New, m.secret)
	mac.Write([]byte(payload))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return payload + "." + sig
}

// Verify checks the signature and expiry, returning the user id.
func (m *Manager) Verify(token string) (string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 4 || parts[0] != "v1" {
		return "", ErrInvalid
	}
	payload := strings.Join(parts[:3], ".")
	mac := hmac.New(sha256.New, m.secret)
	mac.Write([]byte(payload))
	want := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(parts[3]), []byte(want)) != 1 {
		return "", ErrInvalid
	}
	expBytes, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return "", ErrInvalid
	}
	exp, err := strconv.ParseInt(string(expBytes), 10, 64)
	if err != nil {
		return "", ErrInvalid
	}
	if time.Now().Unix() > exp {
		return "", ErrExpired
	}
	uidBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || len(uidBytes) == 0 {
		return "", ErrInvalid
	}
	return string(uidBytes), nil
}

// SetCookie writes the HttpOnly session cookie on login.
func (m *Manager) SetCookie(c fiber.Ctx, token string) {
	c.Cookie(&fiber.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		HTTPOnly: true,
		Secure:   m.secure,
		SameSite: "Lax",
		Expires:  time.Now().Add(m.ttl),
	})
}

// ClearCookie removes the session cookie on logout.
func (m *Manager) ClearCookie(c fiber.Ctx) {
	c.Cookie(&fiber.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		HTTPOnly: true,
		Secure:   m.secure,
		SameSite: "Lax",
		MaxAge:   -1,
	})
}

// AuthRequired rejects requests without a valid session cookie with 401,
// otherwise stores the user id in Locals for handlers.
func (m *Manager) AuthRequired(c fiber.Ctx) error {
	userID, err := m.Verify(c.Cookies(CookieName))
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}
	c.Locals(ctxKey, userID)
	return c.Next()
}

// UserID returns the authenticated user id stored by AuthRequired.
func UserID(c fiber.Ctx) string {
	id, _ := c.Locals(ctxKey).(string)
	return id
}
