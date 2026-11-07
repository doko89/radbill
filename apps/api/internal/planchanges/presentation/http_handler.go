// Package presentation adapts plan-change use cases to HTTP.
package presentation

import (
	"errors"
	"log"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v3"

	auditapp "boilerplate/api/internal/audit/application"
	auditdomain "boilerplate/api/internal/audit/domain"
	authpres "boilerplate/api/internal/jwtauth/presentation"
	planchangesapp "boilerplate/api/internal/planchanges/application"
	planchangesdomain "boilerplate/api/internal/planchanges/domain"
	planchangesinfra "boilerplate/api/internal/planchanges/infrastructure"
	"boilerplate/api/internal/shared/apperr"
)

// customerLocalsKey is the dictated Locals key C1 fills from JWT claims
// ("" = admin/bypass, skips ownership checks).
const customerLocalsKey = "billing_customer_id"

// Handler serves portal + admin plan-change endpoints.
type Handler struct {
	svc     *planchangesapp.Service
	catalog *planchangesinfra.PackageCatalog
	// audit is the G4 sink (nil = disabled, pre-G4 behavior).
	audit *auditapp.Service
}

// NewHandler builds a Handler.
func NewHandler(svc *planchangesapp.Service, catalog *planchangesinfra.PackageCatalog) *Handler {
	return &Handler{svc: svc, catalog: catalog}
}

// SetAuditor installs the G4 audit sink (Fase 4, ADITIF).
func (h *Handler) SetAuditor(a *auditapp.Service) { h.audit = a }

// auditLog records one best-effort entry: failures are logged, the
// request still succeeds. Self-service moves land in the admin audit
// log with the portal user as actor.
func (h *Handler) auditLog(c fiber.Ctx, action, targetID, detail string) {
	if h.audit == nil {
		return
	}
	if err := h.audit.Log(c.Context(), auditapp.LogInput{
		ActorID: authpres.Subject(c), Action: action,
		TargetType: auditdomain.TargetPlanChange, TargetID: targetID,
		Detail: detail, IP: c.IP(),
	}); err != nil {
		log.Printf("audit: log %s failed: %v", action, err)
	}
}

type createRequest struct {
	PPPoEAccountID string `json:"pppoe_account_id"`
	ToPackageID    string `json:"to_package_id"`
}

// ChangeDTO is the public shape of a plan change.
type ChangeDTO struct {
	ID             string  `json:"id"`
	PPPoEAccountID string  `json:"pppoe_account_id"`
	FromPackage    string  `json:"from_package"`
	ToPackage      string  `json:"to_package"`
	EffectiveAt    string  `json:"effective_at"`
	Status         string  `json:"status"`
	CreatedAt      string  `json:"created_at"`
	ProrataInvoice *string `json:"prorata_invoice_id,omitempty"`
}

// Create records a self-service move:
// {data:{status,effective_at,prorata_invoice_id?}} (dictated shape).
func (h *Handler) Create(c fiber.Ctx) error {
	var req createRequest
	if err := c.Bind().JSON(&req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid", "invalid JSON body")
	}
	if req.PPPoEAccountID == "" {
		return writeFieldError(c, apperr.Invalid("pppoe_account_id", "required"))
	}
	if req.ToPackageID == "" {
		return writeFieldError(c, apperr.Invalid("to_package_id", "required"))
	}
	custID, _ := c.Locals(customerLocalsKey).(string)
	res, err := h.svc.Request(c.Context(), custID, req.PPPoEAccountID, req.ToPackageID)
	if err != nil {
		return mapError(c, err)
	}
	h.auditLog(c, auditdomain.ActionPlanChangeCreated, res.Change.ID,
		"pppoe="+res.Change.PPPoEAccountID+" to="+res.Change.ToPackage)
	data := fiber.Map{
		"status":       res.Change.Status,
		"effective_at": res.Change.EffectiveAt.UTC().Format(time.RFC3339),
	}
	if res.ProrataInvoiceID != nil {
		data["prorata_invoice_id"] = *res.ProrataInvoiceID
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"data": data})
}

