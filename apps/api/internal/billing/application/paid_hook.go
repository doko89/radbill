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

// fanoutNotifier calls every branch in order and returns the first error.
// The service logs hook errors without failing the payment.
type fanoutNotifier struct {
	notifiers []PaidNotifier
}

// FanoutNotifiers combines post-payment hooks (G3, ADITIF): the scheduler
// paid notification plus top-up apply-on-paid. Nil entries are skipped.
func FanoutNotifiers(notifiers ...PaidNotifier) PaidNotifier {
	return &fanoutNotifier{notifiers: notifiers}
}

// NotifyPaid implements PaidNotifier.
func (f *fanoutNotifier) NotifyPaid(ctx context.Context, e PaidEvent) error {
	var first error
	for _, n := range f.notifiers {
		if n == nil {
			continue
		}
		if err := n.NotifyPaid(ctx, e); err != nil && first == nil {
			first = err
		}
	}
	return first
}
