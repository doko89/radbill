// Package presentation adapts package use cases to HTTP.
package presentation

import (
	"errors"
	"strconv"

	"github.com/gofiber/fiber/v3"

	pkgapp "boilerplate/api/internal/packages/application"
	"boilerplate/api/internal/packages/domain"
	"boilerplate/api/internal/shared/apperr"
)

// Handler serves /api/v1/admin/packages endpoints.
type Handler struct {
	svc *pkgapp.Service
}

// NewHandler builds a Handler.
func NewHandler(svc *pkgapp.Service) *Handler {
	return &Handler{svc: svc}
}

type packageRequest struct {
	Name               string `json:"name"`
	Price              int64  `json:"price"`
	Period             string `json:"period"`
	RateLimit          string `json:"rate_limit"`
	AddressPool        string `json:"address_pool"`
	ValiditySeconds    *int64 `json:"validity_seconds"`
	OnlineSecondsLimit *int64 `json:"online_seconds_limit"`
	QuotaBytes         *int64 `json:"quota_bytes"`
	QuotaReset         string `json:"quota_reset"`
	FUPThresholdBytes  *int64 `json:"fup_threshold_bytes"`
	FUPRateLimit       string `json:"fup_rate_limit"`
	OnExhausted        string `json:"on_exhausted"`
	IsPublic           *bool  `json:"is_public"`
	AllowSelfService   *bool  `json:"allow_self_service"`
	UpgradeGroup       string `json:"upgrade_group"`
}

// List returns a page of packages: {data:[],total}.
func (h *Handler) List(c fiber.Ctx) error {
	limit, offset := pagination(c)
	items, total, err := h.svc.List(c.Context(), limit, offset)
	if err != nil {
		return mapError(c, err)
	}
	return c.JSON(fiber.Map{"data": items, "total": total})
}

// Get returns one package: {data:{}}.
func (h *Handler) Get(c fiber.Ctx) error {
	dto, err := h.svc.Get(c.Context(), c.Params("id"))
	if err != nil {
		return mapError(c, err)
	}
	return c.JSON(fiber.Map{"data": dto})
}

// Create persists a package. Booleans default to true when omitted.
func (h *Handler) Create(c fiber.Ctx) error {
	var req packageRequest
	if err := c.Bind().JSON(&req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid", "invalid JSON body")
	}
	dto, err := h.svc.Create(c.Context(), toInput(req, true))
	if err != nil {
		return mapError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"data": dto})
}

// Update replaces package details.
func (h *Handler) Update(c fiber.Ctx) error {
	var req packageRequest
	if err := c.Bind().JSON(&req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid", "invalid JSON body")
	}
	dto, err := h.svc.Update(c.Context(), c.Params("id"), toInput(req, true))
	if err != nil {
		return mapError(c, err)
	}
	return c.JSON(fiber.Map{"data": dto})
}

// Delete removes a package.
func (h *Handler) Delete(c fiber.Ctx) error {
	if err := h.svc.Delete(c.Context(), c.Params("id")); err != nil {
		return mapError(c, err)
	}
	return c.JSON(fiber.Map{"data": fiber.Map{"ok": true}})
}

func toInput(req packageRequest, boolDefault bool) pkgapp.DetailsInput {
	isPublic, selfService := boolDefault, boolDefault
	if req.IsPublic != nil {
		isPublic = *req.IsPublic
	}
	if req.AllowSelfService != nil {
		selfService = *req.AllowSelfService
	}
	return pkgapp.DetailsInput{
		Name: req.Name, Price: req.Price, Period: req.Period,
		RateLimit: req.RateLimit, AddressPool: req.AddressPool,
		ValiditySeconds: req.ValiditySeconds, OnlineSecondsLimit: req.OnlineSecondsLimit,
		QuotaBytes: req.QuotaBytes, QuotaReset: req.QuotaReset,
		FUPThresholdBytes: req.FUPThresholdBytes, FUPRateLimit: req.FUPRateLimit,
		OnExhausted: req.OnExhausted, IsPublic: isPublic,
		AllowSelfService: selfService, UpgradeGroup: req.UpgradeGroup,
	}
}

func mapError(c fiber.Ctx, err error) error {
	var fe *apperr.FieldError
	if errors.As(err, &fe) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": fiber.Map{"code": "invalid", "message": fe.Message, "field": fe.Field},
		})
	}
	switch {
	case errors.Is(err, domain.ErrPackageNotFound):
		return writeError(c, fiber.StatusNotFound, "not_found", "package not found")
	case errors.Is(err, domain.ErrPackageInUse):
		return writeError(c, fiber.StatusConflict, "conflict", "package still has pppoe accounts")
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
