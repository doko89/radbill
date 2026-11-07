// Package infrastructure implements billing ports on SQL.
package infrastructure

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"boilerplate/api/internal/billing/domain"
	"boilerplate/api/internal/network/notify"
	"boilerplate/api/internal/platform/db/sqlcgen"
	"boilerplate/api/internal/shared/id"
)

// SQLRepository implements domain.Repository, domain.PPPoELookup, and
// domain.PackageLookup on sqlc queries.
type SQLRepository struct {
	db     *sql.DB
	q      *sqlcgen.Queries
	events *notify.Notifier
}

// NewSQLRepository builds a SQLRepository.
func NewSQLRepository(db *sql.DB, q *sqlcgen.Queries, events *notify.Notifier) *SQLRepository {
	return &SQLRepository{db: db, q: q, events: events}
}

// CreateInvoice inserts an invoice with its items in one transaction.
func (r *SQLRepository) CreateInvoice(ctx context.Context, inv *domain.Invoice, items []*domain.InvoiceItem) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	qtx := r.q.WithTx(tx)
	if _, err := qtx.CreateBillingInvoice(ctx, sqlcgen.CreateBillingInvoiceParams{
		ID:             inv.ID,
		Number:         inv.Number,
		PppoeAccountID: inv.PPPoEAccountID,
		CustomerID:     inv.CustomerID,
		Total:          inv.Total,
		DueDate:        inv.DueDate,
		Status:         inv.Status,
		IssuedAt:       inv.IssuedAt.UTC().Format(time.RFC3339),
		PaidAt:         nullTime(inv.PaidAt),
		CreatedAt:      inv.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:      inv.UpdatedAt.UTC().Format(time.RFC3339),
	}); err != nil {
		if isUniqueViolation(err) {
			return domain.ErrNumberTaken
		}
		if isForeignKeyViolation(err) {
			return domain.ErrUnknownPPPoE
		}
		return err
	}
	for _, it := range items {
		if _, err := qtx.CreateBillingInvoiceItem(ctx, sqlcgen.CreateBillingInvoiceItemParams{
			ID:          it.ID,
			InvoiceID:   it.InvoiceID,
			Description: it.Description,
			Qty:         it.Qty,
			Price:       it.Price,
		}); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// GetInvoice returns one invoice or domain.ErrInvoiceNotFound.
func (r *SQLRepository) GetInvoice(ctx context.Context, invoiceID string) (*domain.Invoice, error) {
	row, err := r.q.GetBillingInvoice(ctx, invoiceID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrInvoiceNotFound
		}
		return nil, err
	}
	return toInvoice(row)
}

// ListInvoices returns a page of invoices (status "" = all), newest first.
func (r *SQLRepository) ListInvoices(ctx context.Context, status string, limit, offset int) ([]*domain.Invoice, error) {
	var (
		rows []sqlcgen.Invoice
		err  error
	)
	if status == "" {
		rows, err = r.q.ListBillingInvoices(ctx, sqlcgen.ListBillingInvoicesParams{
			Limit:  int32(limit),
			Offset: int32(offset),
		})
	} else {
		rows, err = r.q.ListBillingInvoicesByStatus(ctx, sqlcgen.ListBillingInvoicesByStatusParams{
			Status: status,
			Limit:  int32(limit),
			Offset: int32(offset),
		})
	}
	if err != nil {
		return nil, err
	}
	out := make([]*domain.Invoice, 0, len(rows))
	for _, row := range rows {
		inv, err := toInvoice(row)
		if err != nil {
			return nil, err
		}
		out = append(out, inv)
	}
	return out, nil
}

// CountInvoices counts invoices (status "" = all).
func (r *SQLRepository) CountInvoices(ctx context.Context, status string) (int64, error) {
	if status == "" {
		return r.q.CountBillingInvoices(ctx)
	}
	return r.q.CountBillingInvoicesByStatus(ctx, status)
}

// ListItems returns the lines of an invoice.
func (r *SQLRepository) ListItems(ctx context.Context, invoiceID string) ([]*domain.InvoiceItem, error) {
	rows, err := r.q.ListBillingInvoiceItems(ctx, invoiceID)
	if err != nil {
		return nil, err
	}
	out := make([]*domain.InvoiceItem, 0, len(rows))
	for _, row := range rows {
		out = append(out, &domain.InvoiceItem{
			ID: row.ID, InvoiceID: row.InvoiceID,
			Description: row.Description, Qty: row.Qty, Price: row.Price,
		})
	}
	return out, nil
}

