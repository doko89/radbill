// Package application hosts report use cases. All grouping happens in
// Go over UTC calendar buckets (portable across sqlite/postgres);
// zero-filled buckets make every series chart-ready.
package application

import (
	"context"
	"time"

	reportsdomain "boilerplate/api/internal/reports/domain"
)

// queryLimit caps one report scan. Documented: installations larger than
// this per range need paged reports (Fase 4).
// queryLimit bounds the receivables scan (G4 hardening): far above realistic
// unpaid-invoice counts, prevents unbounded growth in response size.
const queryLimit = 5000

// pageSize bounds one account-status page for growth.
const pageSize = 2000

// Service implements report use cases.
type Service struct {
	store reportsdomain.Store
	now   func() time.Time
}

// NewService builds a Service.
func NewService(store reportsdomain.Store) *Service {
	return &Service{store: store, now: time.Now}
}

// SetNow overrides the clock (tests).
func (s *Service) SetNow(fn func() time.Time) { s.now = fn }

// Revenue sums payments (cash basis, every method including kriskris
// unique-code rows) per UTC day or month over [from,to], zero-filled.
// Empty from/to default to the trailing 30 days ending today (UTC).
func (s *Service) Revenue(ctx context.Context, from, to, groupBy string) (reportsdomain.Revenue, error) {
	if groupBy == "" {
		groupBy = reportsdomain.GroupByDay
	}
	if groupBy != reportsdomain.GroupByDay && groupBy != reportsdomain.GroupByMonth {
		return reportsdomain.Revenue{}, reportsdomain.ErrBadGroupBy
	}
	start, end, err := s.resolveRange(from, to)
	if err != nil {
		return reportsdomain.Revenue{}, err
	}
	payments, err := s.store.PaymentsInRange(ctx,
		start.Format(time.RFC3339), end.Format(time.RFC3339), queryLimit)
	if err != nil {
		return reportsdomain.Revenue{}, err
	}
	points := daySkeleton(start, end)
	if groupBy == reportsdomain.GroupByMonth {
		points = monthSkeleton(start, end)
	}
	index := make(map[string]int, len(points))
	for i := range points {
		index[points[i].Period] = i
	}
	var total int64
	for _, p := range payments {
		t, err := time.Parse(time.RFC3339, p.PaidAt)
		if err != nil {
			continue
		}
		key := t.UTC().Format("2006-01-02")
		if groupBy == reportsdomain.GroupByMonth {
			key = t.UTC().Format("2006-01")
		}
		if i, ok := index[key]; ok {
			points[i].Total += p.Amount
			total += p.Amount
		}
	}
	return reportsdomain.Revenue{Points: points, Total: total}, nil
}

// Usage sums radius session bytes per UTC day over [from,to],
// zero-filled; totals sum the same rows. Empty from/to default to the
// trailing 30 days ending today (UTC).
func (s *Service) Usage(ctx context.Context, from, to string) (reportsdomain.Usage, error) {
	start, end, err := s.resolveRange(from, to)
	if err != nil {
		return reportsdomain.Usage{}, err
	}
	sessions, err := s.store.SessionsInRange(ctx,
		start.Format(time.RFC3339), end.Format(time.RFC3339), queryLimit)
	if err != nil {
		return reportsdomain.Usage{}, err
	}
	points := make([]reportsdomain.UsagePoint, 0, int(end.Sub(start).Hours()/24))
	index := make(map[string]int)
	for d := start; d.Before(end); d = d.AddDate(0, 0, 1) {
		index[d.Format("2006-01-02")] = len(points)
		points = append(points, reportsdomain.UsagePoint{Date: d.Format("2006-01-02")})
	}
	var totals reportsdomain.UsageTotals
	for _, se := range sessions {
		t, err := time.Parse(time.RFC3339, se.StartedAt)
		if err != nil {
			continue
		}
		i, ok := index[t.UTC().Format("2006-01-02")]
		if !ok {
			continue
		}
		points[i].BytesIn += se.BytesIn
		points[i].BytesOut += se.BytesOut
		totals.BytesIn += se.BytesIn
		totals.BytesOut += se.BytesOut
	}
	return reportsdomain.Usage{Points: points, Totals: totals}, nil
}

// Receivables lists every unpaid invoice, oldest due first, with
// days_overdue clamped at zero (not-yet-due invoices show 0).
func (s *Service) Receivables(ctx context.Context) (reportsdomain.Receivables, error) {
	rows, err := s.store.UnpaidInvoices(ctx, queryLimit)
	if err != nil {
		return reportsdomain.Receivables{}, err
	}
	today := s.now().UTC().Truncate(24 * time.Hour)
	out := reportsdomain.Receivables{Invoices: make([]reportsdomain.Receivable, 0, len(rows))}
	for _, r := range rows {
		var overdue int64
		if due, err := time.Parse("2006-01-02", r.DueDate); err == nil {
			if d := int64(today.Sub(due.Truncate(24*time.Hour)).Hours() / 24); d > 0 {
				overdue = d
			}
		}
		out.Invoices = append(out.Invoices, reportsdomain.Receivable{
			ID: r.ID, Number: r.Number, CustomerName: r.CustomerName,
			Total: r.Total, DueDate: r.DueDate, DaysOverdue: overdue,
		})
		out.Total += r.Total
	}
	return out, nil
}

