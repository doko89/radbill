// Package presentation adapts session use cases to HTTP.
package presentation

import (
	"strconv"

	"github.com/gofiber/fiber/v3"

	sessapp "boilerplate/api/internal/sessions/application"
)

// Handler serves /api/v1/admin/sessions endpoints.
type Handler struct {
	svc *sessapp.Service
}

// NewHandler builds a Handler.
func NewHandler(svc *sessapp.Service) *Handler {
	return &Handler{svc: svc}
}

// List returns online sessions by default (?online=true), or history when
// ?online=false. {data:[],total}.
func (h *Handler) List(c fiber.Ctx) error {
	limit, offset := pagination(c)
	online := c.Query("online", "true") != "false"
	var (
		items []sessapp.SessionDTO
		total int64
		err   error
	)
	if online {
		items, total, err = h.svc.ListOnline(c.Context(), limit, offset)
	} else {
		items, total, err = h.svc.ListHistory(c.Context(), limit, offset)
	}
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "internal error")
	}
	return c.JSON(fiber.Map{"data": items, "total": total})
}

// History returns session history, newest first.
func (h *Handler) History(c fiber.Ctx) error {
	limit, offset := pagination(c)
	items, total, err := h.svc.ListHistory(c.Context(), limit, offset)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "internal error")
	}
	return c.JSON(fiber.Map{"data": items, "total": total})
}

func writeError(c fiber.Ctx, status int, code, message string) error {
	return c.Status(status).JSON(fiber.Map{
		"error": fiber.Map{"code": code, "message": message},
	})
}

func pagination(c fiber.Ctx) (limit, offset int) {
	limit = queryInt(c, "limit", 50)
	offset = queryInt(c, "offset", 0)
	if limit < 1 {
		limit = 1
	}
	if limit > 200 {
		limit = 200
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func queryInt(c fiber.Ctx, key string, fallback int) int {
	raw := c.Query(key)
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return n
}
