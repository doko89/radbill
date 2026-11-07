package infrastructure

import (
	"context"

	billapp "boilerplate/api/internal/billing/application"
	topupapp "boilerplate/api/internal/topup/application"
)

// PaidHook implements billing PaidNotifier: every settled manual payment
// offers its invoice to top-up apply-on-paid, which is itself selective
// (non-top-up invoices are silent no-ops) and idempotent.
type PaidHook struct {
	svc *topupapp.Service
}

// NewPaidHook builds a PaidHook.
func NewPaidHook(svc *topupapp.Service) *PaidHook {
	return &PaidHook{svc: svc}
}

// NotifyPaid credits the top-up when the settled invoice is a paid top-up.
func (h *PaidHook) NotifyPaid(ctx context.Context, e billapp.PaidEvent) error {
	return h.svc.ApplyOnInvoicePaid(ctx, e.InvoiceID)
}
