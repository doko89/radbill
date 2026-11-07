// Package presentation adapts report use cases to HTTP (admin, JSON).
package presentation

import (
	"bytes"
	"encoding/csv"
	"errors"
	"strconv"

	"github.com/gofiber/fiber/v3"

	reportsapp "boilerplate/api/internal/reports/application"
	reportsdomain "boilerplate/api/internal/reports/domain"
)

// Handler serves /api/v1/admin/reports/* endpoints.
type Handler struct {
	svc *reportsapp.Service
}

// NewHandler builds a Handler.
func NewHandler(svc *reportsapp.Service) *Handler {
	return &Handler{svc: svc}
}

// Revenue answers ?from&to&group_by=day|month:
// {data:{points:[{period,total}],total}}. ?format=csv exports CSV.
func (h *Handler) Revenue(c fiber.Ctx) error {
	out, err := h.svc.Revenue(c.Context(), c.Query("from"), c.Query("to"), c.Query("group_by"))
	if err != nil {
		return mapError(c, err)
	}
	if c.Query("format") == "csv" {
		return writeCSV(c, "revenue.csv", []string{"period", "total"}, revenueRows(out))
	}
	return c.JSON(fiber.Map{"data": out})
}

// Usage answers ?from&to:
// {data:{points:[{date,bytes_in,bytes_out}],totals:{bytes_in,bytes_out}}}.
// ?format=csv exports CSV.
func (h *Handler) Usage(c fiber.Ctx) error {
	out, err := h.svc.Usage(c.Context(), c.Query("from"), c.Query("to"))
	if err != nil {
		return mapError(c, err)
	}
	if c.Query("format") == "csv" {
		return writeCSV(c, "usage.csv", []string{"date", "bytes_in", "bytes_out"}, usageRows(out))
	}
	return c.JSON(fiber.Map{"data": out})
}

// Receivables answers {data:{invoices:[...],total}}.
// ?format=csv exports CSV.
func (h *Handler) Receivables(c fiber.Ctx) error {
	out, err := h.svc.Receivables(c.Context())
	if err != nil {
		return mapError(c, err)
	}
	if c.Query("format") == "csv" {
		return writeCSV(c, "receivables.csv",
			[]string{"id", "number", "customer_name", "total", "due_date", "days_overdue"},
			receivablesRows(out))
	}
	return c.JSON(fiber.Map{"data": out})
}

// Growth answers ?months=6: {data:{points:[{period,new,churn,active}]}}.
// ?format=csv exports CSV.
func (h *Handler) Growth(c fiber.Ctx) error {
	months := 0
	if raw := c.Query("months"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return writeError(c, fiber.StatusBadRequest, "invalid", "months must be an integer 1..24")
		}
		months = n
	}
	out, err := h.svc.Growth(c.Context(), months)
	if err != nil {
		return mapError(c, err)
	}
	if c.Query("format") == "csv" {
		return writeCSV(c, "growth.csv",
			[]string{"period", "new", "churn", "active"}, growthRows(out))
	}
	return c.JSON(fiber.Map{"data": out})
}

// CSV exporters (Fase 4, agen E1/G4, ADITIF). Column order is stable;
// encoding/csv handles quoting/escaping. JSON responses are unchanged.

func revenueRows(out reportsdomain.Revenue) [][]string {
	rows := make([][]string, 0, len(out.Points))
	for _, p := range out.Points {
		rows = append(rows, []string{p.Period, strconv.FormatInt(p.Total, 10)})
	}
	return rows
}

func usageRows(out reportsdomain.Usage) [][]string {
	rows := make([][]string, 0, len(out.Points))
	for _, p := range out.Points {
		rows = append(rows, []string{
			p.Date,
			strconv.FormatInt(p.BytesIn, 10),
			strconv.FormatInt(p.BytesOut, 10),
		})
	}
	return rows
}

func receivablesRows(out reportsdomain.Receivables) [][]string {
	rows := make([][]string, 0, len(out.Invoices))
	for _, r := range out.Invoices {
		rows = append(rows, []string{
			r.ID, r.Number, r.CustomerName,
			strconv.FormatInt(r.Total, 10),
			r.DueDate,
			strconv.FormatInt(r.DaysOverdue, 10),
		})
	}
	return rows
}

func growthRows(out reportsdomain.Growth) [][]string {
	rows := make([][]string, 0, len(out.Points))
	for _, p := range out.Points {
		rows = append(rows, []string{
			p.Period,
			strconv.FormatInt(p.New, 10),
			strconv.FormatInt(p.Churn, 10),
			strconv.FormatInt(p.Active, 10),
		})
	}
	return rows
}

func writeCSV(c fiber.Ctx, filename string, header []string, rows [][]string) error {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.Write(header); err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "internal error")
	}
	for _, row := range rows {
		if err := w.Write(row); err != nil {
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

func mapError(c fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, reportsdomain.ErrBadRange):
		return writeError(c, fiber.StatusBadRequest, "invalid", "from/to must be YYYY-MM-DD with from <= to")
	case errors.Is(err, reportsdomain.ErrBadGroupBy):
		return writeError(c, fiber.StatusBadRequest, "invalid", "group_by must be day|month")
	case errors.Is(err, reportsdomain.ErrBadMonths):
		return writeError(c, fiber.StatusBadRequest, "invalid", "months must be an integer 1..24")
	}
	return writeError(c, fiber.StatusInternalServerError, "internal", "internal error")
}

func writeError(c fiber.Ctx, status int, code, message string) error {
	return c.Status(status).JSON(fiber.Map{
		"error": fiber.Map{"code": code, "message": message},
	})
}
