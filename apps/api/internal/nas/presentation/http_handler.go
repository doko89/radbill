// Package presentation adapts nas use cases to HTTP.
package presentation

import (
	"errors"
	"log"
	"strconv"

	"github.com/gofiber/fiber/v3"

	auditapp "boilerplate/api/internal/audit/application"
	auditdomain "boilerplate/api/internal/audit/domain"
	authpres "boilerplate/api/internal/jwtauth/presentation"
	nasapp "boilerplate/api/internal/nas/application"
	"boilerplate/api/internal/nas/domain"
	"boilerplate/api/internal/shared/apperr"
)

// Handler serves /api/v1/admin/nas endpoints.
type Handler struct {
	svc *nasapp.Service
	// audit is the G4 sink (nil = disabled, pre-G4 behavior).
	audit *auditapp.Service
}

// NewHandler builds a Handler.
func NewHandler(svc *nasapp.Service) *Handler {
	return &Handler{svc: svc}
}

// SetAuditor installs the G4 audit sink (Fase 4, ADITIF).
func (h *Handler) SetAuditor(a *auditapp.Service) { h.audit = a }

// auditLog records one best-effort entry: failures are logged, the
// request still succeeds. The shared secret never lands in detail.
func (h *Handler) auditLog(c fiber.Ctx, action, targetID, detail string) {
	if h.audit == nil {
		return
	}
	if err := h.audit.Log(c.Context(), auditapp.LogInput{
		ActorID: authpres.Subject(c), Action: action,
		TargetType: auditdomain.TargetNAS, TargetID: targetID,
		Detail: detail, IP: c.IP(),
	}); err != nil {
		log.Printf("audit: log %s failed: %v", action, err)
	}
}

type nasRequest struct {
	Name    string `json:"name"`
	IP      string `json:"ip"`
	Secret  string `json:"secret"`
	CoAPort int    `json:"coa_port"`
}

// List returns a page of NAS rows: {data:[],total}.
func (h *Handler) List(c fiber.Ctx) error {
	limit, offset := pagination(c)
	items, total, err := h.svc.List(c.Context(), limit, offset)
	if err != nil {
		return mapError(c, err)
	}
	return c.JSON(fiber.Map{"data": items, "total": total})
}

// Get returns one NAS: {data:{}} (secret never included).
func (h *Handler) Get(c fiber.Ctx) error {
	dto, err := h.svc.Get(c.Context(), c.Params("id"))
	if err != nil {
		return mapError(c, err)
	}
	return c.JSON(fiber.Map{"data": dto})
}

// Create persists a NAS.
func (h *Handler) Create(c fiber.Ctx) error {
	var req nasRequest
	if err := c.Bind().JSON(&req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid", "invalid JSON body")
	}
	dto, err := h.svc.Create(c.Context(), nasapp.DetailsInput{
		Name: req.Name, IP: req.IP, Secret: req.Secret, CoAPort: req.CoAPort,
	})
	if err != nil {
		return mapError(c, err)
	}
	h.auditLog(c, auditdomain.ActionNASCreated, dto.ID, "name="+dto.Name+" ip="+dto.IP)
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"data": dto})
}

// Update replaces NAS details.
func (h *Handler) Update(c fiber.Ctx) error {
	var req nasRequest
	if err := c.Bind().JSON(&req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid", "invalid JSON body")
	}
	dto, err := h.svc.Update(c.Context(), c.Params("id"), nasapp.DetailsInput{
		Name: req.Name, IP: req.IP, Secret: req.Secret, CoAPort: req.CoAPort,
	})
	if err != nil {
		return mapError(c, err)
	}
	h.auditLog(c, auditdomain.ActionNASUpdated, dto.ID, "name="+dto.Name+" ip="+dto.IP)
	return c.JSON(fiber.Map{"data": dto})
}

// Delete removes a NAS.
func (h *Handler) Delete(c fiber.Ctx) error {
	if err := h.svc.Delete(c.Context(), c.Params("id")); err != nil {
		return mapError(c, err)
	}
	h.auditLog(c, auditdomain.ActionNASDeleted, c.Params("id"), "")
	return c.JSON(fiber.Map{"data": fiber.Map{"ok": true}})
}

// TestCoA probes the NAS CoA endpoint.
func (h *Handler) TestCoA(c fiber.Ctx) error {
	res, err := h.svc.TestCoA(c.Context(), c.Params("id"))
	if err != nil {
		return mapError(c, err)
	}
	return c.JSON(fiber.Map{"data": res})
}

func mapError(c fiber.Ctx, err error) error {
	var fe *apperr.FieldError
	if errors.As(err, &fe) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": fiber.Map{"code": "invalid", "message": fe.Message, "field": fe.Field},
		})
	}
	switch {
	case errors.Is(err, domain.ErrNASNotFound):
		return writeError(c, fiber.StatusNotFound, "not_found", "nas not found")
	case errors.Is(err, domain.ErrNASIPTaken):
		return writeError(c, fiber.StatusConflict, "conflict", "nas ip already registered")
	case errors.Is(err, domain.ErrNASInUse):
		return writeError(c, fiber.StatusConflict, "conflict", "nas still referenced")
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
