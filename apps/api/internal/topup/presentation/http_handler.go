// Package presentation adapts top-up use cases to HTTP.
package presentation

import (
	"errors"

	"github.com/gofiber/fiber/v3"

	"boilerplate/api/internal/shared/apperr"
	topupapp "boilerplate/api/internal/topup/application"
	topupdomain "boilerplate/api/internal/topup/domain"
)

// customerLocalsKey is the dictated Locals key C1 fills from JWT claims
// ("" = admin/bypass, skips ownership checks).
const customerLocalsKey = "billing_customer_id"

// Handler serves POST /api/v1/portal/topup.
type Handler struct {
	svc *topupapp.Service
}

// NewHandler builds a Handler.
func NewHandler(svc *topupapp.Service) *Handler {
	return &Handler{svc: svc}
}

type topupRequest struct {
	PPPoEAccountID string `json:"pppoe_account_id"`
	Bytes          int64  `json:"bytes"`
}

// Topup creates the unpaid top-up invoice for one own PPPoE account:
// {data:{invoice:{id,number,total}}}. The customer pays through the
// existing Bayar flow; quota lands when the invoice settles.
func (h *Handler) Topup(c fiber.Ctx) error {
	var req topupRequest
	if err := c.Bind().JSON(&req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid", "invalid JSON body")
	}
	if req.PPPoEAccountID == "" {
		return writeFieldError(c, apperr.Invalid("pppoe_account_id", "required"))
	}
	custID, _ := c.Locals(customerLocalsKey).(string)
	inv, err := h.svc.CreateInvoice(c.Context(), custID, req.PPPoEAccountID, req.Bytes)
	if err != nil {
		return mapError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"data": fiber.Map{
		"invoice": fiber.Map{"id": inv.ID, "number": inv.Number, "total": inv.Total},
	}})
}

func mapError(c fiber.Ctx, err error) error {
	var fe *apperr.FieldError
	if errors.As(err, &fe) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": fiber.Map{"code": "invalid", "message": fe.Message, "field": fe.Field},
		})
	}
	switch {
	case errors.Is(err, topupdomain.ErrAccountNotFound):
		return writeError(c, fiber.StatusNotFound, "not_found", "pppoe account not found")
	case errors.Is(err, topupdomain.ErrBadPreset):
		return writeError(c, fiber.StatusBadRequest, "invalid", "bytes must be a 1, 5, or 10 GB preset")
	case errors.Is(err, topupdomain.ErrNoQuotaPackage):
		return writeError(c, fiber.StatusBadRequest, "invalid", "package has no quota")
	}
	return writeError(c, fiber.StatusInternalServerError, "internal", "internal error")
}

func writeError(c fiber.Ctx, status int, code, message string) error {
	return c.Status(status).JSON(fiber.Map{
		"error": fiber.Map{"code": code, "message": message},
	})
}

func writeFieldError(c fiber.Ctx, err error) error {
	var fe *apperr.FieldError
	if errors.As(err, &fe) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": fiber.Map{"code": "invalid", "message": fe.Message, "field": fe.Field},
		})
	}
	return writeError(c, fiber.StatusBadRequest, "invalid", "invalid request")
}