// Growth reports the trailing N UTC months ending with the current one:
// new = accounts created in the month, churn = accounts disabled in the
// month (status=disabled with updated_at in the month), active =
// accounts created at or before month end that were not disabled at or
// before month end. months == 0 selects the 6-month default.
func (s *Service) Growth(ctx context.Context, months int) (reportsdomain.Growth, error) {
	if months == 0 {
		months = reportsdomain.DefaultGrowthMonths
	}
	if months < reportsdomain.MinGrowthMonths || months > reportsdomain.MaxGrowthMonths {
		return reportsdomain.Growth{}, reportsdomain.ErrBadMonths
	}
	var accounts []reportsdomain.AccountStatus
	for offset := 0; ; offset += pageSize {
		rows, err := s.store.AccountStatuses(ctx, pageSize, offset)
		if err != nil {
			return reportsdomain.Growth{}, err
		}
		if len(rows) == 0 {
			break
		}
		accounts = append(accounts, rows...)
		if len(rows) < pageSize {
			break
		}
	}
	type parsed struct {
		created, updated time.Time
		disabled         bool
		ok               bool
	}
	parsedAccts := make([]parsed, 0, len(accounts))
	for _, a := range accounts {
		c, err1 := time.Parse(time.RFC3339, a.CreatedAt)
		u, err2 := time.Parse(time.RFC3339, a.UpdatedAt)
		if err1 != nil || err2 != nil {
			continue
		}
		parsedAccts = append(parsedAccts, parsed{
			created: c.UTC(), updated: u.UTC(),
			disabled: a.Status == "disabled", ok: true,
		})
	}
	now := s.now().UTC()
	first := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -(months - 1), 0)
	points := make([]reportsdomain.GrowthPoint, 0, months)
	for m := 0; m < months; m++ {
		start := first.AddDate(0, m, 0)
		end := start.AddDate(0, 1, 0)
		var p reportsdomain.GrowthPoint
		p.Period = start.Format("2006-01")
		for _, a := range parsedAccts {
			if !a.ok {
				continue
			}
			if !a.created.Before(start) && a.created.Before(end) {
				p.New++
			}
			if a.disabled && !a.updated.Before(start) && a.updated.Before(end) {
				p.Churn++
			}
			if a.created.Before(end) && !(a.disabled && a.updated.Before(end)) {
				p.Active++
			}
		}
		points = append(points, p)
	}
	return reportsdomain.Growth{Points: points}, nil
}

// resolveRange parses YYYY-MM-DD from/to (defaults: trailing 30 days)
// into [start, end) UTC midnights. Ranges over 366 days are rejected.
func (s *Service) resolveRange(from, to string) (time.Time, time.Time, error) {
	end := s.now().UTC().Truncate(24*time.Hour).AddDate(0, 0, 1)
	start := end.AddDate(0, 0, -reportsdomain.DefaultRangeDays)
	if from != "" {
		t, err := time.Parse("2006-01-02", from)
		if err != nil {
			return time.Time{}, time.Time{}, reportsdomain.ErrBadRange
		}
		start = t.UTC()
	}
	if to != "" {
		t, err := time.Parse("2006-01-02", to)
		if err != nil {
			return time.Time{}, time.Time{}, reportsdomain.ErrBadRange
		}
		end = t.UTC().AddDate(0, 0, 1)
	}
	if !start.Before(end) || end.Sub(start).Hours()/24 > reportsdomain.MaxRangeDays {
		return time.Time{}, time.Time{}, reportsdomain.ErrBadRange
	}
	return start, end, nil
}

func daySkeleton(start, end time.Time) []reportsdomain.RevenuePoint {
	out := make([]reportsdomain.RevenuePoint, 0, int(end.Sub(start).Hours()/24))
	for d := start; d.Before(end); d = d.AddDate(0, 0, 1) {
		out = append(out, reportsdomain.RevenuePoint{Period: d.Format("2006-01-02")})
	}
	return out
}

func monthSkeleton(start, end time.Time) []reportsdomain.RevenuePoint {
	first := time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, time.UTC)
	last := time.Date(end.Year(), end.Month(), 1, 0, 0, 0, 0, time.UTC)
	if !end.After(last) {
		last = last.AddDate(0, -1, 0)
	}
	out := make([]reportsdomain.RevenuePoint, 0)
	for m := first; !m.After(last); m = m.AddDate(0, 1, 0) {
		out = append(out, reportsdomain.RevenuePoint{Period: m.Format("2006-01")})
	}
	return out
}
