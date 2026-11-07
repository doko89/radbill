// Package presentation adapts pppoe use cases to HTTP.
package presentation

import (
	"errors"
	"log"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v3"

	authdomain "boilerplate/api/internal/jwtauth/domain"
	authpres "boilerplate/api/internal/jwtauth/presentation"
	pppoeapp "boilerplate/api/internal/pppoe/application"
	"boilerplate/api/internal/pppoe/domain"
	"boilerplate/api/internal/shared/apperr"
)

// Handler serves /api/v1/admin/pppoe-accounts endpoints.
type Handler struct {
	svc *pppoeapp.Service
}

// NewHandler builds a Handler.
func NewHandler(svc *pppoeapp.Service) *Handler {
	return &Handler{svc: svc}
}

type pppoeRequest struct {
	CustomerID string  `json:"customer_id"`
	PackageID  string  `json:"package_id"`
	NASID      string  `json:"nas_id"`
	Username   string  `json:"username"`
	Password   string  `json:"password"`
	ExpiresAt  *string `json:"expires_at"`
	StaticIP   string  `json:"static_ip"`
}

// List returns a page of PPPoE accounts: {data:[],total} (no passwords).
func (h *Handler) List(c fiber.Ctx) error {
	limit, offset := pagination(c)
	items, total, err := h.svc.List(c.Context(), limit, offset)
	if err != nil {
		return mapError(c, err)
	}
	return c.JSON(fiber.Map{"data": items, "total": total})
}

// Get returns one PPPoE account. ?reveal=true includes the plaintext
// password and writes a password_revealed audit log line (audit table
// lands in Fase 4; until then the line goes to the server log).
func (h *Handler) Get(c fiber.Ctx) error {
	reveal := c.Query("reveal") == "true"
	dto, err := h.svc.Get(c.Context(), c.Params("id"), reveal)
	if err != nil {
		return mapError(c, err)
	}
	if reveal {
		log.Printf("audit password_revealed actor=%s pppoe=%s", authpres.Subject(c), dto.ID)
	}
	return c.JSON(fiber.Map{"data": dto})
}

// Create persists a PPPoE account.
func (h *Handler) Create(c fiber.Ctx) error {
	var req pppoeRequest
	if err := c.Bind().JSON(&req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid", "invalid JSON body")
	}
	in, err := toInput(req)
	if err != nil {
		return mapError(c, err)
	}
	dto, err := h.svc.Create(c.Context(), in)
	if err != nil {
		return mapError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"data": dto})
}

// Update replaces PPPoE details.
func (h *Handler) Update(c fiber.Ctx) error {
	var req pppoeRequest
	if err := c.Bind().JSON(&req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid", "invalid JSON body")
	}
	in, err := toInput(req)
	if err != nil {
		return mapError(c, err)
	}
	dto, err := h.svc.Update(c.Context(), c.Params("id"), in)
	if err != nil {
		return mapError(c, err)
	}
	return c.JSON(fiber.Map{"data": dto})
}

// Delete removes a PPPoE account.
func (h *Handler) Delete(c fiber.Ctx) error {
	if err := h.svc.Delete(c.Context(), c.Params("id")); err != nil {
		return mapError(c, err)
	}
	return c.JSON(fiber.Map{"data": fiber.Map{"ok": true}})
}

// Isolate moves the account to isolated.
func (h *Handler) Isolate(c fiber.Ctx) error {
	dto, err := h.svc.Isolate(c.Context(), c.Params("id"))
	if err != nil {
		return mapError(c, err)
	}
	return c.JSON(fiber.Map{"data": dto})
}

// Activate moves the account to active.
func (h *Handler) Activate(c fiber.Ctx) error {
	dto, err := h.svc.Activate(c.Context(), c.Params("id"))
	if err != nil {
		return mapError(c, err)
	}
	return c.JSON(fiber.Map{"data": dto})
}

// Disable moves the account to disabled.
func (h *Handler) Disable(c fiber.Ctx) error {
	dto, err := h.svc.Disable(c.Context(), c.Params("id"))
	if err != nil {
		return mapError(c, err)
	}
	return c.JSON(fiber.Map{"data": dto})
}

type extendRequest struct {
	Days int `json:"days"`
}

// Extend moves expiry forward by days.
func (h *Handler) Extend(c fiber.Ctx) error {
	var req extendRequest
	if err := c.Bind().JSON(&req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid", "invalid JSON body")
	}
	dto, err := h.svc.Extend(c.Context(), c.Params("id"), req.Days)
	if err != nil {
		return mapError(c, err)
	}
	return c.JSON(fiber.Map{"data": dto})
}

