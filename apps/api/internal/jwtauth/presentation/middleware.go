package presentation

import (
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"

	authapp "boilerplate/api/internal/jwtauth/application"
	authdomain "boilerplate/api/internal/jwtauth/domain"
)

// claimsKey is the Locals key where auth middleware stores Claims.
const claimsKey = "billing_claims"

// RefreshCookieName is the httpOnly refresh-token cookie.
const RefreshCookieName = "refresh_token"

// RefreshCookiePath scopes the cookie to auth endpoints only.
const RefreshCookiePath = "/api/v1/auth"

// Authenticate verifies the Bearer access token and stores Claims in Locals.
func Authenticate(svc *authapp.Service) fiber.Handler {
	return func(c fiber.Ctx) error {
		claims, err := verifyBearer(svc, c)
		if err != nil {
			return writeError(c, fiber.StatusUnauthorized, "unauthorized", "invalid or expired token")
		}
		c.Locals(claimsKey, claims)
		return c.Next()
	}
}

// Require verifies the Bearer token and enforces one RBAC permission.
// Handlers check permissions, never roles.
func Require(svc *authapp.Service, perm string) fiber.Handler {
	return func(c fiber.Ctx) error {
		claims, err := verifyBearer(svc, c)
		if err != nil {
			return writeError(c, fiber.StatusUnauthorized, "unauthorized", "invalid or expired token")
		}
		if !authdomain.HasPermission(claims.Role, perm) {
			return writeError(c, fiber.StatusForbidden, "forbidden", "missing permission "+perm)
		}
		c.Locals(claimsKey, claims)
		return c.Next()
	}
}

func verifyBearer(svc *authapp.Service, c fiber.Ctx) (authdomain.Claims, error) {
	h := c.Get("Authorization")
	token, ok := strings.CutPrefix(h, "Bearer ")
	if !ok || token == "" {
		return authdomain.Claims{}, authdomain.ErrTokenInvalid
	}
	return svc.VerifyAccess(token)
}

// ClaimsFrom returns the Claims stored by Authenticate/Require.
func ClaimsFrom(c fiber.Ctx) (authdomain.Claims, bool) {
	claims, ok := c.Locals(claimsKey).(authdomain.Claims)
	return claims, ok
}

// Subject returns the authenticated account id, or "" when unauthenticated.
func Subject(c fiber.Ctx) string {
	claims, ok := ClaimsFrom(c)
	if !ok {
		return ""
	}
	return claims.Subject
}

// CustomerID returns the customer id from JWT claims. Portal handlers must
// always scope user data by this value, never by URL/body input (anti-IDOR).
func CustomerID(c fiber.Ctx) string {
	claims, ok := ClaimsFrom(c)
	if !ok {
		return ""
	}
	return claims.CustomerID
}

// SetRefreshCookie writes the httpOnly refresh cookie (Secure in prod).
func SetRefreshCookie(c fiber.Ctx, raw string, ttl time.Duration, secure bool) {
	c.Cookie(&fiber.Cookie{
		Name:     RefreshCookieName,
		Value:    raw,
		Path:     RefreshCookiePath,
		HTTPOnly: true,
		Secure:   secure,
		SameSite: "Lax",
		Expires:  time.Now().Add(ttl),
	})
}

// ClearRefreshCookie removes the refresh cookie.
func ClearRefreshCookie(c fiber.Ctx, secure bool) {
	c.Cookie(&fiber.Cookie{
		Name:     RefreshCookieName,
		Value:    "",
		Path:     RefreshCookiePath,
		HTTPOnly: true,
		Secure:   secure,
		SameSite: "Lax",
		MaxAge:   -1,
	})
}
