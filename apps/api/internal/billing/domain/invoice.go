// Package domain holds the billing bounded-context core.
package domain

import (
	"context"
	"errors"
	"time"

	"boilerplate/api/internal/shared/apperr"
)

// Invoice statuses.
const (
	StatusUnpaid = "unpaid"
	StatusPaid   = "paid"
	StatusVoid   = "void"
)

// Payment methods.
const (
	MethodKriskris = "kriskris"
	MethodManual   = "manual"
)

var (
	// ErrInvoiceNotFound is returned when the invoice does not exist.
	ErrInvoiceNotFound = errors.New("invoice not found")
	// ErrAlreadyPaid is returned when paying or voiding a paid invoice.
	ErrAlreadyPaid = errors.New("invoice already paid")
	// ErrInvoiceVoid is returned when paying a void invoice.
	ErrInvoiceVoid = errors.New("invoice is void")
	// ErrUnknownPPPoE is returned when pppoe_account_id does not exist.
	ErrUnknownPPPoE = errors.New("unknown pppoe account")
	// ErrAmountMismatch is returned when the paid amount differs from total.
	ErrAmountMismatch = errors.New("amount differs from invoice total")
	// ErrNumberTaken is returned on duplicate invoice numbers (retry).
	ErrNumberTaken = errors.New("invoice number already taken")
)

// Invoice is a bill for one PPPoE account.
type Invoice struct {
	ID             string
	Number         string
	PPPoEAccountID string
	CustomerID     string
	Total          int64
	DueDate        string
	Status         string
	IssuedAt       time.Time
	PaidAt         *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// NewInvoice builds a validated unpaid invoice. dueDate is YYYY-MM-DD.
func NewInvoice(id, number, pppoeID, customerID string, total int64, dueDate string, now time.Time) (*Invoice, error) {
	if id == "" {
		return nil, apperr.Invalid("id", "required")
	}
	if number == "" {
		return nil, apperr.Invalid("number", "required")
	}
	if pppoeID == "" {
		return nil, apperr.Invalid("pppoe_account_id", "required")
	}
	if customerID == "" {
		return nil, apperr.Invalid("customer_id", "required")
	}
	if total <= 0 {
		return nil, apperr.Invalid("total", "must be > 0")
	}
	if _, err := time.Parse("2006-01-02", dueDate); err != nil {
		return nil, apperr.Invalid("due_date", "must be YYYY-MM-DD")
	}
	return &Invoice{
		ID: id, Number: number, PPPoEAccountID: pppoeID, CustomerID: customerID,
		Total: total, DueDate: dueDate, Status: StatusUnpaid,
		IssuedAt: now, CreatedAt: now, UpdatedAt: now,
	}, nil
}

// MarkPaid moves an unpaid invoice to paid.
func (inv *Invoice) MarkPaid(paidAt time.Time) error {
	switch inv.Status {
	case StatusUnpaid:
		inv.Status = StatusPaid
		inv.PaidAt = &paidAt
		inv.UpdatedAt = paidAt
		return nil
	case StatusPaid:
		return ErrAlreadyPaid
	default:
		return ErrInvoiceVoid
	}
}

// Void moves an unpaid invoice to void (idempotent for void).
func (inv *Invoice) Void(now time.Time) error {
	switch inv.Status {
	case StatusUnpaid:
		inv.Status = StatusVoid
		inv.UpdatedAt = now
		return nil
	case StatusVoid:
		return nil
	default:
		return ErrAlreadyPaid
	}
}

// InvoiceItem is one line of an invoice.
type InvoiceItem struct {
	ID          string
	InvoiceID   string
	Description string
	Qty         int64
	Price       int64
}

// NewItem builds a validated invoice line.
func NewItem(id, invoiceID, description string, qty, price int64) (*InvoiceItem, error) {
	if description == "" {
		return nil, apperr.Invalid("description", "required")
	}
	if len(description) > 200 {
		return nil, apperr.Invalid("description", "max 200 characters")
	}
	if qty < 1 {
		return nil, apperr.Invalid("qty", "must be >= 1")
	}
	if price < 0 {
		return nil, apperr.Invalid("price", "must be >= 0")
	}
	return &InvoiceItem{ID: id, InvoiceID: invoiceID, Description: description, Qty: qty, Price: price}, nil
}

// Payment records money received for an invoice.
type Payment struct {
	ID        string
	InvoiceID string
	Amount    int64
	Method    string
	AttemptID string
	PaidAt    time.Time
	CreatedAt time.Time
}

// PPPoERef is the slice of PPPoE state billing needs for post-payment updates.
type PPPoERef struct {
	ID         string
	CustomerID string
	PackageID  string
	Status     string
	ExpiresAt  *time.Time
}

// Repository persists invoices, items, and payments. Implemented in infrastructure.
type Repository interface {
	CreateInvoice(ctx context.Context, inv *Invoice, items []*InvoiceItem) error
	GetInvoice(ctx context.Context, id string) (*Invoice, error)
	ListInvoices(ctx context.Context, status string, limit, offset int) ([]*Invoice, error)
	CountInvoices(ctx context.Context, status string) (int64, error)
	ListItems(ctx context.Context, invoiceID string) ([]*InvoiceItem, error)
	ListPayments(ctx context.Context, invoiceID string) ([]*Payment, error)
	VoidInvoice(ctx context.Context, id string, now time.Time) (*Invoice, error)
	// PayManual records a manual payment, extends expiry, and reactivates
	// isolated accounts, all in one transaction.
	PayManual(ctx context.Context, invoiceID string, amount int64, newExpiresAt time.Time, now time.Time) (*Invoice, error)
}

// PPPoELookup resolves PPPoE references for billing. Implemented in infrastructure.
type PPPoELookup interface {
	GetPPPoE(ctx context.Context, id string) (*PPPoERef, error)
}

// PackageLookup resolves package validity for billing. Implemented in infrastructure.
type PackageLookup interface {
	// ValiditySeconds returns package validity or nil when unset.
	ValiditySeconds(ctx context.Context, packageID string) (*int64, error)
}
