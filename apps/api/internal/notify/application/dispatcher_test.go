package application_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	notifyapp "boilerplate/api/internal/notify/application"
	notifydomain "boilerplate/api/internal/notify/domain"
	notifyinfra "boilerplate/api/internal/notify/infrastructure"
	"boilerplate/api/internal/platform/db"
	"boilerplate/api/internal/platform/db/sqlcgen"
)

type fakeSettings struct {
	snap notifyapp.Snapshot
	err  error
}

func (f *fakeSettings) Snapshot(_ context.Context) (notifyapp.Snapshot, error) {
	if f.err != nil {
		return notifyapp.Snapshot{}, f.err
	}
	return f.snap, nil
}

type captureSender struct {
	to      string
	subject string
	body    string
	err     error
}

func (c *captureSender) Send(_ context.Context, to, subject, body string) error {
	c.to, c.subject, c.body = to, subject, body
	return c.err
}

func openLogRepo(t *testing.T) *notifyinfra.SQLLogRepository {
	t.Helper()
	pool, err := db.Open("sqlite", ":memory:", "")
	if err != nil {
		t.Fatalf("sqlite unavailable: %v", err)
	}
	if err := db.Migrate(context.Background(), pool, "sqlite"); err != nil {
		pool.Close()
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	return notifyinfra.NewSQLLogRepository(sqlcgen.New(pool))
}

func logDispatcher(settings *fakeSettings, logs *notifyinfra.SQLLogRepository, logSnd *captureSender, smtpSnd *captureSender) *notifyapp.Dispatcher {
	return notifyapp.NewDispatcher(settings, logs, logSnd, func(notifyapp.SMTPConfig) notifydomain.Sender {
		return smtpSnd
	})
}

func TestNotifyTemplateAndLogProvider(t *testing.T) {
	logs := openLogRepo(t)
	settings := &fakeSettings{snap: notifyapp.Snapshot{
		Provider: "log", Company: "Radbill",
		Templates: map[string]string{"invoice_issued": "Halo {{.Nama}}, {{.Nomor}} Rp{{.Total}}."},
	}}
	logSnd, smtpSnd := &captureSender{}, &captureSender{}
	d := logDispatcher(settings, logs, logSnd, smtpSnd)

	data := map[string]string{"Nama": "Budi", "Nomor": "INV-1", "Total": "50000", "JatuhTempo": "2026-12-01"}
	if err := d.InvoiceIssued(context.Background(), "0812", "ppp1", "inv1", data); err != nil {
		t.Fatal(err)
	}
	if logSnd.body != "Halo Budi, INV-1 Rp50000." {
		t.Fatalf("body = %q", logSnd.body)
	}
	if logSnd.to != "0812" || !strings.Contains(logSnd.subject, "INV-1") {
		t.Fatalf("to=%q subject=%q", logSnd.to, logSnd.subject)
	}
	if smtpSnd.body != "" {
		t.Fatalf("smtp sender used under log provider: %q", smtpSnd.body)
	}
}

func TestNotifyWritesLogRow(t *testing.T) {
	pool, err := db.Open("sqlite", ":memory:", "")
	if err != nil {
		t.Fatalf("sqlite unavailable: %v", err)
	}
	defer pool.Close()
	if err := db.Migrate(context.Background(), pool, "sqlite"); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	q := sqlcgen.New(pool)
	settings := &fakeSettings{snap: notifyapp.Snapshot{Provider: "log", Company: "C"}}
	d := notifyapp.NewDispatcher(settings, notifyinfra.NewSQLLogRepository(q), &captureSender{},
		func(notifyapp.SMTPConfig) notifydomain.Sender { return &captureSender{} })

	if err := d.Paid(context.Background(), "0812", "ppp1", "inv9", map[string]string{"Nomor": "INV-9", "Total": "100"}); err != nil {
		t.Fatal(err)
	}
	rows, err := q.ListNotificationLog(context.Background(), sqlcgen.ListNotificationLogParams{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("log rows = %d, want 1", len(rows))
	}
	row := rows[0]
	if row.Kind != "paid" || row.Target != "0812" || row.Status != "sent" {
		t.Fatalf("log row = %+v", row)
	}
	if !row.InvoiceID.Valid || row.InvoiceID.String != "inv9" {
		t.Fatalf("log invoice_id = %+v", row.InvoiceID)
	}
}

func TestNotifySMTPFallbackToLog(t *testing.T) {
	pool, err := db.Open("sqlite", ":memory:", "")
	if err != nil {
		t.Fatalf("sqlite unavailable: %v", err)
	}
	defer pool.Close()
	if err := db.Migrate(context.Background(), pool, "sqlite"); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	q := sqlcgen.New(pool)
	settings := &fakeSettings{snap: notifyapp.Snapshot{Provider: "smtp", Company: "C", SMTPHost: "mail.example"}}
	logSnd := &captureSender{}
	smtpSnd := &captureSender{err: errors.New("dial refused")}
	d := notifyapp.NewDispatcher(settings, notifyinfra.NewSQLLogRepository(q), logSnd,
		func(notifyapp.SMTPConfig) notifydomain.Sender { return smtpSnd })

	if err := d.Reminder(context.Background(), "0812", "ppp1", "inv2", map[string]string{"Nomor": "INV-2"}); err != nil {
		t.Fatal(err)
	}
	if logSnd.body == "" {
		t.Fatal("log fallback did not receive the message after smtp failure")
	}
	rows, err := q.ListNotificationLog(context.Background(), sqlcgen.ListNotificationLogParams{Limit: 10})
	if err != nil || len(rows) != 1 {
		t.Fatalf("log rows = %d,%v", len(rows), err)
	}
	if rows[0].Status != "sent" || !strings.Contains(rows[0].Error.String, "dial refused") {
		t.Fatalf("fallback not recorded: %+v", rows[0])
	}
}

func TestNotifyEmptyTargetFallsBack(t *testing.T) {
	logs := openLogRepo(t)
	settings := &fakeSettings{snap: notifyapp.Snapshot{Provider: "log", Company: "C"}}
	logSnd := &captureSender{}
	d := logDispatcher(settings, logs, logSnd, &captureSender{})
	if err := d.Isolated(context.Background(), "", "ppp1", "inv3", map[string]string{"Username": "budi01"}); err != nil {
		t.Fatal(err)
	}
	if logSnd.to != "budi01" {
		t.Fatalf("target = %q, want username fallback", logSnd.to)
	}
}
