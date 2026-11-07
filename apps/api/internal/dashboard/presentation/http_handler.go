// Package presentation adapts dashboard use cases to HTTP.
package presentation

import (
	"github.com/gofiber/fiber/v3"

	dashapp "boilerplate/api/internal/dashboard/application"
)

// Handler serves /api/v1/admin/dashboard and /api/v1/admin/metrics.
type Handler struct {
	svc *dashapp.Service
}

// NewHandler builds a Handler.
func NewHandler(svc *dashapp.Service) *Handler {
	return &Handler{svc: svc}
}

// Dashboard returns the admin aggregates: {data:{revenue_mtd,
// outstanding_total, overdue_count, accounts:{active,isolated,
// disabled}, online_sessions, recent_payments:[...]}} (E3 contract).
func (h *Handler) Dashboard(c fiber.Ctx) error {
	out, err := h.svc.Dashboard(c.Context())
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "internal error")
	}
	return c.JSON(fiber.Map{"data": out})
}

// Metrics returns process metrics: {data:{uptime_seconds, db_ok,
// scheduler:{jobs:[...]}, kriskris_pending, webhooks_failed_24h}}.
func (h *Handler) Metrics(c fiber.Ctx) error {
	out, err := h.svc.Metrics(c.Context())
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "internal error")
	}
	return c.JSON(fiber.Map{"data": out})
}

func writeError(c fiber.Ctx, status int, code, message string) error {
	return c.Status(status).JSON(fiber.Map{
		"error": fiber.Map{"code": code, "message": message},
	})
}
