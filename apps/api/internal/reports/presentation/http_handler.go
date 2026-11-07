// Package presentation adapts report use cases to HTTP (admin, JSON).
package presentation

import (
	"errors"
	"strconv"

	"github.com/gofiber/fiber/v3"

	reportsapp "boilerplate/api/internal/reports/application"
	reportsdomain "boilerplate/api/internal/reports/domain"
)

// Handler serves /api/v1/admin/reports/* endpoints.
type Handler struct {
	svc *reportsapp.Service
}

// NewHandler builds a Handler.
func NewHandler(svc *reportsapp.Service) *Handler {
	return &Handler{svc: svc}
}

// Revenue answers ?from&to&group_by=day|month:
// {data:{points:[{period,total}],total}}.
func (h *Handler) Revenue(c fiber.Ctx) error {
	out, err := h.svc.Revenue(c.Context(), c.Query("from"), c.Query("to"), c.Query("group_by"))
	if err != nil {
		return mapError(c, err)
	}
	return c.JSON(fiber.Map{"data": out})
}

// Usage answers ?from&to:
// {data:{points:[{date,bytes_in,bytes_out}],totals:{bytes_in,bytes_out}}}.
func (h *Handler) Usage(c fiber.Ctx) error {
	out, err := h.svc.Usage(c.Context(), c.Query("from"), c.Query("to"))
	if err != nil {
		return mapError(c, err)
	}
	return c.JSON(fiber.Map{"data": out})
}

// Receivables answers {data:{invoices:[...],total}}.
func (h *Handler) Receivables(c fiber.Ctx) error {
	out, err := h.svc.Receivables(c.Context())
	if err != nil {
		return mapError(c, err)
	}
	return c.JSON(fiber.Map{"data": out})
}

// Growth answers ?months=6: {data:{points:[{period,new,churn,active}]}}.
func (h *Handler) Growth(c fiber.Ctx) error {
	months := 0
	if raw := c.Query("months"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return writeError(c, fiber.StatusBadRequest, "invalid", "months must be an integer 1..24")
		}
		months = n
	}
	out, err := h.svc.Growth(c.Context(), months)
	if err != nil {
		return mapError(c, err)
	}
	return c.JSON(fiber.Map{"data": out})
}

func mapError(c fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, reportsdomain.ErrBadRange):
		return writeError(c, fiber.StatusBadRequest, "invalid", "from/to must be YYYY-MM-DD with from <= to")
	case errors.Is(err, reportsdomain.ErrBadGroupBy):
		return writeError(c, fiber.StatusBadRequest, "invalid", "group_by must be day|month")
	case errors.Is(err, reportsdomain.ErrBadMonths):
		return writeError(c, fiber.StatusBadRequest, "invalid", "months must be an integer 1..24")
	}
	return writeError(c, fiber.StatusInternalServerError, "internal", "internal error")
}

func writeError(c fiber.Ctx, status int, code, message string) error {
	return c.Status(status).JSON(fiber.Map{
		"error": fiber.Map{"code": code, "message": message},
	})
}
