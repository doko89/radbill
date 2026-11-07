package presentation

import (
	"log"

	"github.com/gofiber/fiber/v3"

	auditapp "boilerplate/api/internal/audit/application"
	auditdomain "boilerplate/api/internal/audit/domain"
)

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// SetAuditor installs the G4 audit sink (Fase 4, ADITIF).
func (h *Handler) SetAuditor(a *auditapp.Service) { h.audit = a }

// ChangePassword sets a new panel password for the authenticated account
// and revokes every refresh token (G2, ADITIF).
func (h *Handler) ChangePassword(c fiber.Ctx) error {
	claims, ok := ClaimsFrom(c)
	if !ok {
		return writeError(c, fiber.StatusUnauthorized, "unauthorized", "invalid or expired token")
	}
	var req changePasswordRequest
	if err := c.Bind().JSON(&req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid", "invalid JSON body")
	}
	if err := h.svc.ChangePassword(c.Context(), claims.Subject, req.CurrentPassword, req.NewPassword); err != nil {
		return mapAuthError(c, err)
	}
	h.auditPasswordChange(c, claims.Subject)
	return writeItem(c, fiber.StatusOK, fiber.Map{"ok": true})
}

// auditPasswordChange records auth.password_change best-effort:
// failures are logged, the request still succeeds.
func (h *Handler) auditPasswordChange(c fiber.Ctx, subject string) {
	if h.audit == nil {
		return
	}
	if err := h.audit.Log(c.Context(), auditapp.LogInput{
		ActorID: subject, Action: auditdomain.ActionAuthPasswordChange,
		TargetType: auditdomain.TargetAccount, TargetID: subject, IP: c.IP(),
	}); err != nil {
		log.Printf("audit: log %s failed: %v", auditdomain.ActionAuthPasswordChange, err)
	}
}
