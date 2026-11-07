// Package infrastructure implements topup ports on sqlc and the billing
// context service.
package infrastructure

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"

	billapp "boilerplate/api/internal/billing/application"
	billdomain "boilerplate/api/internal/billing/domain"
	"boilerplate/api/internal/platform/db/sqlcgen"
	topupdomain "boilerplate/api/internal/topup/domain"
)

// Store implements AccountStore, PriceStore, TopupStore, and InvoiceStore.
type Store struct {
	q *sqlcgen.Queries
}

// NewStore builds a Store.
func NewStore(q *sqlcgen.Queries) *Store {
	return &Store{q: q}
}

// GetAccount returns the top-up slice of a PPPoE account.
func (s *Store) GetAccount(ctx context.Context, id string) (*topupdomain.Account, error) {
	row, err := s.q.GetBillingPPPoE(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, topupdomain.ErrAccountNotFound
		}
		return nil, err
	}
	return &topupdomain.Account{
		ID: row.ID, CustomerID: row.CustomerID,
		PackageID: row.PackageID, CreatedAt: row.CreatedAt,
	}, nil
}

// GetPackage returns the top-up slice of a package.
func (s *Store) GetPackage(ctx context.Context, id string) (*topupdomain.Package, error) {
	row, err := s.q.GetBillingPackage(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, topupdomain.ErrAccountNotFound
		}
		return nil, err
	}
	pkg := &topupdomain.Package{ID: row.ID, QuotaReset: row.QuotaReset}
	if row.QuotaBytes.Valid {
		q := row.QuotaBytes.Int64
		pkg.QuotaBytes = &q
	}
	return pkg, nil
}

// PricePerGB reads topup_price_per_gb, defaulting to 10000 when the
// setting is missing or invalid (a bad setting must never block sales).
func (s *Store) PricePerGB(ctx context.Context) (int64, error) {
	row, err := s.q.GetBillingSetting(ctx, topupdomain.SettingsKeyPricePerGB)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return topupdomain.DefaultPricePerGB, nil
		}
		return 0, err
	}
	n, err := strconv.ParseInt(strings.TrimSpace(row.Value), 10, 64)
	if err != nil || n <= 0 {
		return topupdomain.DefaultPricePerGB, nil
	}
	return n, nil
}

// FindByInvoice returns the top-up credited for an invoice, or (nil, nil).
func (s *Store) FindByInvoice(ctx context.Context, invoiceID string) (*topupdomain.Topup, error) {
	row, err := s.q.GetQuotaTopupByInvoice(ctx, sql.NullString{String: invoiceID, Valid: true})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	created, err := time.Parse(time.RFC3339, row.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &topupdomain.Topup{
		ID: row.ID, PPPoEAccountID: row.PppoeAccountID, Bytes: row.Bytes,
		PeriodStart: row.PeriodStart, InvoiceID: row.InvoiceID.String, CreatedAt: created,
	}, nil
}

// Insert persists a quota_topups row.
func (s *Store) Insert(ctx context.Context, t *topupdomain.Topup) error {
	_, err := s.q.CreateQuotaTopup(ctx, sqlcgen.CreateQuotaTopupParams{
		ID:             t.ID,
		PppoeAccountID: t.PPPoEAccountID,
		Bytes:          t.Bytes,
		PeriodStart:    t.PeriodStart,
		InvoiceID:      sql.NullString{String: t.InvoiceID, Valid: t.InvoiceID != ""},
		CreatedAt:      t.CreatedAt.UTC().Format(time.RFC3339),
	})
	return err
}

// SumByAccountPeriod sums top-up bytes in one bucket.
func (s *Store) SumByAccountPeriod(ctx context.Context, accountID, periodStart string) (int64, error) {
	v, err := s.q.SumQuotaTopupsByAccountPeriod(ctx, sqlcgen.SumQuotaTopupsByAccountPeriodParams{
		PppoeAccountID: accountID,
		PeriodStart:    periodStart,
	})
	if err != nil {
		return 0, err
	}
	// sqlc types COALESCE(SUM()) as any; normalize the driver shapes.
	switch n := v.(type) {
	case int64:
		return n, nil
	case int32:
		return int64(n), nil
	default:
		return 0, nil
	}
}

// GetInvoice returns the paid-detection slice of an invoice.
func (s *Store) GetInvoice(ctx context.Context, id string) (*topupdomain.Invoice, error) {
	row, err := s.q.GetBillingInvoice(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, topupdomain.ErrInvoiceNotFound
		}
		return nil, err
	}
	return &topupdomain.Invoice{
		ID: row.ID, PPPoEAccountID: row.PppoeAccountID,
		Status: row.Status, Paid: row.Status == billdomain.StatusPaid,
	}, nil
}

// ListItems returns the lines of an invoice.
func (s *Store) ListItems(ctx context.Context, invoiceID string) ([]topupdomain.Item, error) {
	rows, err := s.q.ListBillingInvoiceItems(ctx, invoiceID)
	if err != nil {
		return nil, err
	}
	out := make([]topupdomain.Item, 0, len(rows))
	for _, row := range rows {
		out = append(out, topupdomain.Item{
			Description: row.Description, Qty: row.Qty, Price: row.Price,
		})
	}
	return out, nil
}

// BillingAdapter implements InvoiceCreator on the billing service, reusing
// its numbering, validation, and totals.
type BillingAdapter struct {
	svc *billapp.Service
}

// NewBillingAdapter builds a BillingAdapter.
func NewBillingAdapter(svc *billapp.Service) *BillingAdapter {
	return &BillingAdapter{svc: svc}
}

// Create builds an unpaid invoice for the account.
func (a *BillingAdapter) Create(ctx context.Context, pppoeID string, items []topupdomain.ItemInput, dueDate string) (topupdomain.CreatedInvoice, error) {
	in := make([]billapp.ItemInput, 0, len(items))
	for _, it := range items {
		in = append(in, billapp.ItemInput{Description: it.Description, Qty: it.Qty, Price: it.Price})
	}
	dto, err := a.svc.Create(ctx, pppoeID, in, dueDate)
	if err != nil {
		return topupdomain.CreatedInvoice{}, err
	}
	return topupdomain.CreatedInvoice{ID: dto.ID, Number: dto.Number, Total: dto.Total}, nil
}
