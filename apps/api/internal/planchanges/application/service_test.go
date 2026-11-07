package application_test

import (
	"context"
	"errors"
	"testing"
	"time"

	planapp "boilerplate/api/internal/planchanges/application"
	plandomain "boilerplate/api/internal/planchanges/domain"
)

type fakeAccounts struct {
	accts   map[string]*plandomain.Account
	pkgs    map[string]*plandomain.Package
	setPkgs []string
	unpaid  map[string]bool
}

func (f *fakeAccounts) GetAccount(_ context.Context, id string) (*plandomain.Account, error) {
	a, ok := f.accts[id]
	if !ok {
		return nil, plandomain.ErrAccountNotFound
	}
	return a, nil
}

func (f *fakeAccounts) GetPackage(_ context.Context, id string) (*plandomain.Package, error) {
	p, ok := f.pkgs[id]
	if !ok {
		return nil, plandomain.ErrPackageNotFound
	}
	return p, nil
}

func (f *fakeAccounts) SetPackage(_ context.Context, accountID, packageID string, _ time.Time) error {
	if _, ok := f.pkgs[packageID]; !ok {
		return plandomain.ErrPackageNotFound
	}
	a, ok := f.accts[accountID]
	if !ok {
		return plandomain.ErrAccountNotFound
	}
	a.PackageID = packageID
	f.setPkgs = append(f.setPkgs, accountID+"->"+packageID)
	return nil
}

func (f *fakeAccounts) HasUnpaid(_ context.Context, accountID string) (bool, error) {
	return f.unpaid[accountID], nil
}

type fakePlans struct {
	rows     map[string]*plandomain.PlanChange
	ordered  []*plandomain.PlanChange
	setCalls int
}

func (f *fakePlans) Insert(_ context.Context, c *plandomain.PlanChange) error {
	cp := *c
	f.rows[c.ID] = &cp
	f.ordered = append(f.ordered, &cp)
	return nil
}

func (f *fakePlans) Get(_ context.Context, id string) (*plandomain.PlanChange, error) {
	c, ok := f.rows[id]
	if !ok {
		return nil, plandomain.ErrChangeNotFound
	}
	return c, nil
}

