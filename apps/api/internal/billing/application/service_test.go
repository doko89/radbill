package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"boilerplate/api/internal/billing/domain"
)

type fakeRepo struct {
	inv      *domain.Invoice
	items    []*domain.InvoiceItem
	payments []*domain.Payment
	paid     *domain.Invoice
	payErr   error
}

func (f *fakeRepo) CreateInvoice(_ context.Context, inv *domain.Invoice, items []*domain.InvoiceItem) error {
	f.inv = inv
	f.items = items
	return nil
}

func (f *fakeRepo) GetInvoice(_ context.Context, _ string) (*domain.Invoice, error) {
	if f.inv == nil {
		return nil, domain.ErrInvoiceNotFound
	}
	return f.inv, nil
}

func (f *fakeRepo) ListInvoices(_ context.Context, _ string, _, _ int) ([]*domain.Invoice, error) {
	return []*domain.Invoice{f.inv}, nil
}

func (f *fakeRepo) CountInvoices(_ context.Context, _ string) (int64, error) { return 1, nil }

func (f *fakeRepo) ListItems(_ context.Context, _ string) ([]*domain.InvoiceItem, error) {
	return f.items, nil
}

func (f *fakeRepo) ListPayments(_ context.Context, _ string) ([]*domain.Payment, error) {
	return f.payments, nil
}

func (f *fakeRepo) VoidInvoice(_ context.Context, _ string, now time.Time) (*domain.Invoice, error) {
	if err := f.inv.Void(now); err != nil {
		return nil, err
	}
	return f.inv, nil
}

func (f *fakeRepo) PayManual(_ context.Context, _ string, amount int64, newExpires time.Time, now time.Time) (*domain.Invoice, error) {
	if f.payErr != nil {
		return nil, f.payErr
	}
	if err := f.inv.MarkPaid(now); err != nil {
		return nil, err
	}
	f.paid = f.inv
	_ = amount
	_ = newExpires
	return f.inv, nil
}

type fakePPPoE struct {
	ref *domain.PPPoERef
	err error
}

func (f *fakePPPoE) GetPPPoE(_ context.Context, _ string) (*domain.PPPoERef, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.ref, nil
}

type fakePkg struct {
	validity *int64
}

func (f *fakePkg) ValiditySeconds(_ context.Context, _ string) (*int64, error) {
	return f.validity, nil
}

func unpaidInvoice() *domain.Invoice {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	inv, _ := domain.NewInvoice("inv1", "INV-1", "ppp1", "cust1", 50000, "2026-11-14", now)
	return inv
}

func TestManualPaymentTransitions(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	validity := int64(7 * 86400)
	tests := []struct {
		name    string
		amount  int64
		wantErr error
	}{
		{"full amount", 50000, nil},
		{"zero means full total", 0, nil},
		{"mismatch rejected", 49999, domain.ErrAmountMismatch},
		{"overpay rejected", 50001, domain.ErrAmountMismatch},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepo{inv: unpaidInvoice()}
			svc := NewService(repo,
				&fakePPPoE{ref: &domain.PPPoERef{ID: "ppp1", CustomerID: "cust1", PackageID: "pkg1", Status: "isolated"}},
				&fakePkg{validity: &validity})
			svc.now = func() time.Time { return now }
			dto, err := svc.ManualPayment(context.Background(), "inv1", tt.amount)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if dto.Status != domain.StatusPaid {
				t.Fatalf("status = %q, want paid", dto.Status)
			}
			if repo.paid == nil || repo.paid.Status != domain.StatusPaid {
				t.Fatalf("repo invoice not marked paid")
			}
		})
	}
}

func TestManualPaymentPropagatesRepoErrors(t *testing.T) {
	for _, wantErr := range []error{domain.ErrAlreadyPaid, domain.ErrInvoiceVoid, domain.ErrInvoiceNotFound} {
		repo := &fakeRepo{inv: unpaidInvoice(), payErr: wantErr}
		svc := NewService(repo,
			&fakePPPoE{ref: &domain.PPPoERef{ID: "ppp1", CustomerID: "cust1", PackageID: "pkg1", Status: "active"}},
			&fakePkg{})
		if _, err := svc.ManualPayment(context.Background(), "inv1", 50000); !errors.Is(err, wantErr) {
			t.Fatalf("err = %v, want %v", err, wantErr)
		}
	}
}

func TestManualPaymentUnknownPPPoE(t *testing.T) {
	repo := &fakeRepo{inv: unpaidInvoice()}
	svc := NewService(repo, &fakePPPoE{err: domain.ErrUnknownPPPoE}, &fakePkg{})
	if _, err := svc.ManualPayment(context.Background(), "inv1", 0); !errors.Is(err, domain.ErrUnknownPPPoE) {
		t.Fatalf("err = %v, want ErrUnknownPPPoE", err)
	}
}

func TestCreateValidation(t *testing.T) {
	svc := NewService(&fakeRepo{},
		&fakePPPoE{ref: &domain.PPPoERef{ID: "ppp1", CustomerID: "cust1", PackageID: "pkg1"}},
		&fakePkg{})
	if _, err := svc.Create(context.Background(), "ppp1", nil, ""); err == nil {
		t.Fatalf("empty items accepted")
	}
	if _, _, err := svc.List(context.Background(), "bogus", 10, 0); err == nil {
		t.Fatalf("bogus status filter accepted")
	}
}
