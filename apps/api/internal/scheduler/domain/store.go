package domain

import (
	"context"
	"time"
)

// BillingConfig is the typed billing subset of settings the jobs need.
type BillingConfig struct {
	Mode    string // activation|fixed
	Day     int    // fixed-mode anchor 1..31
	Grace   int    // grace days after due date before isolation
	Company string
}

// ConfigReader supplies the billing config. Implemented in infrastructure
// on top of the settings context.
type ConfigReader interface {
	BillingConfig(ctx context.Context) (BillingConfig, error)
}

// BillableAccount is the scheduler slice of a PPPoE account.
type BillableAccount struct {
	ID         string
	CustomerID string
	PackageID  string
	Username   string
	Status     string
	CreatedAt  time.Time
}

// PackageInfo is the slice of a package needed for invoicing.
type PackageInfo struct {
	ID    string
	Name  string
	Price int64
}

// CustomerInfo is the slice of a customer needed for notifications.
type CustomerInfo struct {
	ID    string
	Name  string
	Phone string
}

// InvoiceInfo is the slice of an invoice needed for overdue handling.
type InvoiceInfo struct {
	ID             string
	Number         string
	PPPoEAccountID string
	CustomerID     string
	Total          int64
	DueDate        string // YYYY-MM-DD
}

// Item is one invoice line the generate job creates.
type Item struct {
	Description string
	Qty         int64
	Price       int64
}

// CreatedInvoice is the slice of a created invoice the job needs.
type CreatedInvoice struct {
	ID      string
	Number  string
	Total   int64
	DueDate string
}

// Store abstracts scheduler database access. Implemented in infrastructure.
type Store interface {
	ListBillable(ctx context.Context, limit, offset int) ([]BillableAccount, error)
	GetAccount(ctx context.Context, id string) (BillableAccount, error)
	GetPackage(ctx context.Context, id string) (PackageInfo, error)
	GetCustomer(ctx context.Context, id string) (CustomerInfo, error)
	// HasUnpaid reports whether the account has an unpaid invoice.
	HasUnpaid(ctx context.Context, pppoeID string) (bool, error)
	// LatestIssuedAt returns the newest issue time, or nil when the
	// account was never invoiced.
	LatestIssuedAt(ctx context.Context, pppoeID string) (*time.Time, error)
	// ListOverdue returns unpaid invoices with due_date before the given
	// YYYY-MM-DD date, oldest first.
	ListOverdue(ctx context.Context, beforeDate string, limit, offset int) ([]InvoiceInfo, error)
	// RemindedSince reports whether a reminder was logged for the invoice
	// at or after the given RFC3339 timestamp.
	RemindedSince(ctx context.Context, invoiceID, since string) (bool, error)
}

// Billing creates invoices. Implemented in infrastructure on top of the
// billing context service.
type Billing interface {
	Create(ctx context.Context, pppoeID string, items []Item, dueDate string) (CreatedInvoice, error)
}

// PPPoE isolates accounts. Implemented in infrastructure on top of the
// pppoe context repository (which emits the cache-refresh event).
type PPPoE interface {
	Isolate(ctx context.Context, id string, now time.Time) error
}

// Notifier sends templated messages. Implemented by the notify dispatcher.
type Notifier interface {
	InvoiceIssued(ctx context.Context, target, pppoeID, invoiceID string, data map[string]string) error
	Reminder(ctx context.Context, target, pppoeID, invoiceID string, data map[string]string) error
	Isolated(ctx context.Context, target, pppoeID, invoiceID string, data map[string]string) error
	Paid(ctx context.Context, target, pppoeID, invoiceID string, data map[string]string) error
}

// Disconnecter drops live sessions. Satisfied by the pppoe CoA disconnecter.
type Disconnecter interface {
	DisconnectAll(ctx context.Context, pppoeID string) (disconnected, total int, err error)
}

// Emitter publishes cache-refresh events. Satisfied by network/notify.
type Emitter interface {
	Emit(ctx context.Context, typ, action, id string)
}
