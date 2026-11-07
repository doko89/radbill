package infrastructure

import (
	"context"

	notifyapp "boilerplate/api/internal/notify/application"
	notifydomain "boilerplate/api/internal/notify/domain"
	setdomain "boilerplate/api/internal/settings/domain"
)

// typedReader is the slice of the settings service the adapter needs.
type typedReader interface {
	Typed(ctx context.Context) (setdomain.Typed, error)
}

// SettingsAdapter implements application.Settings on top of the settings
// context's typed view.
type SettingsAdapter struct {
	svc typedReader
}

// NewSettingsAdapter builds a SettingsAdapter.
func NewSettingsAdapter(svc typedReader) *SettingsAdapter {
	return &SettingsAdapter{svc: svc}
}

// Snapshot maps the typed settings to the dispatcher view.
func (a *SettingsAdapter) Snapshot(ctx context.Context) (notifyapp.Snapshot, error) {
	t, err := a.svc.Typed(ctx)
	if err != nil {
		return notifyapp.Snapshot{}, err
	}
	return notifyapp.Snapshot{
		Provider: t.NotifProvider,
		Company:  t.CompanyName,
		From:     t.NotifFrom,
		SMTPHost: t.SMTPHost,
		SMTPPort: t.SMTPPort,
		SMTPUser: t.SMTPUser,
		SMTPPass: t.SMTPPass,
		Templates: map[string]string{
			notifydomain.KindInvoiceIssued: t.TplIssued,
			notifydomain.KindReminder:      t.TplReminder,
			notifydomain.KindIsolated:      t.TplIsolated,
			notifydomain.KindPaid:          t.TplPaid,
		},
	}, nil
}
