// Package application hosts top-up use cases: portal invoice creation
// and idempotent apply-on-paid.
package application

import (
	"context"
	"time"

	"boilerplate/api/internal/shared/id"
	topupdomain "boilerplate/api/internal/topup/domain"
)

// dueDaysAfterIssue is the top-up invoice due offset (same as scheduler's).
const dueDaysAfterIssue = 7

// Deps wires a Service. All fields are required.
type Deps struct {
	Accounts topupdomain.AccountStore
	Prices   topupdomain.PriceStore
	Topups   topupdomain.TopupStore
	Invoices topupdomain.InvoiceStore
	Billing  topupdomain.InvoiceCreator
	Events   topupdomain.Emitter
}

// Service implements top-up use cases.
type Service struct {
	deps Deps
	now  func() time.Time
}

// NewService builds a Service.
func NewService(d Deps) *Service {
	return &Service{deps: d, now: time.Now}
}

// SetNow overrides the clock (tests).
func (s *Service) SetNow(fn func() time.Time) { s.now = fn }

// CreateInvoice validates a portal top-up request and builds its unpaid
// invoice; the customer pays through the existing Bayar flow.
//
// Decisions (product, documented): the request is NOT blocked by other
// unpaid invoices — a top-up is itself a way to pay — and any preset may
// be bought repeatedly (each purchase is a new invoice + quota_topups row).
func (s *Service) CreateInvoice(ctx context.Context, customerID, accountID string, bytes int64) (topupdomain.CreatedInvoice, error) {
	if !topupdomain.IsPreset(bytes) {
		return topupdomain.CreatedInvoice{}, topupdomain.ErrBadPreset
	}
	acct, err := s.deps.Accounts.GetAccount(ctx, accountID)
	if err != nil {
		return topupdomain.CreatedInvoice{}, err
	}
	if customerID != "" && acct.CustomerID != customerID {
		return topupdomain.CreatedInvoice{}, topupdomain.ErrAccountNotFound
	}
	pkg, err := s.deps.Accounts.GetPackage(ctx, acct.PackageID)
	if err != nil {
		return topupdomain.CreatedInvoice{}, err
	}
	if pkg.QuotaBytes == nil {
		return topupdomain.CreatedInvoice{}, topupdomain.ErrNoQuotaPackage
	}
	price, err := s.deps.Prices.PricePerGB(ctx)
	if err != nil {
		return topupdomain.CreatedInvoice{}, err
	}
	now := s.now().UTC()
	total := topupdomain.PriceForBytes(bytes, price)
	due := now.AddDate(0, 0, dueDaysAfterIssue).Format("2006-01-02")
	return s.deps.Billing.Create(ctx, accountID, []topupdomain.ItemInput{
		{Description: topupdomain.ItemDescription(bytes), Qty: 1, Price: total},
	}, due)
}

// ApplyOnInvoicePaid credits the running usage bucket when a top-up invoice
// settles. It is called best-effort from the billing paid hook (manual
// payments) and the kriskris ConfirmPaid hook (QRIS payments).
//
// Idempotent and selective: unknown invoices error, unpaid invoices and
// non-top-up invoices (no "Top-up kuota N GB" line) are silent no-ops, and
// an invoice that already produced a quota_topups row is not credited
// twice. period_start follows the package quota_reset mode (D1 contract).
func (s *Service) ApplyOnInvoicePaid(ctx context.Context, invoiceID string) error {
	inv, err := s.deps.Invoices.GetInvoice(ctx, invoiceID)
	if err != nil {
		return err
	}
	if !inv.Paid {
		return nil
	}
	existing, err := s.deps.Topups.FindByInvoice(ctx, invoiceID)
	if err != nil {
		return err
	}
	if existing != nil {
		return nil
	}
	items, err := s.deps.Invoices.ListItems(ctx, invoiceID)
	if err != nil {
		return err
	}
	var bytes int64
	for _, it := range items {
		if b, ok := topupdomain.ParseItemDescription(it.Description); ok {
			bytes = b
			break
		}
	}
	if bytes == 0 {
		return nil // not a top-up invoice
	}
	acct, err := s.deps.Accounts.GetAccount(ctx, inv.PPPoEAccountID)
	if err != nil {
		return err
	}
	pkg, err := s.deps.Accounts.GetPackage(ctx, acct.PackageID)
	if err != nil {
		return err
	}
	now := s.now().UTC()
	topup, err := topupdomain.NewTopup(
		id.New(), acct.ID, bytes,
		topupdomain.BucketStart(pkg.QuotaReset, acct.CreatedAt, now),
		invoiceID, now,
	)
	if err != nil {
		return err
	}
	if err := s.deps.Topups.Insert(ctx, topup); err != nil {
		return err
	}
	s.deps.Events.Emit(ctx, "pppoe", "upsert", acct.ID)
	return nil
}
