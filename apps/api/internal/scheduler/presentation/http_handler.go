// Package presentation adapts scheduler use cases to HTTP.
package presentation

import (
	"github.com/gofiber/fiber/v3"

	schedapp "boilerplate/api/internal/scheduler/application"
)

// Handler serves /api/v1/admin/scheduler endpoints.
type Handler struct {
	svc *schedapp.Service
}

// NewHandler builds a Handler.
func NewHandler(svc *schedapp.Service) *Handler {
	return &Handler{svc: svc}
}

type jobStatus struct {
	Job       string  `json:"job"`
	LastRun   *string `json:"last_run"`
	LastOK    bool    `json:"last_ok"`
	LastError string  `json:"last_error"`
	Runs      int64   `json:"runs"`
}

// Status returns the last run per job: {data:{jobs:[...]}}.
func (h *Handler) Status(c fiber.Ctx) error {
	rows := h.svc.Status()
	jobs := make([]jobStatus, 0, len(rows))
	for _, st := range rows {
		j := jobStatus{Job: st.Job, LastOK: st.LastOK, LastError: st.LastError, Runs: st.Runs}
		if !st.LastRun.IsZero() {
			s := st.LastRun.UTC().Format("2006-01-02T15:04:05Z07:00")
			j.LastRun = &s
		}
		jobs = append(jobs, j)
	}
	return c.JSON(fiber.Map{"data": fiber.Map{"jobs": jobs}})
}
