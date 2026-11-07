// Package application hosts the notify dispatcher: templated messages over
// the configured provider with every attempt recorded in notification_log.
package application

import (
	"context"
	"time"

	notifydomain "boilerplate/api/internal/notify/domain"
	"boilerplate/api/internal/shared/id"
)

// Snapshot is the settings view the dispatcher needs: provider selection,
// company name, SMTP config, and one template per kind.
type Snapshot struct {
	Provider  string // log|smtp
	Company   string
	From      string
	SMTPHost  string
	SMTPPort  int
	SMTPUser  string
	SMTPPass  string
	Templates map[string]string
}

// Settings supplies snapshots. Implemented in infrastructure on top of the
// settings context; faked in tests.
type Settings interface {
	Snapshot(ctx context.Context) (Snapshot, error)
}

// Dispatcher renders templates and sends through the configured provider.
// Every attempt is recorded via the log repository.
type Dispatcher struct {
	settings  Settings
	logs      notifydomain.LogRepository
	logSender notifydomain.Sender
	newSMTP   func(cfg SMTPConfig) notifydomain.Sender
	now       func() time.Time
}

// SMTPConfig carries the SMTP dial settings for one send.
type SMTPConfig struct {
	Host string
	Port int
	User string
	Pass string
	From string
}

// NewDispatcher builds a Dispatcher. newSMTP constructs the smtp provider
// (injected for tests).
func NewDispatcher(settings Settings, logs notifydomain.LogRepository, logSender notifydomain.Sender, newSMTP func(SMTPConfig) notifydomain.Sender) *Dispatcher {
	return &Dispatcher{settings: settings, logs: logs, logSender: logSender, newSMTP: newSMTP, now: time.Now}
}

// Notify sends one templated message and records the attempt. It only fails
// when the log entry itself cannot be written; delivery problems are captured
// in the entry's status/error columns instead.
func (d *Dispatcher) Notify(ctx context.Context, kind, target, pppoeID, invoiceID string, data map[string]string) error {
	snap, err := d.settings.Snapshot(ctx)
	if err != nil {
		snap = Snapshot{Provider: "log", Templates: map[string]string{}}
	}
	if data == nil {
		data = map[string]string{}
	}
	if snap.Company != "" {
		if _, ok := data["Perusahaan"]; !ok {
			data["Perusahaan"] = snap.Company
		}
	}
	tpl := snap.Templates[kind]
	if tpl == "" {
		tpl = notifydomain.DefaultTemplate(kind)
	}
	body, err := notifydomain.Render(tpl, data)
	if err != nil {
		body, _ = notifydomain.Render(notifydomain.DefaultTemplate(kind), data)
	}
	subject := notifydomain.SubjectFor(kind, data)
	if target == "" {
		target = fallbackTarget(invoiceID, data)
	}

	status := notifydomain.StatusSent
	note := ""
	if snap.Provider == "smtp" && snap.SMTPHost != "" {
		cfg := SMTPConfig{Host: snap.SMTPHost, Port: snap.SMTPPort, User: snap.SMTPUser, Pass: snap.SMTPPass, From: snap.From}
		if serr := d.newSMTP(cfg).Send(ctx, target, subject, body); serr != nil {
			note = "smtp failed, logged instead: " + serr.Error()
			_ = d.logSender.Send(ctx, target, subject, body)
		}
	} else {
		if serr := d.logSender.Send(ctx, target, subject, body); serr != nil {
			status = notifydomain.StatusFailed
			note = serr.Error()
		}
	}

	_, err = d.logs.Create(ctx, notifydomain.Entry{
		ID:             id.New(),
		Kind:           kind,
		Target:         target,
		PPPoEAccountID: pppoeID,
		InvoiceID:      invoiceID,
		Status:         status,
		Error:          note,
		CreatedAt:      d.now().UTC(),
	})
	return err
}

// InvoiceIssued notifies that an invoice was issued.
func (d *Dispatcher) InvoiceIssued(ctx context.Context, target, pppoeID, invoiceID string, data map[string]string) error {
	return d.Notify(ctx, notifydomain.KindInvoiceIssued, target, pppoeID, invoiceID, data)
}

// Reminder nudges about an overdue invoice inside the grace period.
func (d *Dispatcher) Reminder(ctx context.Context, target, pppoeID, invoiceID string, data map[string]string) error {
	return d.Notify(ctx, notifydomain.KindReminder, target, pppoeID, invoiceID, data)
}

// Isolated notifies that the service was isolated for non-payment.
func (d *Dispatcher) Isolated(ctx context.Context, target, pppoeID, invoiceID string, data map[string]string) error {
	return d.Notify(ctx, notifydomain.KindIsolated, target, pppoeID, invoiceID, data)
}

// Paid confirms a settled payment.
func (d *Dispatcher) Paid(ctx context.Context, target, pppoeID, invoiceID string, data map[string]string) error {
	return d.Notify(ctx, notifydomain.KindPaid, target, pppoeID, invoiceID, data)
}

func fallbackTarget(invoiceID string, data map[string]string) string {
	if u := data["Username"]; u != "" {
		return u
	}
	return "invoice:" + invoiceID
}
