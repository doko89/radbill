package domain

import (
	"testing"
	"time"

	"boilerplate/api/internal/shared/apperr"
)

func TestNewOrder(t *testing.T) {
	now := time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC)
	longItem := make([]byte, 121)
	for i := range longItem {
		longItem[i] = 'x'
	}

	tests := []struct {
		name    string
		id      string
		userID  string
		item    string
		qty     int64
		wantErr *apperr.FieldError
	}{
		{"valid", "o1", "u1", "keyboard", 2, nil},
		{"qty min boundary", "o1", "u1", "keyboard", 1, nil},
		{"qty max boundary", "o1", "u1", "keyboard", MaxQty, nil},
		{"missing id", "", "u1", "keyboard", 1, apperr.Invalid("id", "required")},
		{"missing user", "o1", "", "keyboard", 1, apperr.Invalid("userId", "required")},
		{"missing item", "o1", "u1", "", 1, apperr.Invalid("item", "required")},
		{"item too long", "o1", "u1", string(longItem), 1, apperr.Invalid("item", "max 120 characters")},
		{"qty zero", "o1", "u1", "keyboard", 0, apperr.Invalid("qty", "must be between 1 and 9999")},
		{"qty over max", "o1", "u1", "keyboard", MaxQty + 1, apperr.Invalid("qty", "must be between 1 and 9999")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o, err := NewOrder(tt.id, tt.userID, tt.item, tt.qty, now)
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
				if o.Status != StatusOpen {
					t.Fatalf("expected status open, got %q", o.Status)
				}
				return
			}
			fe, ok := err.(*apperr.FieldError)
			if !ok {
				t.Fatalf("expected *FieldError, got %T (%v)", err, err)
			}
			if fe.Field != tt.wantErr.Field || fe.Message != tt.wantErr.Message {
				t.Fatalf("expected %v, got %v", tt.wantErr, fe)
			}
		})
	}
}

func TestOrderTransitions(t *testing.T) {
	now := time.Now().UTC()

	o, err := NewOrder("o1", "u1", "mouse", 1, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := o.Done(); err != nil {
		t.Fatalf("Done on open: %v", err)
	}
	if o.Status != StatusDone {
		t.Fatalf("expected done, got %q", o.Status)
	}
	if err := o.Cancel(); err != ErrNotOpen {
		t.Fatalf("expected ErrNotOpen, got %v", err)
	}
	if err := o.Done(); err != ErrNotOpen {
		t.Fatalf("expected ErrNotOpen, got %v", err)
	}

	o2, err := NewOrder("o2", "u1", "cable", 1, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := o2.Cancel(); err != nil {
		t.Fatalf("Cancel on open: %v", err)
	}
	if o2.Status != StatusCancelled {
		t.Fatalf("expected cancelled, got %q", o2.Status)
	}
	if err := o2.Done(); err != ErrNotOpen {
		t.Fatalf("expected ErrNotOpen, got %v", err)
	}
}
