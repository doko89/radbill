package infrastructure

import (
	"context"

	billapp "boilerplate/api/internal/billing/application"
	scheddomain "boilerplate/api/internal/scheduler/domain"
)

// paidStore is the slice of domain.Store the paid hook needs.
type paidStore interface {
	GetCustomer(ctx context.Context, id string) (scheddomain.CustomerInfo, error)
	GetAccount(ctx context.Context, id string) (scheddomain.BillableAccount, error)
}

// PaidHook implements billing PaidNotifier: it resolves the customer and
// account names and sends the paid template. Best-effort by contract.
type PaidHook struct {
	store  paidStore
	notify scheddomain.Notifier
	config scheddomain.ConfigReader
}

// NewPaidHook builds a PaidHook.
func NewPaidHook(store paidStore, notify scheddomain.Notifier, config scheddomain.ConfigReader) *PaidHook {
	return &PaidHook{store: store, notify: notify, config: config}
}

// NotifyPaid sends the paid confirmation for a settled manual payment.
func (h *PaidHook) NotifyPaid(ctx context.Context, e billapp.PaidEvent) error {
	cust, err := h.store.GetCustomer(ctx, e.CustomerID)
	if err != nil {
		return err
	}
	acct, err := h.store.GetAccount(ctx, e.PPPoEAccountID)
	if err != nil {
		return err
	}
	cfg, err := h.config.BillingConfig(ctx)
	if err != nil {
		return err
	}
	return h.notify.Paid(ctx, cust.Phone, e.PPPoEAccountID, e.InvoiceID, map[string]string{
		"Nama": cust.Name, "Nomor": e.Number, "Total": scheddomain.FormatIDR(e.Total),
		"JatuhTempo": e.DueDate, "Username": acct.Username, "Perusahaan": cfg.Company,
	})
}
