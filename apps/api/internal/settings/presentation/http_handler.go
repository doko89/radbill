// Package presentation adapts settings use cases to HTTP.
package presentation

import (
	"errors"
	"strconv"

	"github.com/gofiber/fiber/v3"

	setapp "boilerplate/api/internal/settings/application"
	"boilerplate/api/internal/shared/apperr"
)

// Handler serves /api/v1/admin/settings endpoints.
type Handler struct {
	svc *setapp.Service
}

// NewHandler builds a Handler.
func NewHandler(svc *setapp.Service) *Handler {
	return &Handler{svc: svc}
}

// List returns all settings as {data:{key:value}} with secrets masked.
func (h *Handler) List(c fiber.Ctx) error {
	out, err := h.svc.List(c.Context())
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "internal error")
	}
	return c.JSON(fiber.Map{"data": out})
}

// Update upserts a flat JSON object {key:value}. Numbers and booleans are
// stringified; nested objects/arrays are rejected.
func (h *Handler) Update(c fiber.Ctx) error {
	var raw map[string]any
	if err := c.Bind().JSON(&raw); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid", "invalid JSON body")
	}
	vals := make(map[string]string, len(raw))
	for key, v := range raw {
		s, ok := stringify(v)
		if !ok {
			return writeError(c, fiber.StatusBadRequest, "invalid", "value for "+key+" must be a string, number, or boolean")
		}
		vals[key] = s
	}
	out, err := h.svc.Update(c.Context(), vals)
	if err != nil {
		return mapError(c, err)
	}
	return c.JSON(fiber.Map{"data": out})
}

func stringify(v any) (string, bool) {
	switch t := v.(type) {
	case nil:
		return "", true
	case string:
		return t, true
	case bool:
		return strconv.FormatBool(t), true
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64), true
	default:
		return "", false
	}
}

func mapError(c fiber.Ctx, err error) error {
	var fe *apperr.FieldError
	if errors.As(err, &fe) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": fiber.Map{"code": "invalid", "message": fe.Message, "field": fe.Field},
		})
	}
	return writeError(c, fiber.StatusInternalServerError, "internal", "internal error")
}

func writeError(c fiber.Ctx, status int, code, message string) error {
	return c.Status(status).JSON(fiber.Map{
		"error": fiber.Map{"code": code, "message": message},
	})
}
