package presentation

import (
	"github.com/gofiber/fiber/v3"
)

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

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
	return writeItem(c, fiber.StatusOK, fiber.Map{"ok": true})
}
