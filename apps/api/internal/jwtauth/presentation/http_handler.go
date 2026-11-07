package presentation

import (
	"time"

	"github.com/gofiber/fiber/v3"

	authapp "boilerplate/api/internal/jwtauth/application"
)

// Handler serves /api/v1/auth endpoints.
type Handler struct {
	svc        *authapp.Service
	refreshTTL time.Duration
	secure     bool
	devExpose  bool
}

// NewHandler builds a Handler. secure marks cookies Secure (https only);
// devExpose includes one-time tokens in JSON responses (dev/test only,
// Fase 2 replaces this with WhatsApp/email delivery).
func NewHandler(svc *authapp.Service, refreshTTL time.Duration, secure, devExpose bool) *Handler {
	return &Handler{svc: svc, refreshTTL: refreshTTL, secure: secure, devExpose: devExpose}
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// Login verifies credentials, sets the refresh cookie, returns the session.
func (h *Handler) Login(c fiber.Ctx) error {
	var req loginRequest
	if err := c.Bind().JSON(&req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid", "invalid JSON body")
	}
	session, rawRefresh, err := h.svc.Login(c.Context(), req.Username, req.Password, c.IP())
	if err != nil {
		return mapAuthError(c, err)
	}
	SetRefreshCookie(c, rawRefresh, h.refreshTTL, h.secure)
	return writeItem(c, fiber.StatusOK, session)
}

// Refresh rotates the refresh cookie and returns a new session.
func (h *Handler) Refresh(c fiber.Ctx) error {
	session, rawRefresh, err := h.svc.Refresh(c.Context(), c.Cookies(RefreshCookieName))
	if err != nil {
		return mapAuthError(c, err)
	}
	SetRefreshCookie(c, rawRefresh, h.refreshTTL, h.secure)
	return writeItem(c, fiber.StatusOK, session)
}

// Logout revokes the refresh cookie. Always succeeds (idempotent).
func (h *Handler) Logout(c fiber.Ctx) error {
	_ = h.svc.Logout(c.Context(), c.Cookies(RefreshCookieName))
	ClearRefreshCookie(c, h.secure)
	return writeItem(c, fiber.StatusOK, fiber.Map{"ok": true})
}

// Me returns the authenticated user profile.
func (h *Handler) Me(c fiber.Ctx) error {
	claims, ok := ClaimsFrom(c)
	if !ok {
		return writeError(c, fiber.StatusUnauthorized, "unauthorized", "invalid or expired token")
	}
	user, err := h.svc.Me(c.Context(), claims.Subject)
	if err != nil {
		return mapAuthError(c, err)
	}
	return writeItem(c, fiber.StatusOK, fiber.Map{"user": user})
}

type resetRequestRequest struct {
	Username string `json:"username"`
}

// RequestPasswordReset creates a reset token. The response never reveals
// whether the username exists.
func (h *Handler) RequestPasswordReset(c fiber.Ctx) error {
	var req resetRequestRequest
	if err := c.Bind().JSON(&req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid", "invalid JSON body")
	}
	raw, err := h.svc.RequestPasswordReset(c.Context(), req.Username)
	if err != nil {
		return mapAuthError(c, err)
	}
	body := fiber.Map{"ok": true}
	if h.devExpose && raw != "" {
		body["reset_token"] = raw
	}
	return writeItem(c, fiber.StatusOK, body)
}

type resetRequest struct {
	Token       string `json:"token"`
	NewPassword string `json:"new_password"`
}

// ResetPassword consumes a reset token and sets a new password.
func (h *Handler) ResetPassword(c fiber.Ctx) error {
	var req resetRequest
	if err := c.Bind().JSON(&req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid", "invalid JSON body")
	}
	if err := h.svc.ResetPassword(c.Context(), req.Token, req.NewPassword); err != nil {
		return mapAuthError(c, err)
	}
	return writeItem(c, fiber.StatusOK, fiber.Map{"ok": true})
}
