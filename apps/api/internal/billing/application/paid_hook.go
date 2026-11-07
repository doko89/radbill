package application

import "context"

// PaidEvent describes a settled manual payment for post-payment hooks.
type PaidEvent struct {
	InvoiceID      string
	Number         string
	Total          int64
	DueDate        string
	CustomerID     string
	PPPoEAccountID string
}

// PaidNotifier receives settled-payment events (G2: paid notification).
// It runs best-effort after the payment transaction commits; an error is
// logged by the service and never fails the payment.
type PaidNotifier interface {
	NotifyPaid(ctx context.Context, e PaidEvent) error
}

// SetPaidNotifier installs the post-payment hook. Nil (the default) keeps
// the Fase 1 behavior exactly.
func (s *Service) SetPaidNotifier(n PaidNotifier) {
	s.paidNotifier = n
}
