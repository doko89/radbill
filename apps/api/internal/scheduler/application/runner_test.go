package application

import (
	"context"
	"testing"
	"time"

	scheddomain "boilerplate/api/internal/scheduler/domain"
)

type stubStore struct {
	list func(ctx context.Context, limit, offset int) ([]scheddomain.BillableAccount, error)
}

func (s *stubStore) ListBillable(ctx context.Context, limit, offset int) ([]scheddomain.BillableAccount, error) {
	return s.list(ctx, limit, offset)
}

func (s *stubStore) GetAccount(_ context.Context, _ string) (scheddomain.BillableAccount, error) {
	return scheddomain.BillableAccount{}, nil
}

func (s *stubStore) GetPackage(_ context.Context, _ string) (scheddomain.PackageInfo, error) {
	return scheddomain.PackageInfo{}, nil
}

func (s *stubStore) GetCustomer(_ context.Context, _ string) (scheddomain.CustomerInfo, error) {
	return scheddomain.CustomerInfo{}, nil
}

func (s *stubStore) HasUnpaid(_ context.Context, _ string) (bool, error) { return false, nil }

func (s *stubStore) LatestIssuedAt(_ context.Context, _ string) (*time.Time, error) {
	return nil, nil
}

func (s *stubStore) ListOverdue(_ context.Context, _ string, _, _ int) ([]scheddomain.InvoiceInfo, error) {
	return nil, nil
}

func (s *stubStore) RemindedSince(_ context.Context, _, _ string) (bool, error) {
	return false, nil
}

type stubBilling struct{}

func (stubBilling) Create(_ context.Context, _ string, _ []scheddomain.Item, _ string) (scheddomain.CreatedInvoice, error) {
	return scheddomain.CreatedInvoice{}, nil
}

type stubPPPoE struct{}

func (stubPPPoE) Isolate(_ context.Context, _ string, _ time.Time) error { return nil }

type stubNotifier struct{}

func (stubNotifier) InvoiceIssued(_ context.Context, _, _, _ string, _ map[string]string) error {
	return nil
}

func (stubNotifier) Reminder(_ context.Context, _, _, _ string, _ map[string]string) error {
	return nil
}

func (stubNotifier) Isolated(_ context.Context, _, _, _ string, _ map[string]string) error {
	return nil
}

func (stubNotifier) Paid(_ context.Context, _, _, _ string, _ map[string]string) error {
	return nil
}

type stubCoA struct{}

func (stubCoA) DisconnectAll(_ context.Context, _ string) (int, int, error) {
	return 0, 0, nil
}

type stubEvents struct{}

func (stubEvents) Emit(_ context.Context, _, _, _ string) {}

type stubConfig struct{}

func (stubConfig) BillingConfig(_ context.Context) (scheddomain.BillingConfig, error) {
	return scheddomain.BillingConfig{Mode: "activation", Day: 1, Grace: 3}, nil
}

func stubService(store scheddomain.Store) *Service {
	return NewService(Deps{
		Store: store, Billing: stubBilling{}, PPPoE: stubPPPoE{},
		Notify: stubNotifier{}, CoA: stubCoA{}, Events: stubEvents{}, Config: stubConfig{},
	})
}

func TestRunnerSingleFlight(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{}, 1)
	store := &stubStore{list: func(ctx context.Context, _ int, _ int) ([]scheddomain.BillableAccount, error) {
		select {
		case entered <- struct{}{}:
		default:
		}
		select {
		case <-release:
			return nil, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}}
	r := NewRunner(stubService(store), time.Hour)
	ctx := context.Background()

	firstDone := make(chan bool, 1)
	go func() { firstDone <- r.RunOnce(ctx) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first run never started")
	}
	// Second run while the first is blocked must skip, not queue.
	if r.RunOnce(ctx) {
		t.Fatal("concurrent run executed instead of skipping")
	}
	close(release)
	select {
	case ok := <-firstDone:
		if !ok {
			t.Fatal("first run reported skip")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("first run never finished")
	}
	if !r.RunOnce(ctx) {
		t.Fatal("run after release skipped")
	}
}

func TestRunnerStartRunsImmediatelyAndStops(t *testing.T) {
	store := &stubStore{list: func(_ context.Context, _ int, _ int) ([]scheddomain.BillableAccount, error) {
		return nil, nil
	}}
	svc := stubService(store)
	r := NewRunner(svc, time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	stop := r.Start(ctx)
	defer stop()
	deadline := time.Now().Add(5 * time.Second)
	for {
		st := svc.Status()
		if st[0].Runs == 1 && st[1].Runs == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("immediate run did not happen: %+v", st)
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	stop()
}
