// Package application hosts billing use cases.
package application

import (
	"context"
	"errors"
	"strings"
	"time"

	"boilerplate/api/internal/billing/domain"
	"boilerplate/api/internal/shared/apperr"
	"boilerplate/api/internal/shared/id"
)

// DefaultManualExtension is the fallback extension when the package has no
// validity_seconds (30 days).
const DefaultManualExtension = 30 * 24 * time.Hour

// InvoiceDTO is the public snake_case shape of an invoice.
type InvoiceDTO struct {
	ID             string  `json:"id"`
	Number         string  `json:"number"`
	PPPoEAccountID string  `json:"pppoe_account_id"`
	CustomerID     string  `json:"customer_id"`
	Total          int64   `json:"total"`
	DueDate        string  `json:"due_date"`
	Status         string  `json:"status"`
	IssuedAt       string  `json:"issued_at"`
	PaidAt         *string `json:"paid_at"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
}

// InvoiceItemDTO is the public shape of an invoice line.
type InvoiceItemDTO struct {
	ID          string `json:"id"`
	InvoiceID   string `json:"invoice_id"`
	Description string `json:"description"`
	Qty         int64  `json:"qty"`
	Price       int64  `json:"price"`
}

// PaymentDTO is the public shape of a payment.
type PaymentDTO struct {
	ID        string  `json:"id"`
	InvoiceID string  `json:"invoice_id"`
	Amount    int64   `json:"amount"`
	Method    string  `json:"method"`
	AttemptID *string `json:"attempt_id"`
	PaidAt    string  `json:"paid_at"`
	CreatedAt string  `json:"created_at"`
}

// InvoiceDetailDTO is an invoice with its items and payments.
type InvoiceDetailDTO struct {
	InvoiceDTO
	Items    []InvoiceItemDTO `json:"items"`
	Payments []PaymentDTO     `json:"payments"`
}

// ItemInput carries one raw invoice line.
type ItemInput struct {
	Description string
	Qty         int64
	Price       int64
}

// Service implements billing use cases.
type Service struct {
	invoices domain.Repository
	pppoe    domain.PPPoELookup
	packages domain.PackageLookup
	now      func() time.Time
}

// NewService builds a Service.
func NewService(invoices domain.Repository, pppoe domain.PPPoELookup, packages domain.PackageLookup) *Service {
	return &Service{invoices: invoices, pppoe: pppoe, packages: packages, now: time.Now}
}

// List returns a page of invoices (status "" = all) plus the total count.
func (s *Service) List(ctx context.Context, status string, limit, offset int) ([]InvoiceDTO, int64, error) {
	switch status {
	case "", domain.StatusUnpaid, domain.StatusPaid, domain.StatusVoid:
	default:
		return nil, 0, apperr.Invalid("status", "must be unpaid|paid|void")
	}
	rows, err := s.invoices.ListInvoices(ctx, status, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.invoices.CountInvoices(ctx, status)
	if err != nil {
		return nil, 0, err
	}
	out := make([]InvoiceDTO, 0, len(rows))
	for _, inv := range rows {
		out = append(out, toDTO(inv))
	}
	return out, total, nil
}

// Get returns an invoice with items and payments.
func (s *Service) Get(ctx context.Context, invoiceID string) (InvoiceDetailDTO, error) {
	inv, err := s.invoices.GetInvoice(ctx, invoiceID)
	if err != nil {
		return InvoiceDetailDTO{}, err
	}
	items, err := s.invoices.ListItems(ctx, invoiceID)
	if err != nil {
		return InvoiceDetailDTO{}, err
	}
	payments, err := s.invoices.ListPayments(ctx, invoiceID)
	if err != nil {
		return InvoiceDetailDTO{}, err
	}
	return toDetail(inv, items, payments), nil
}

// Create builds an unpaid invoice for a PPPoE account. Total is computed
// server-side; customer_id comes from the PPPoE account. Empty dueDate
// defaults to 7 days after issue.
func (s *Service) Create(ctx context.Context, pppoeID string, items []ItemInput, dueDate string) (InvoiceDetailDTO, error) {
	if pppoeID == "" {
		return InvoiceDetailDTO{}, apperr.Invalid("pppoe_account_id", "required")
	}
	if len(items) == 0 {
		return InvoiceDetailDTO{}, apperr.Invalid("items", "at least one item required")
	}
	ref, err := s.pppoe.GetPPPoE(ctx, pppoeID)
	if err != nil {
		return InvoiceDetailDTO{}, err
	}
	now := s.now().UTC()
	if dueDate == "" {
		dueDate = now.AddDate(0, 0, 7).Format("2006-01-02")
	}
	var total int64
	for _, it := range items {
		if it.Qty < 1 {
			return InvoiceDetailDTO{}, apperr.Invalid("qty", "must be >= 1")
		}
		if it.Price < 0 {
			return InvoiceDetailDTO{}, apperr.Invalid("price", "must be >= 0")
		}
		total += it.Qty * it.Price
	}
	if total <= 0 {
		return InvoiceDetailDTO{}, apperr.Invalid("total", "must be > 0")
	}
	var inv *domain.Invoice
	for attempt := 0; attempt < 5; attempt++ {
		invoiceID := id.New()
		number := "INV-" + now.Format("20060102") + "-" + strings.ToUpper(invoiceID[:6])
		inv, err = domain.NewInvoice(invoiceID, number, pppoeID, ref.CustomerID, total, dueDate, now)
		if err != nil {
			return InvoiceDetailDTO{}, err
		}
		lines := make([]*domain.InvoiceItem, 0, len(items))
		for _, it := range items {
			line, err := domain.NewItem(id.New(), invoiceID, it.Description, it.Qty, it.Price)
			if err != nil {
				return InvoiceDetailDTO{}, err
			}
			lines = append(lines, line)
		}
		if err := s.invoices.CreateInvoice(ctx, inv, lines); err != nil {
			if isNumberTaken(err) {
				continue
			}
			return InvoiceDetailDTO{}, err
		}
		return toDetail(inv, lines, nil), nil
	}
	return InvoiceDetailDTO{}, err
}

// Void cancels an unpaid invoice.
func (s *Service) Void(ctx context.Context, invoiceID string) (InvoiceDTO, error) {
	inv, err := s.invoices.VoidInvoice(ctx, invoiceID, s.now().UTC())
	if err != nil {
		return InvoiceDTO{}, err
	}
	return toDTO(inv), nil
}

// ManualPayment records a manual (cash/transfer) payment: invoice paid,
// expiry extended by package validity (30 days fallback), isolated
// accounts reactivated. Amount 0 means "full total".
func (s *Service) ManualPayment(ctx context.Context, invoiceID string, amount int64) (InvoiceDTO, error) {
	now := s.now().UTC()
	inv, err := s.invoices.GetInvoice(ctx, invoiceID)
	if err != nil {
		return InvoiceDTO{}, err
	}
	if amount == 0 {
		amount = inv.Total
	}
	if amount != inv.Total {
		return InvoiceDTO{}, domain.ErrAmountMismatch
	}
	ref, err := s.pppoe.GetPPPoE(ctx, inv.PPPoEAccountID)
	if err != nil {
		return InvoiceDTO{}, err
	}
	validity, err := s.packages.ValiditySeconds(ctx, ref.PackageID)
	if err != nil {
		return InvoiceDTO{}, err
	}
	period := DefaultManualExtension
	if validity != nil && *validity > 0 {
		period = time.Duration(*validity) * time.Second
	}
	base := now
	if ref.ExpiresAt != nil && ref.ExpiresAt.After(now) {
		base = *ref.ExpiresAt
	}
	paid, err := s.invoices.PayManual(ctx, invoiceID, amount, base.Add(period), now)
	if err != nil {
		return InvoiceDTO{}, err
	}
	return toDTO(paid), nil
}

func isNumberTaken(err error) bool {
	return errors.Is(err, domain.ErrNumberTaken)
}

func toDTO(inv *domain.Invoice) InvoiceDTO {
	dto := InvoiceDTO{
		ID: inv.ID, Number: inv.Number,
		PPPoEAccountID: inv.PPPoEAccountID, CustomerID: inv.CustomerID,
		Total: inv.Total, DueDate: inv.DueDate, Status: inv.Status,
		IssuedAt:  inv.IssuedAt.UTC().Format(time.RFC3339),
		CreatedAt: inv.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: inv.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if inv.PaidAt != nil {
		paid := inv.PaidAt.UTC().Format(time.RFC3339)
		dto.PaidAt = &paid
	}
	return dto
}

func toDetail(inv *domain.Invoice, items []*domain.InvoiceItem, payments []*domain.Payment) InvoiceDetailDTO {
	d := InvoiceDetailDTO{
		InvoiceDTO: toDTO(inv),
		Items:      make([]InvoiceItemDTO, 0, len(items)),
		Payments:   make([]PaymentDTO, 0, len(payments)),
	}
	for _, it := range items {
		d.Items = append(d.Items, InvoiceItemDTO{
			ID: it.ID, InvoiceID: it.InvoiceID,
			Description: it.Description, Qty: it.Qty, Price: it.Price,
		})
	}
	for _, p := range payments {
		d.Payments = append(d.Payments, PaymentDTO{
			ID: p.ID, InvoiceID: p.InvoiceID, Amount: p.Amount, Method: p.Method,
			AttemptID: nullStr(p.AttemptID),
			PaidAt:    p.PaidAt.UTC().Format(time.RFC3339),
			CreatedAt: p.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	return d
}

func nullStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
