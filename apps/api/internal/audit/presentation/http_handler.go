// Package presentation adapts audit use cases to HTTP.
package presentation

import (
	"bytes"
	"encoding/csv"
	"errors"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v3"

	auditapp "boilerplate/api/internal/audit/application"
	auditdomain "boilerplate/api/internal/audit/domain"
)

// pageSize is the fixed JSON list page size.
const pageSize = 50

// csvCap bounds one CSV export.
const csvCap = 10000

// Handler serves /api/v1/admin/audit-logs endpoints.
type Handler struct {
	svc *auditapp.Service
}

// NewHandler builds a Handler.
func NewHandler(svc *auditapp.Service) *Handler {
	return &Handler{svc: svc}
}

// entryDTO is the public shape of one audit entry. actor_id is null
// when the entry has no actor (system/unknown).
type entryDTO struct {
	ID            string  `json:"id"`
	ActorID       *string `json:"actor_id"`
	ActorUsername string  `json:"actor_username"`
	Action        string  `json:"action"`
	TargetType    string  `json:"target_type"`
	TargetID      string  `json:"target_id"`
	Detail        string  `json:"detail"`
	IP            string  `json:"ip"`
	CreatedAt     string  `json:"created_at"`
}

// List answers ?actor&action&from&to&page: {data:[],total,page}.
// from/to accept RFC3339 or YYYY-MM-DD (a bare date covers the whole
// UTC day). ?format=csv exports the filtered rows as CSV instead.
func (h *Handler) List(c fiber.Ctx) error {
	from, err := parseBound(c.Query("from"), true)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid", "from must be RFC3339 or YYYY-MM-DD")
	}
	to, err := parseBound(c.Query("to"), false)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid", "to must be RFC3339 or YYYY-MM-DD")
	}
	if c.Query("format") == "csv" {
		rows, _, err := h.svc.List(c.Context(), auditdomain.Filter{
			Actor: c.Query("actor"), Action: c.Query("action"),
			From: from, To: to, Limit: csvCap,
		})
		if err != nil {
			return writeError(c, fiber.StatusInternalServerError, "internal", "internal error")
		}
		return writeCSV(c, "audit-logs.csv", rows)
	}
	page := 1
	if raw := c.Query("page"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			return writeError(c, fiber.StatusBadRequest, "invalid", "page must be an integer >= 1")
		}
		page = n
	}
	rows, total, err := h.svc.List(c.Context(), auditdomain.Filter{
		Actor: c.Query("actor"), Action: c.Query("action"),
		From: from, To: to, Limit: pageSize, Offset: (page - 1) * pageSize,
	})
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "internal error")
	}
	return c.JSON(fiber.Map{"data": toDTOs(rows), "total": total, "page": page})
}

// parseBound normalizes a from/to bound to RFC3339 UTC. "" stays open;
// YYYY-MM-DD expands to the day start (from) or day end (to).
func parseBound(raw string, isFrom bool) (string, error) {
	if raw == "" {
		return "", nil
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.UTC().Format(time.RFC3339), nil
	}
	if d, err := time.Parse("2006-01-02", raw); err == nil {
		if isFrom {
			return d.UTC().Format("2006-01-02") + "T00:00:00Z", nil
		}
		return d.UTC().Format("2006-01-02") + "T23:59:59Z", nil
	}
	return "", errBadBound
}

var errBadBound = errors.New("invalid bound")

func toDTOs(rows []auditdomain.Entry) []entryDTO {
	out := make([]entryDTO, 0, len(rows))
	for _, r := range rows {
		var actorID *string
		if r.ActorID != "" {
			id := r.ActorID
			actorID = &id
		}
		out = append(out, entryDTO{
			ID: r.ID, ActorID: actorID,
			ActorUsername: r.ActorUsername, Action: r.Action,
			TargetType: r.TargetType, TargetID: r.TargetID,
			Detail: r.Detail, IP: r.IP,
			CreatedAt: r.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	return out
}

// csvHeader is the stable audit CSV column order.
var csvHeader = []string{
	"id", "actor_id", "actor_username", "action",
	"target_type", "target_id", "detail", "ip", "created_at",
}

func writeCSV(c fiber.Ctx, filename string, rows []auditdomain.Entry) error {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.Write(csvHeader); err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "internal error")
	}
	for _, r := range rows {
		if err := w.Write([]string{
			r.ID, r.ActorID, r.ActorUsername, r.Action,
			r.TargetType, r.TargetID, r.Detail, r.IP,
			r.CreatedAt.UTC().Format(time.RFC3339),
		}); err != nil {
			return writeError(c, fiber.StatusInternalServerError, "internal", "internal error")
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "internal error")
	}
	c.Set("Content-Type", "text/csv; charset=utf-8")
	c.Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	return c.Send(buf.Bytes())
}

func writeError(c fiber.Ctx, status int, code, message string) error {
	return c.Status(status).JSON(fiber.Map{
		"error": fiber.Map{"code": code, "message": message},
	})
}