type changePackageRequest struct {
	PackageID string `json:"package_id"`
}

// ChangePackage switches the package.
func (h *Handler) ChangePackage(c fiber.Ctx) error {
	var req changePackageRequest
	if err := c.Bind().JSON(&req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid", "invalid JSON body")
	}
	dto, err := h.svc.ChangePackage(c.Context(), c.Params("id"), req.PackageID)
	if err != nil {
		return mapError(c, err)
	}
	return c.JSON(fiber.Map{"data": dto})
}

// ResetQuota clears usage counters.
func (h *Handler) ResetQuota(c fiber.Ctx) error {
	if err := h.svc.ResetQuota(c.Context(), c.Params("id")); err != nil {
		return mapError(c, err)
	}
	return c.JSON(fiber.Map{"data": fiber.Map{"ok": true}})
}

// Disconnect drops live sessions via CoA/Disconnect.
func (h *Handler) Disconnect(c fiber.Ctx) error {
	dto, err := h.svc.Disconnect(c.Context(), c.Params("id"))
	if err != nil {
		return mapError(c, err)
	}
	return c.JSON(fiber.Map{"data": dto})
}

type portalRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// CreatePortalAccount creates a portal login for the owner customer.
func (h *Handler) CreatePortalAccount(c fiber.Ctx) error {
	var req portalRequest
	if err := c.Bind().JSON(&req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid", "invalid JSON body")
	}
	dto, err := h.svc.CreatePortalAccount(c.Context(), c.Params("id"), req.Username, req.Password)
	if err != nil {
		return mapError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"data": dto})
}

// ResetPortalAccount replaces the portal password.
func (h *Handler) ResetPortalAccount(c fiber.Ctx) error {
	var req portalRequest
	if err := c.Bind().JSON(&req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid", "invalid JSON body")
	}
	dto, err := h.svc.ResetPortalAccount(c.Context(), c.Params("id"), req.Password)
	if err != nil {
		return mapError(c, err)
	}
	return c.JSON(fiber.Map{"data": dto})
}

func toInput(req pppoeRequest) (pppoeapp.DetailsInput, error) {
	in := pppoeapp.DetailsInput{
		CustomerID: req.CustomerID, PackageID: req.PackageID, NASID: req.NASID,
		Username: req.Username, Password: req.Password, StaticIP: req.StaticIP,
	}
	if req.ExpiresAt != nil && *req.ExpiresAt != "" {
		exp, err := time.Parse(time.RFC3339, *req.ExpiresAt)
		if err != nil {
			return pppoeapp.DetailsInput{}, apperr.Invalid("expires_at", "must be RFC3339")
		}
		in.ExpiresAt = &exp
	}
	return in, nil
}

func mapError(c fiber.Ctx, err error) error {
	var fe *apperr.FieldError
	if errors.As(err, &fe) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": fiber.Map{"code": "invalid", "message": fe.Message, "field": fe.Field},
		})
	}
	switch {
	case errors.Is(err, domain.ErrPPPoENotFound):
		return writeError(c, fiber.StatusNotFound, "not_found", "pppoe account not found")
	case errors.Is(err, domain.ErrUnknownCustomer):
		return writeError(c, fiber.StatusBadRequest, "invalid", "unknown customer")
	case errors.Is(err, domain.ErrUnknownPackage):
		return writeError(c, fiber.StatusBadRequest, "invalid", "unknown package")
	case errors.Is(err, domain.ErrUnknownNAS):
		return writeError(c, fiber.StatusBadRequest, "invalid", "unknown nas")
	case errors.Is(err, domain.ErrUsernameTaken):
		return writeError(c, fiber.StatusConflict, "conflict", "pppoe username already taken")
	case errors.Is(err, domain.ErrBadTransition):
		return writeError(c, fiber.StatusConflict, "conflict", "illegal status transition")
	case errors.Is(err, domain.ErrPPPoEInUse):
		return writeError(c, fiber.StatusConflict, "conflict", "pppoe account still has sessions or invoices")
	case errors.Is(err, domain.ErrPortalExists):
		return writeError(c, fiber.StatusConflict, "conflict", "customer already has a portal account")
	case errors.Is(err, domain.ErrNoPortalAccount):
		return writeError(c, fiber.StatusNotFound, "not_found", "customer has no portal account")
	case errors.Is(err, authdomain.ErrUsernameTaken):
		return writeError(c, fiber.StatusConflict, "conflict", "portal username already taken")
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