// List returns the caller's own changes: {data:[],total}.
func (h *Handler) List(c fiber.Ctx) error {
	limit, offset := pagination(c)
	custID, _ := c.Locals(customerLocalsKey).(string)
	rows, total, err := h.svc.ListMine(c.Context(), custID, limit, offset)
	if err != nil {
		return mapError(c, err)
	}
	return c.JSON(fiber.Map{"data": toDTOs(rows), "total": total})
}

// Cancel voids a pending change.
func (h *Handler) Cancel(c fiber.Ctx) error {
	custID, _ := c.Locals(customerLocalsKey).(string)
	change, err := h.svc.Cancel(c.Context(), custID, c.Params("id"))
	if err != nil {
		return mapError(c, err)
	}
	h.auditLog(c, auditdomain.ActionPlanChangeCancelled, change.ID, "")
	return c.JSON(fiber.Map{"data": toDTO(change, nil)})
}

// ListAdmin returns every change: {data:[],total}.
func (h *Handler) ListAdmin(c fiber.Ctx) error {
	limit, offset := pagination(c)
	rows, total, err := h.svc.ListAll(c.Context(), limit, offset)
	if err != nil {
		return mapError(c, err)
	}
	return c.JSON(fiber.Map{"data": toDTOs(rows), "total": total})
}

// Packages lists is_public packages for the portal plan-picker:
// {data:[...],total} (catalog specs only, no sensitive fields).
func (h *Handler) Packages(c fiber.Ctx) error {
	rows, err := h.catalog.ListPublic(c.Context())
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "internal error")
	}
	return c.JSON(fiber.Map{"data": rows, "total": len(rows)})
}

func toDTO(change *planchangesdomain.PlanChange, prorataID *string) ChangeDTO {
	return ChangeDTO{
		ID: change.ID, PPPoEAccountID: change.PPPoEAccountID,
		FromPackage: change.FromPackage, ToPackage: change.ToPackage,
		EffectiveAt:    change.EffectiveAt.UTC().Format(time.RFC3339),
		Status:         change.Status,
		CreatedAt:      change.CreatedAt.UTC().Format(time.RFC3339),
		ProrataInvoice: prorataID,
	}
}

func toDTOs(rows []*planchangesdomain.PlanChange) []ChangeDTO {
	out := make([]ChangeDTO, 0, len(rows))
	for _, r := range rows {
		out = append(out, toDTO(r, nil))
	}
	return out
}

func mapError(c fiber.Ctx, err error) error {
	var fe *apperr.FieldError
	if errors.As(err, &fe) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": fiber.Map{"code": "invalid", "message": fe.Message, "field": fe.Field},
		})
	}
	switch {
	case errors.Is(err, planchangesdomain.ErrAccountNotFound),
		errors.Is(err, planchangesdomain.ErrChangeNotFound):
		return writeError(c, fiber.StatusNotFound, "not_found", "not found")
	case errors.Is(err, planchangesdomain.ErrHasUnpaid):
		return writeError(c, fiber.StatusConflict, "conflict", "account has an unpaid invoice")
	case errors.Is(err, planchangesdomain.ErrFrequencyExceeded):
		return writeError(c, fiber.StatusConflict, "conflict", "only one plan change per period")
	case errors.Is(err, planchangesdomain.ErrNotPending):
		return writeError(c, fiber.StatusConflict, "conflict", "only pending changes can be cancelled")
	case errors.Is(err, planchangesdomain.ErrNotSelfService):
		return writeError(c, fiber.StatusForbidden, "forbidden", "package is not self-service")
	case errors.Is(err, planchangesdomain.ErrGroupMismatch):
		return writeError(c, fiber.StatusForbidden, "forbidden", "package is in another upgrade group")
	case errors.Is(err, planchangesdomain.ErrSamePackage):
		return writeError(c, fiber.StatusBadRequest, "invalid", "already on that package")
	case errors.Is(err, planchangesdomain.ErrPackageNotFound):
		return writeError(c, fiber.StatusBadRequest, "invalid", "unknown target package")
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
