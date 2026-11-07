// Package presentation adapts customer use cases to HTTP.
package presentation

import (
	"errors"
	"strconv"

	"github.com/gofiber/fiber/v3"

	custapp "boilerplate/api/internal/customers/application"
	"boilerplate/api/internal/customers/domain"
	"boilerplate/api/internal/shared/apperr"
)

// Handler serves /api/v1/admin/customers endpoints.
type Handler struct {
	svc *custapp.Service
}

// NewHandler builds a Handler.
func NewHandler(svc *custapp.Service) *Handler {
	return &Handler{svc: svc}
}

type customerRequest struct {
	Name    string `json:"name"`
	Phone   string `json:"phone"`
	Address string `json:"address"`
	Notes   string `json:"notes"`
}

// List returns a page of customers: {data:[],total}.
func (h *Handler) List(c fiber.Ctx) error {
	limit, offset := pagination(c)
	items, total, err := h.svc.List(c.Context(), limit, offset)
	if err != nil {
		return mapError(c, err)
	}
	return c.JSON(fiber.Map{"data": items, "total": total})
}

// Get returns one customer: {data:{}}.
func (h *Handler) Get(c fiber.Ctx) error {
	dto, err := h.svc.Get(c.Context(), c.Params("id"))
	if err != nil {
		return mapError(c, err)
	}
	return c.JSON(fiber.Map{"data": dto})
}

// Create persists a customer.
func (h *Handler) Create(c fiber.Ctx) error {
	var req customerRequest
	if err := c.Bind().JSON(&req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid", "invalid JSON body")
	}
	dto, err := h.svc.Create(c.Context(), custapp.CreateInput{
		Name: req.Name, Phone: req.Phone, Address: req.Address, Notes: req.Notes,
	})
	if err != nil {
		return mapError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"data": dto})
}

// Update replaces customer details.
func (h *Handler) Update(c fiber.Ctx) error {
	var req customerRequest
	if err := c.Bind().JSON(&req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid", "invalid JSON body")
	}
	dto, err := h.svc.Update(c.Context(), c.Params("id"), custapp.UpdateInput{
		Name: req.Name, Phone: req.Phone, Address: req.Address, Notes: req.Notes,
	})
	if err != nil {
		return mapError(c, err)
	}
	return c.JSON(fiber.Map{"data": dto})
}

// Delete removes a customer.
func (h *Handler) Delete(c fiber.Ctx) error {
	if err := h.svc.Delete(c.Context(), c.Params("id")); err != nil {
		return mapError(c, err)
	}
	return c.JSON(fiber.Map{"data": fiber.Map{"ok": true}})
}

func mapError(c fiber.Ctx, err error) error {
	var fe *apperr.FieldError
	if errors.As(err, &fe) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": fiber.Map{"code": "invalid", "message": fe.Message, "field": fe.Field},
		})
	}
	switch {
	case errors.Is(err, domain.ErrCustomerNotFound):
		return writeError(c, fiber.StatusNotFound, "not_found", "customer not found")
	case errors.Is(err, domain.ErrCustomerInUse):
		return writeError(c, fiber.StatusConflict, "conflict", "customer still has pppoe accounts")
	}
	return writeError(c, fiber.StatusInternalServerError, "internal", "internal error")
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