func (f *fakePlans) ListByAccount(_ context.Context, accountID string, _, _ int) ([]*plandomain.PlanChange, error) {
	var out []*plandomain.PlanChange
	for _, c := range f.ordered {
		if c.PPPoEAccountID == accountID {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *fakePlans) ListMine(_ context.Context, _ string, _, _ int) ([]*plandomain.PlanChange, error) {
	return f.ordered, nil
}

func (f *fakePlans) CountMine(_ context.Context, _ string) (int64, error) {
	return int64(len(f.ordered)), nil
}

func (f *fakePlans) ListAll(_ context.Context, _, _ int) ([]*plandomain.PlanChange, error) {
	return f.ordered, nil
}

func (f *fakePlans) CountAll(_ context.Context) (int64, error) {
	return int64(len(f.ordered)), nil
}

func (f *fakePlans) CountActiveSince(_ context.Context, accountID, since string) (int64, error) {
	var n int64
	for _, c := range f.ordered {
		if c.PPPoEAccountID != accountID {
			continue
		}
		if c.Status != plandomain.StatusPending && c.Status != plandomain.StatusApplied {
			continue
		}
		if c.CreatedAt.UTC().Format(time.RFC3339) >= since {
			n++
		}
	}
	return n, nil
}

func (f *fakePlans) ListDue(_ context.Context, at string, _ int) ([]*plandomain.PlanChange, error) {
	var out []*plandomain.PlanChange
	for _, c := range f.ordered {
		if c.Status == plandomain.StatusPending && c.EffectiveAt.UTC().Format(time.RFC3339) <= at {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *fakePlans) SetStatus(_ context.Context, id, status string) (*plandomain.PlanChange, error) {
	c, ok := f.rows[id]
	if !ok {
		return nil, plandomain.ErrChangeNotFound
	}
	c.Status = status
	f.setCalls++
	return c, nil
}

type fakeBilling struct {
	nextID int
	items  []plandomain.ItemInput
	dues   []string
}

func (f *fakeBilling) Create(_ context.Context, _ string, items []plandomain.ItemInput, due string) (plandomain.CreatedInvoice, error) {
	f.nextID++
	f.items = append(f.items, items...)
	f.dues = append(f.dues, due)
	return plandomain.CreatedInvoice{ID: "inv-prorata", Number: "INV-P", Total: items[0].Price}, nil
}

type fakeDisco struct{ ids []string }

func (f *fakeDisco) DisconnectAll(_ context.Context, pppoeID string) (int, int, error) {
	f.ids = append(f.ids, pppoeID)
	return 0, 0, nil
}

func testDeps() (planapp.Deps, *fakeAccounts, *fakePlans, *fakeBilling, *fakeDisco) {
	accts := &fakeAccounts{
		accts: map[string]*plandomain.Account{
			"acc1": {ID: "acc1", CustomerID: "cust1", PackageID: "basic"},
			"acc2": {ID: "acc2", CustomerID: "cust2", PackageID: "basic"},
		},
		pkgs: map[string]*plandomain.Package{
			"basic":   {ID: "basic", Name: "Basic", Price: 50000, AllowSelfService: true, UpgradeGroup: "g1"},
			"pro":     {ID: "pro", Name: "Pro", Price: 100000, AllowSelfService: true, UpgradeGroup: "g1"},
			"lite":    {ID: "lite", Name: "Lite", Price: 30000, AllowSelfService: true, UpgradeGroup: "g1"},
			"same":    {ID: "same", Name: "Same", Price: 50000, AllowSelfService: true, UpgradeGroup: "g1"},
			"corp":    {ID: "corp", Name: "Corp", Price: 200000, AllowSelfService: false, UpgradeGroup: "g1"},
			"other":   {ID: "other", Name: "Other", Price: 80000, AllowSelfService: true, UpgradeGroup: "g2"},
			"planolo": {ID: "planolo", Name: "Planolo", Price: 60000, AllowSelfService: true, UpgradeGroup: ""},
		},
		unpaid: map[string]bool{},
	}
	plans := &fakePlans{rows: map[string]*plandomain.PlanChange{}}
	billing := &fakeBilling{}
	disco := &fakeDisco{}
	return planapp.Deps{Accounts: accts, Plans: plans, Billing: billing, Disco: disco}, accts, plans, billing, disco
}

func fixedSvc(d planapp.Deps) *planapp.Service {
	svc := planapp.NewService(d)
	svc.SetNow(func() time.Time { return time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC) })
	return svc
}

func TestUpgradeImmediateWithProrata(t *testing.T) {
	deps, accts, _, billing, disco := testDeps()
	svc := fixedSvc(deps)
	res, err := svc.Request(context.Background(), "cust1", "acc1", "pro")
	if err != nil {
		t.Fatal(err)
	}
	if res.Change.Status != plandomain.StatusApplied {
		t.Fatalf("status = %q", res.Change.Status)
	}
	if res.ProrataInvoiceID == nil || *res.ProrataInvoiceID != "inv-prorata" {
		t.Fatalf("prorata = %+v", res.ProrataInvoiceID)
	}
	// diff 50000 x 24/30 = 40000.
	if billing.items[0].Price != 40000 {
		t.Fatalf("prorata price = %d", billing.items[0].Price)
	}
	if billing.dues[0] != "2026-11-14" {
		t.Fatalf("due = %q", billing.dues[0])
	}
	if accts.accts["acc1"].PackageID != "pro" {
		t.Fatalf("package = %q", accts.accts["acc1"].PackageID)
	}
	if len(disco.ids) != 1 || disco.ids[0] != "acc1" {
		t.Fatalf("disconnect = %v", disco.ids)
	}
}

func TestEqualPriceImmediateWithoutInvoice(t *testing.T) {
	deps, accts, _, billing, _ := testDeps()
	svc := fixedSvc(deps)
	res, err := svc.Request(context.Background(), "cust1", "acc1", "same")
	if err != nil {
		t.Fatal(err)
	}
	if res.Change.Status != plandomain.StatusApplied || res.ProrataInvoiceID != nil {
		t.Fatalf("res = %+v", res)
	}
	if len(billing.items) != 0 {
		t.Fatalf("billing called: %+v", billing.items)
	}
	if accts.accts["acc1"].PackageID != "same" {
		t.Fatalf("package = %q", accts.accts["acc1"].PackageID)
	}
}

func TestDowngradePendingNextMonth(t *testing.T) {
	deps, accts, _, billing, disco := testDeps()
	svc := fixedSvc(deps)
	res, err := svc.Request(context.Background(), "cust1", "acc1", "lite")
	if err != nil {
		t.Fatal(err)
	}
	if res.Change.Status != plandomain.StatusPending || res.ProrataInvoiceID != nil {
		t.Fatalf("res = %+v", res)
	}
	if want := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC); !res.Change.EffectiveAt.Equal(want) {
		t.Fatalf("effective = %v", res.Change.EffectiveAt)
	}
	if accts.accts["acc1"].PackageID != "basic" {
		t.Fatal("package switched early")
	}
	if len(billing.items) != 0 || len(disco.ids) != 0 {
		t.Fatal("downgrade billed or disconnected early")
	}
}

func TestRequestRejects(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name          string
		cust, acc, to string
		unpaid        bool
		want          error
	}{
		{"other customer", "cust1", "acc2", "pro", false, plandomain.ErrAccountNotFound},
		{"unknown account", "", "ghost", "pro", false, plandomain.ErrAccountNotFound},
		{"unknown package", "cust1", "acc1", "ghost", false, plandomain.ErrPackageNotFound},
		{"same package", "cust1", "acc1", "basic", false, plandomain.ErrSamePackage},
		{"not self-service", "cust1", "acc1", "corp", false, plandomain.ErrNotSelfService},
		{"group mismatch", "cust1", "acc1", "other", false, plandomain.ErrGroupMismatch},
		{"empty group mismatch", "cust1", "acc1", "planolo", false, plandomain.ErrGroupMismatch},
		{"unpaid blocked", "cust1", "acc1", "pro", true, plandomain.ErrHasUnpaid},
		{"unpaid blocks downgrade", "cust1", "acc1", "lite", true, plandomain.ErrHasUnpaid},
	}
	for _, tc := range cases {
		deps, accts, _, _, _ := testDeps()
		if tc.unpaid {
			accts.unpaid[tc.acc] = true
		}
		_, err := fixedSvc(deps).Request(ctx, tc.cust, tc.acc, tc.to)
		if !errors.Is(err, tc.want) {
			t.Fatalf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
	}
}

func TestFrequencyOnePerMonth(t *testing.T) {
	ctx := context.Background()
	deps, _, _, _, _ := testDeps()
	svc := fixedSvc(deps)
	first, err := svc.Request(ctx, "cust1", "acc1", "lite")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Request(ctx, "cust1", "acc1", "pro"); !errors.Is(err, plandomain.ErrFrequencyExceeded) {
		t.Fatalf("second = %v", err)
	}
	// Another account is unaffected.
	if _, err := svc.Request(ctx, "cust2", "acc2", "pro"); err != nil {
		t.Fatalf("other account: %v", err)
	}
	// Cancelled rows do not count: cancel exactly the pending change,
	// then retry works.
	if _, err := svc.Cancel(ctx, "cust1", first.Change.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Request(ctx, "cust1", "acc1", "pro"); err != nil {
		t.Fatalf("after cancel: %v", err)
	}
}

func TestCancel(t *testing.T) {
	ctx := context.Background()
	deps, _, plans, _, _ := testDeps()
	svc := fixedSvc(deps)
	res, err := svc.Request(ctx, "cust1", "acc1", "lite")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Cancel(ctx, "cust2", res.Change.ID); !errors.Is(err, plandomain.ErrChangeNotFound) {
		t.Fatalf("other customer = %v", err)
	}
	c, err := svc.Cancel(ctx, "cust1", res.Change.ID)
	if err != nil || c.Status != plandomain.StatusCancelled {
		t.Fatalf("cancel = %+v %v", c, err)
	}
	if _, err := svc.Cancel(ctx, "cust1", res.Change.ID); !errors.Is(err, plandomain.ErrNotPending) {
		t.Fatalf("recancel = %v", err)
	}
	if _, err := svc.Cancel(ctx, "cust1", "ghost"); !errors.Is(err, plandomain.ErrChangeNotFound) {
		t.Fatalf("ghost = %v", err)
	}
	// Applied upgrades cannot be cancelled.
	res2, err := svc.Request(ctx, "cust2", "acc2", "pro")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Cancel(ctx, "cust2", res2.Change.ID); !errors.Is(err, plandomain.ErrNotPending) {
		t.Fatalf("cancel applied = %v", err)
	}
	_ = plans
}

func TestApplyDue(t *testing.T) {
	ctx := context.Background()
	deps, accts, plans, _, disco := testDeps()
	svc := fixedSvc(deps)
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	due := &plandomain.PlanChange{ID: "c-due", PPPoEAccountID: "acc1", FromPackage: "basic", ToPackage: "lite",
		EffectiveAt: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), Status: plandomain.StatusPending, CreatedAt: now}
	future := &plandomain.PlanChange{ID: "c-fut", PPPoEAccountID: "acc1", FromPackage: "basic", ToPackage: "lite",
		EffectiveAt: time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC), Status: plandomain.StatusPending, CreatedAt: now}
	broken := &plandomain.PlanChange{ID: "c-bad", PPPoEAccountID: "ghost", FromPackage: "basic", ToPackage: "lite",
		EffectiveAt: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), Status: plandomain.StatusPending, CreatedAt: now}
	for _, c := range []*plandomain.PlanChange{due, future, broken} {
		if err := plans.Insert(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	n, err := svc.ApplyDue(ctx, now)
	if err == nil {
		t.Fatal("expected first error for broken row")
	}
	if n != 1 {
		t.Fatalf("applied = %d", n)
	}
	if accts.accts["acc1"].PackageID != "lite" {
		t.Fatalf("package = %q", accts.accts["acc1"].PackageID)
	}
	if plans.rows["c-due"].Status != plandomain.StatusApplied {
		t.Fatalf("due status = %q", plans.rows["c-due"].Status)
	}
	if plans.rows["c-fut"].Status != plandomain.StatusPending {
		t.Fatalf("future status = %q", plans.rows["c-fut"].Status)
	}
	if len(disco.ids) != 1 {
		t.Fatalf("disconnect = %v", disco.ids)
	}
	// Second run is a no-op for applied rows (broken row still errors).
	n, _ = svc.ApplyDue(ctx, now)
	if n != 0 {
		t.Fatalf("reapply = %d", n)
	}
}