// ListPayments returns the payments of an invoice.
func (r *SQLRepository) ListPayments(ctx context.Context, invoiceID string) ([]*domain.Payment, error) {
	rows, err := r.q.ListBillingPaymentsByInvoice(ctx, invoiceID)
	if err != nil {
		return nil, err
	}
	out := make([]*domain.Payment, 0, len(rows))
	for _, row := range rows {
		p, err := toPayment(row)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// VoidInvoice cancels an unpaid invoice.
func (r *SQLRepository) VoidInvoice(ctx context.Context, invoiceID string, now time.Time) (*domain.Invoice, error) {
	inv, err := r.GetInvoice(ctx, invoiceID)
	if err != nil {
		return nil, err
	}
	if err := inv.Void(now.UTC()); err != nil {
		return nil, err
	}
	row, err := r.q.UpdateBillingInvoiceStatus(ctx, sqlcgen.UpdateBillingInvoiceStatusParams{
		ID:        inv.ID,
		Status:    inv.Status,
		PaidAt:    nullTime(inv.PaidAt),
		UpdatedAt: inv.UpdatedAt.UTC().Format(time.RFC3339),
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrInvoiceNotFound
		}
		return nil, err
	}
	return toInvoice(row)
}

// PayManual records a manual payment, extends the PPPoE expiry, and
// reactivates isolated accounts, all in one transaction. It emits a
// pppoe/upsert event after commit.
func (r *SQLRepository) PayManual(ctx context.Context, invoiceID string, amount int64, newExpiresAt time.Time, now time.Time) (*domain.Invoice, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	qtx := r.q.WithTx(tx)

	row, err := qtx.GetBillingInvoice(ctx, invoiceID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrInvoiceNotFound
		}
		return nil, err
	}
	inv, err := toInvoice(row)
	if err != nil {
		return nil, err
	}
	if err := inv.MarkPaid(now.UTC()); err != nil {
		return nil, err
	}
	paidRow, err := qtx.UpdateBillingInvoiceStatus(ctx, sqlcgen.UpdateBillingInvoiceStatusParams{
		ID:        inv.ID,
		Status:    inv.Status,
		PaidAt:    nullTime(inv.PaidAt),
		UpdatedAt: inv.UpdatedAt.UTC().Format(time.RFC3339),
	})
	if err != nil {
		return nil, err
	}
	stamp := now.UTC().Format(time.RFC3339)
	if _, err := qtx.CreateBillingPayment(ctx, sqlcgen.CreateBillingPaymentParams{
		ID:        id.New(),
		InvoiceID: inv.ID,
		Amount:    amount,
		Method:    domain.MethodManual,
		AttemptID: sql.NullString{},
		PaidAt:    stamp,
		CreatedAt: stamp,
	}); err != nil {
		return nil, err
	}
	pppoe, err := qtx.GetBillingPPPoE(ctx, inv.PPPoEAccountID)
	if err != nil {
		return nil, err
	}
	status := pppoe.Status
	if status == "isolated" {
		status = "active"
	}
	if _, err := qtx.UpdateBillingPPPoE(ctx, sqlcgen.UpdateBillingPPPoEParams{
		ID:         pppoe.ID,
		CustomerID: pppoe.CustomerID,
		PackageID:  pppoe.PackageID,
		NasID:      pppoe.NasID,
		Username:   pppoe.Username,
		Password:   pppoe.Password,
		Status:     status,
		ExpiresAt:  sql.NullString{String: newExpiresAt.UTC().Format(time.RFC3339), Valid: true},
		StaticIp:   pppoe.StaticIp,
		UpdatedAt:  stamp,
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	r.events.Emit(ctx, notify.TypePPPoE, notify.ActionUpsert, inv.PPPoEAccountID)
	return toInvoice(paidRow)
}

// GetPPPoE resolves the billing slice of a PPPoE account.
func (r *SQLRepository) GetPPPoE(ctx context.Context, pid string) (*domain.PPPoERef, error) {
	row, err := r.q.GetBillingPPPoE(ctx, pid)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrUnknownPPPoE
		}
		return nil, err
	}
	ref := &domain.PPPoERef{
		ID: row.ID, CustomerID: row.CustomerID, PackageID: row.PackageID, Status: row.Status,
	}
	if row.ExpiresAt.Valid {
		exp, err := time.Parse(time.RFC3339, row.ExpiresAt.String)
		if err != nil {
			return nil, fmt.Errorf("parse pppoe expires_at: %w", err)
		}
		ref.ExpiresAt = &exp
	}
	return ref, nil
}

// ValiditySeconds returns the package validity or nil when unset.
func (r *SQLRepository) ValiditySeconds(ctx context.Context, packageID string) (*int64, error) {
	row, err := r.q.GetBillingPackage(ctx, packageID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if !row.ValiditySeconds.Valid {
		return nil, nil
	}
	v := row.ValiditySeconds.Int64
	return &v, nil
}

func toInvoice(row sqlcgen.Invoice) (*domain.Invoice, error) {
	issued, err := time.Parse(time.RFC3339, row.IssuedAt)
	if err != nil {
		return nil, fmt.Errorf("parse invoice issued_at: %w", err)
	}
	created, err := time.Parse(time.RFC3339, row.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse invoice created_at: %w", err)
	}
	updated, err := time.Parse(time.RFC3339, row.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse invoice updated_at: %w", err)
	}
	inv := &domain.Invoice{
		ID: row.ID, Number: row.Number,
		PPPoEAccountID: row.PppoeAccountID, CustomerID: row.CustomerID,
		Total: row.Total, DueDate: row.DueDate, Status: row.Status,
		IssuedAt: issued, CreatedAt: created, UpdatedAt: updated,
	}
	if row.PaidAt.Valid {
		paid, err := time.Parse(time.RFC3339, row.PaidAt.String)
		if err != nil {
			return nil, fmt.Errorf("parse invoice paid_at: %w", err)
		}
		inv.PaidAt = &paid
	}
	return inv, nil
}

func toPayment(row sqlcgen.Payment) (*domain.Payment, error) {
	paid, err := time.Parse(time.RFC3339, row.PaidAt)
	if err != nil {
		return nil, fmt.Errorf("parse payment paid_at: %w", err)
	}
	created, err := time.Parse(time.RFC3339, row.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse payment created_at: %w", err)
	}
	return &domain.Payment{
		ID: row.ID, InvoiceID: row.InvoiceID, Amount: row.Amount,
		Method: row.Method, AttemptID: row.AttemptID.String,
		PaidAt: paid, CreatedAt: created,
	}, nil
}

func nullTime(t *time.Time) sql.NullString {
	if t == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: t.UTC().Format(time.RFC3339), Valid: true}
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") ||
		strings.Contains(msg, "unique constraint") ||
		strings.Contains(msg, "duplicate key")
}

func isForeignKeyViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "FOREIGN KEY constraint failed") ||
		strings.Contains(msg, "foreign key constraint") ||
		strings.Contains(msg, "violates foreign key")
}
