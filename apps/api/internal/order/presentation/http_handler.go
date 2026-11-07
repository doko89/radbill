// Package presentation adapts order use cases to HTTP.
package presentation

import (
	"errors"

	"github.com/gofiber/fiber/v3"

	orderapp "boilerplate/api/internal/order/application"
	"boilerplate/api/internal/order/domain"
	"boilerplate/api/internal/platform/session"
	"boilerplate/api/internal/shared/httpx"
)

// Handler serves /orders endpoints. All routes require authentication.
type Handler struct {
	svc *orderapp.Service
}

// NewHandler builds a Handler.
func NewHandler(svc *orderapp.Service) *Handler {
	return &Handler{svc: svc}
}

type createRequest struct {
	Item string `json:"item"`
	Qty  int64  `json:"qty"`
}

type statusRequest struct {
	Status string `json:"status"`
}

// Create persists a new open order.
func (h *Handler) Create(c fiber.Ctx) error {
	var req createRequest
	if err := c.Bind().JSON(&req); err != nil {
		return httpx.Error(c, fiber.StatusBadRequest, "invalid JSON body")
	}
	dto, err := h.svc.Create(c.Context(), orderapp.CreateInput{
		UserID: session.UserID(c),
		Item:   req.Item,
		Qty:    req.Qty,
	})
	if err != nil {
		if handled, herr := httpx.FieldError(c, err); handled {
			return herr
		}
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(dto)
}

// List returns the user's orders, newest first.
func (h *Handler) List(c fiber.Ctx) error {
	dtos, err := h.svc.List(c.Context(), session.UserID(c))
	if err != nil {
		return err
	}
	return c.JSON(dtos)
}

// SetStatus moves an order to done or cancelled.
func (h *Handler) SetStatus(c fiber.Ctx) error {
	var req statusRequest
	if err := c.Bind().JSON(&req); err != nil {
		return httpx.Error(c, fiber.StatusBadRequest, "invalid JSON body")
	}
	dto, err := h.svc.SetStatus(c.Context(), session.UserID(c), c.Params("id"), req.Status)
	if err != nil {
		if handled, herr := httpx.FieldError(c, err); handled {
			return herr
		}
		switch {
		case errors.Is(err, domain.ErrOrderNotFound):
			return httpx.Error(c, fiber.StatusNotFound, "order not found")
		case errors.Is(err, domain.ErrNotOpen):
			return httpx.Error(c, fiber.StatusConflict, "only open orders can change status")
		}
		return err
	}
	return c.JSON(dto)
}
