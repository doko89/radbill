// Package application hosts the dashboard and metrics use cases.
//
// Dashboard aggregates are computed over UTC calendar boundaries
// (month-to-date from the 1st 00:00 UTC, overdue vs today UTC) so the
// numbers are identical on sqlite and postgres.
package application

import (
	"context"
	"time"

	dashdomain "boilerplate/api/internal/dashboard/domain"
	scheddomain "boilerplate/api/internal/scheduler/domain"
)

// Service implements dashboard use cases.
type Service struct {
	store     dashdomain.Store
	now       func() time.Time
	startedAt time.Time
	ping      func(ctx context.Context) error
	scheduler func() []dashdomain.SchedulerJob
}

// NewService builds a Service. startedAt defaults to construction time
// (wiring constructs one service at boot, so uptime tracks the process).
func NewService(store dashdomain.Store) *Service {
	return &Service{store: store, now: time.Now, startedAt: time.Now().UTC()}
}

// SetNow overrides the clock (tests).
func (s *Service) SetNow(fn func() time.Time) { s.now = fn }

// SetStartedAt overrides the boot time (tests).
func (s *Service) SetStartedAt(t time.Time) { s.startedAt = t.UTC() }

// SetPing installs the DB reachability probe (server wiring).
func (s *Service) SetPing(fn func(ctx context.Context) error) { s.ping = fn }

// SetScheduler installs the scheduler snapshot provider (server wiring).
func (s *Service) SetScheduler(fn func() []dashdomain.SchedulerJob) { s.scheduler = fn }

// Dashboard assembles the admin dashboard aggregates.
func (s *Service) Dashboard(ctx context.Context) (dashdomain.Dashboard, error) {
	now := s.now().UTC()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339)

	amounts, err := s.store.PaymentsSince(ctx, monthStart)
	if err != nil {
		return dashdomain.Dashboard{}, err
	}
	var revenue int64
	for _, a := range amounts {
		revenue += a
	}

	unpaid, err := s.store.UnpaidTotals(ctx)
	if err != nil {
		return dashdomain.Dashboard{}, err
	}
	today := now.Format("2006-01-02")
	var outstanding, overdue int64
	for _, u := range unpaid {
		outstanding += u.Total
		if u.DueDate != "" && u.DueDate < today {
			overdue++
		}
	}

	counts, err := s.store.PPPoECounts(ctx)
	if err != nil {
		return dashdomain.Dashboard{}, err
	}
	online, err := s.store.OnlineSessions(ctx)
	if err != nil {
		return dashdomain.Dashboard{}, err
	}
	recent, err := s.store.RecentPayments(ctx, dashdomain.RecentPaymentsLimit)
	if err != nil {
		return dashdomain.Dashboard{}, err
	}
	if recent == nil {
		recent = []dashdomain.RecentPayment{}
	}
	return dashdomain.Dashboard{
		RevenueMTD:       revenue,
		OutstandingTotal: outstanding,
		OverdueCount:     overdue,
		Accounts: dashdomain.Accounts{
			Active:   counts["active"],
			Isolated: counts["isolated"],
			Disabled: counts["disabled"],
		},
		OnlineSessions: online,
		RecentPayments: recent,
	}, nil
}

// Metrics assembles the admin metrics: uptime, DB reachability,
// scheduler jobs, pending QRIS attempts, and attempts parked failed
// in the trailing 24h.
func (s *Service) Metrics(ctx context.Context) (dashdomain.Metrics, error) {
	now := s.now().UTC()
	uptime := int64(now.Sub(s.startedAt).Seconds())
	if uptime < 0 {
		uptime = 0
	}
	dbOK := true
	if s.ping != nil {
		if err := s.ping(ctx); err != nil {
			dbOK = false
		}
	}
	var jobs []dashdomain.SchedulerJob
	if s.scheduler != nil {
		jobs = s.scheduler()
	}
	if jobs == nil {
		jobs = []dashdomain.SchedulerJob{}
	}
	pending, err := s.store.PendingAttempts(ctx)
	if err != nil {
		return dashdomain.Metrics{}, err
	}
	failed, err := s.store.FailedAttemptsSince(ctx, now.Add(-24*time.Hour).Format(time.RFC3339))
	if err != nil {
		return dashdomain.Metrics{}, err
	}
	return dashdomain.Metrics{
		UptimeSeconds:     uptime,
		DbOK:              dbOK,
		Scheduler:         dashdomain.SchedulerStatus{Jobs: jobs},
		KriskrisPending:   pending,
		WebhooksFailed24h: failed,
	}, nil
}

// SchedulerJobs maps scheduler domain statuses to dashboard jobs,
// reusing the exact scheduler status JSON shape. last_run uses the
// same timestamp format as the scheduler status endpoint.
func SchedulerJobs(rows []scheddomain.Status) []dashdomain.SchedulerJob {
	jobs := make([]dashdomain.SchedulerJob, 0, len(rows))
	for _, st := range rows {
		j := dashdomain.SchedulerJob{
			Job: st.Job, LastOK: st.LastOK,
			LastError: st.LastError, Runs: st.Runs,
		}
		if !st.LastRun.IsZero() {
			s := st.LastRun.UTC().Format("2006-01-02T15:04:05Z07:00")
			j.LastRun = &s
		}
		jobs = append(jobs, j)
	}
	return jobs
}
