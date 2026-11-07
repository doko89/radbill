package infrastructure

import (
	"context"
	"time"

	billapp "boilerplate/api/internal/billing/application"
	pppoedomain "boilerplate/api/internal/pppoe/domain"
	pppoeinfra "boilerplate/api/internal/pppoe/infrastructure"
	scheddomain "boilerplate/api/internal/scheduler/domain"
	setdomain "boilerplate/api/internal/settings/domain"
)

// BillingAdapter implements domain.Billing on top of the billing service,
// reusing its numbering, validation, and totals.
type BillingAdapter struct {
	svc *billapp.Service
}

// NewBillingAdapter builds a BillingAdapter.
func NewBillingAdapter(svc *billapp.Service) *BillingAdapter {
	return &BillingAdapter{svc: svc}
}

// Create builds an unpaid invoice for the account.
func (a *BillingAdapter) Create(ctx context.Context, pppoeID string, items []scheddomain.Item, dueDate string) (scheddomain.CreatedInvoice, error) {
	in := make([]billapp.ItemInput, 0, len(items))
	for _, it := range items {
		in = append(in, billapp.ItemInput{Description: it.Description, Qty: it.Qty, Price: it.Price})
	}
	dto, err := a.svc.Create(ctx, pppoeID, in, dueDate)
	if err != nil {
		return scheddomain.CreatedInvoice{}, err
	}
	return scheddomain.CreatedInvoice{ID: dto.ID, Number: dto.Number, Total: dto.Total, DueDate: dto.DueDate}, nil
}

// PPPoEAdapter implements domain.PPPoE on top of the pppoe repository. The
// repository emits the cache-refresh event on update.
type PPPoEAdapter struct {
	repo *pppoeinfra.SQLRepository
}

// NewPPPoEAdapter builds a PPPoEAdapter.
func NewPPPoEAdapter(repo *pppoeinfra.SQLRepository) *PPPoEAdapter {
	return &PPPoEAdapter{repo: repo}
}

// Isolate moves the account to isolated. Already-isolated accounts are a
// no-op; disabled accounts fail closed.
func (a *PPPoEAdapter) Isolate(ctx context.Context, id string, now time.Time) error {
	p, err := a.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	if p.Status == pppoedomain.StatusIsolated {
		return nil
	}
	if err := p.Isolate(now); err != nil {
		return err
	}
	return a.repo.Update(ctx, p)
}

// typedReader is the slice of the settings service the adapter needs.
type typedReader interface {
	Typed(ctx context.Context) (setdomain.Typed, error)
}

// ConfigAdapter implements domain.ConfigReader on top of typed settings.
type ConfigAdapter struct {
	svc typedReader
}

// NewConfigAdapter builds a ConfigAdapter.
func NewConfigAdapter(svc typedReader) *ConfigAdapter {
	return &ConfigAdapter{svc: svc}
}

// BillingConfig maps typed settings to the scheduler view.
func (a *ConfigAdapter) BillingConfig(ctx context.Context) (scheddomain.BillingConfig, error) {
	t, err := a.svc.Typed(ctx)
	if err != nil {
		return scheddomain.BillingConfig{}, err
	}
	return scheddomain.BillingConfig{
		Mode: t.BillingCycleMode, Day: t.BillingCycleDay, Grace: t.GraceDays, Company: t.CompanyName,
	}, nil
}
