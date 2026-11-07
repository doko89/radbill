// Package domain holds the notify bounded-context core: the Sender port,
// template rendering, log entry shape, and notification kinds.
package domain

import (
	"context"
	"strings"
	"text/template"
	"time"
)

// Sender delivers one message. The log provider always succeeds; smtp is
// best-effort (G2 targets are phone numbers, informational in logs).
type Sender interface {
	Send(ctx context.Context, to, subject, body string) error
}

// Notification kinds stored in notification_log.kind.
const (
	KindInvoiceIssued = "invoice_issued"
	KindReminder      = "reminder"
	KindIsolated      = "isolated"
	KindPaid          = "paid"
)

// Delivery statuses stored in notification_log.status.
const (
	StatusSent   = "sent"
	StatusFailed = "failed"
)

// Entry is one notification_log row. Empty PPPoEAccountID/InvoiceID/Error
// mean NULL.
type Entry struct {
	ID             string
	Kind           string
	Target         string
	PPPoEAccountID string
	InvoiceID      string
	Status         string
	Error          string
	CreatedAt      time.Time
}

// LogRepository persists entries. Implemented in infrastructure.
type LogRepository interface {
	Create(ctx context.Context, e Entry) (Entry, error)
}

// Placeholders available to every template:
//
//	{{.Nama}} customer name, {{.Nomor}} invoice number, {{.Total}} amount,
//	{{.JatuhTempo}} due date, {{.Username}} PPPoE login, {{.Perusahaan}} company.
var placeholderKeys = []string{"Nama", "Nomor", "Total", "JatuhTempo", "Username", "Perusahaan"}

// Render executes tpl with data. Unknown placeholders render as "" so a
// template typo degrades the message instead of dropping it. Only a syntax
// error fails.
func Render(tpl string, data map[string]string) (string, error) {
	t, err := template.New("notify").Option("missingkey=zero").Parse(tpl)
	if err != nil {
		return "", err
	}
	full := make(map[string]string, len(placeholderKeys)+len(data))
	for k, v := range data {
		full[k] = v
	}
	var b strings.Builder
	if err := t.Execute(&b, full); err != nil {
		return "", err
	}
	return b.String(), nil
}

// DefaultTemplate returns the built-in template for kind. It mirrors the
// tpl_* seeds and is used when settings are unreadable or hold bad syntax.
func DefaultTemplate(kind string) string {
	switch kind {
	case KindReminder:
		return "Halo {{.Nama}}, tagihan {{.Nomor}} sebesar Rp{{.Total}} sudah jatuh tempo {{.JatuhTempo}}. Segera bayar agar layanan tidak diisolir. - {{.Perusahaan}}"
	case KindIsolated:
		return "Halo {{.Nama}}, layanan {{.Username}} diisolir karena tagihan {{.Nomor}} belum lunas. Bayar untuk mengaktifkan kembali. - {{.Perusahaan}}"
	case KindPaid:
		return "Halo {{.Nama}}, pembayaran tagihan {{.Nomor}} sebesar Rp{{.Total}} diterima. Terima kasih. - {{.Perusahaan}}"
	default:
		return "Halo {{.Nama}}, tagihan {{.Nomor}} sebesar Rp{{.Total}} telah terbit. Jatuh tempo {{.JatuhTempo}}. - {{.Perusahaan}}"
	}
}

// SubjectFor builds the message subject from kind and data.
func SubjectFor(kind string, data map[string]string) string {
	nomor := strings.TrimSpace(data["Nomor"])
	switch kind {
	case KindReminder:
		return "Pengingat tagihan " + nomor
	case KindIsolated:
		return "Layanan diisolir (tagihan " + nomor + ")"
	case KindPaid:
		return "Pembayaran tagihan " + nomor + " diterima"
	default:
		return "Tagihan " + nomor + " telah terbit"
	}
}
