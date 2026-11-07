package infrastructure

import (
	"context"
	"testing"
	"time"

	"boilerplate/api/internal/billing/domain"
)

func TestPaymentNoteRoundtrip(t *testing.T) {
	q, repo := openTestDB(t)
	ctx := context.Background()
	pppoeID, customerID := seedPPPoE(t, q, "note", "active")
	now := time.Now().UTC().Truncate(time.Second)

	inv, _ := domain.NewInvoice("inv-note-1", "INV-NOTE-1", pppoeID, customerID, 50000, "2026-11-14", now)
	item, _ := domain.NewItem("item-note-1", inv.ID, "Monthly", 1, 50000)
	if err := repo.CreateInvoice(ctx, inv, []*domain.InvoiceItem{item}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := repo.PayManual(ctx, inv.ID, 50000, now.Add(30*24*time.Hour), now); err != nil {
		t.Fatalf("pay: %v", err)
	}
	payments, err := repo.ListPayments(ctx, inv.ID)
	if err != nil || len(payments) != 1 {
		t.Fatalf("payments: %+v %v", payments, err)
	}
	// Default is empty.
	if note, err := repo.GetPaymentNote(ctx, payments[0].ID); err != nil || note != "" {
		t.Fatalf("default note = %q,%v", note, err)
	}
	if err := repo.SetPaymentNote(ctx, payments[0].ID, "tunai, kode unik 50"); err != nil {
		t.Fatalf("set note: %v", err)
	}
	if note, err := repo.GetPaymentNote(ctx, payments[0].ID); err != nil || note != "tunai, kode unik 50" {
		t.Fatalf("note = %q,%v", note, err)
	}
}
