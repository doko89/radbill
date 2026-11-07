// Package presentation adapts billing use cases to HTTP.
package presentation

import (
	"errors"
	"strconv"

	"github.com/gofiber/fiber/v3"

	billapp "boilerplate/api/internal/billing/application"
	"boilerplate/api/internal/billing/domain"
	"boilerplate/api/internal/shared/apperr"
)

// Handler serves /api/v1/admin/invoices endpoints.
type Handler struct {
	svc *billapp.Service
}

// NewHandler builds a Handler.
func NewHandler(svc *billapp.Service) *Handler {
	return &Handler{svc: svc}
}

// List returns a page of invoices: {data:[],total}. ?status= filters.
func (h *Handler) List(c fiber.Ctx) error {
	limit, offset := pagination(c)
	items, total, err := h.svc.List(c.Context(), c.Query("status"), limit, offset)
	if err != nil {
		return mapError(c, err)
	}
	return c.JSON(fiber.Map{"data": items, "total": total})
}

// Get returns an invoice with items and payments: {data:{}}.
func (h *Handler) Get(c fiber.Ctx) error {
	dto, err := h.svc.Get(c.Context(), c.Params("id"))
	if err != nil {
		return mapError(c, err)
	}
	return c.JSON(fiber.Map{"data": dto})
}

type itemRequest struct {
	Description string `json:"description"`
	Qty         int64  `json:"qty"`
	Price       int64  `json:"price"`
}

type createRequest struct {
	PPPoEAccountID string        `json:"pppoe_account_id"`
	Items          []itemRequest `json:"items"`
	DueDate        string        `json:"due_date"`
}

// Create builds an unpaid invoice.
func (h *Handler) Create(c fiber.Ctx) error {
	var req createRequest
	if err := c.Bind().JSON(&req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid", "invalid JSON body")
	}
	items := make([]billapp.ItemInput, 0, len(req.Items))
	for _, it := range req.Items {
		qty := it.Qty
		if qty == 0 {
			qty = 1
		}
		items = append(items, billapp.ItemInput{Description: it.Description, Qty: qty, Price: it.Price})
	}
	dto, err := h.svc.Create(c.Context(), req.PPPoEAccountID, items, req.DueDate)
	if err != nil {
		return mapError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"data": dto})
}

// Void cancels an unpaid invoice.
func (h *Handler) Void(c fiber.Ctx) error {
	dto, err := h.svc.Void(c.Context(), c.Params("id"))
	if err != nil {
		return mapError(c, err)
	}
	return c.JSON(fiber.Map{"data": dto})
}

type manualPaymentRequest struct {
	Amount int64 `json:"amount"`
}

// ManualPayment records a cash/transfer payment (amount 0 = full total).
func (h *Handler) ManualPayment(c fiber.Ctx) error {
	var req manualPaymentRequest
	if err := c.Bind().JSON(&req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid", "invalid JSON body")
	}
	if req.Amount < 0 {
		return writeError(c, fiber.StatusBadRequest, "invalid", "amount must be >= 0")
	}
	dto, err := h.svc.ManualPayment(c.Context(), c.Params("id"), req.Amount)
	if err != nil {
		return mapError(c, err)
	}
	return c.JSON(fiber.Map{"data": dto})
}

func mapError(c fiber.Ctx, err error) error {
	var fe *apperr.FieldError
	if errors.As(err, &fe) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": fiber.Map{"code": "invalid", "message": fe.Message, "field": fe.Field},
		})
	}
	switch {
	case errors.Is(err, domain.ErrInvoiceNotFound):
		return writeError(c, fiber.StatusNotFound, "not_found", "invoice not found")
	case errors.Is(err, domain.ErrUnknownPPPoE):
		return writeError(c, fiber.StatusBadRequest, "invalid", "unknown pppoe account")
	case errors.Is(err, domain.ErrAmountMismatch):
		return writeError(c, fiber.StatusBadRequest, "invalid", "amount differs from invoice total")
	case errors.Is(err, domain.ErrAlreadyPaid):
		return writeError(c, fiber.StatusConflict, "conflict", "invoice already paid")
	case errors.Is(err, domain.ErrInvoiceVoid):
		return writeError(c, fiber.StatusConflict, "conflict", "invoice is void")
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
