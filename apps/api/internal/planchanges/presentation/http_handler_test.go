package presentation_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	planapp "boilerplate/api/internal/planchanges/application"
	plandomain "boilerplate/api/internal/planchanges/domain"
	planpres "boilerplate/api/internal/planchanges/presentation"
)

type stubAccounts struct {
	accts map[string]*plandomain.Account
	pkgs  map[string]*plandomain.Package
}

func (s stubAccounts) GetAccount(_ context.Context, id string) (*plandomain.Account, error) {
	a, ok := s.accts[id]
	if !ok {
		return nil, plandomain.ErrAccountNotFound
	}
	return a, nil
}

func (s stubAccounts) GetPackage(_ context.Context, id string) (*plandomain.Package, error) {
	p, ok := s.pkgs[id]
	if !ok {
		return nil, plandomain.ErrPackageNotFound
	}
	return p, nil
}

func (s stubAccounts) SetPackage(_ context.Context, accountID, packageID string, _ time.Time) error {
	s.accts[accountID].PackageID = packageID
	return nil
}

func (s stubAccounts) HasUnpaid(_ context.Context, _ string) (bool, error) { return false, nil }

type stubPlans struct {
	rows []*plandomain.PlanChange
}

func (s *stubPlans) Insert(_ context.Context, c *plandomain.PlanChange) error {
	s.rows = append(s.rows, c)
	return nil
}

func (s *stubPlans) Get(_ context.Context, id string) (*plandomain.PlanChange, error) {
	for _, c := range s.rows {
		if c.ID == id {
			return c, nil
		}
	}
	return nil, plandomain.ErrChangeNotFound
}

func (s *stubPlans) ListByAccount(_ context.Context, _ string, _, _ int) ([]*plandomain.PlanChange, error) {
	return s.rows, nil
}

func (s *stubPlans) ListMine(_ context.Context, _ string, _, _ int) ([]*plandomain.PlanChange, error) {
	return s.rows, nil
}

func (s *stubPlans) CountMine(_ context.Context, _ string) (int64, error) {
	return int64(len(s.rows)), nil
}

func (s *stubPlans) ListAll(_ context.Context, _, _ int) ([]*plandomain.PlanChange, error) {
	return s.rows, nil
}

func (s *stubPlans) CountAll(_ context.Context) (int64, error) { return int64(len(s.rows)), nil }

func (s *stubPlans) CountActiveSince(_ context.Context, _ string, _ string) (int64, error) {
	return 0, nil
}

func (s *stubPlans) ListDue(_ context.Context, _ string, _ int) ([]*plandomain.PlanChange, error) {
	return nil, nil
}

func (s *stubPlans) SetStatus(_ context.Context, id, status string) (*plandomain.PlanChange, error) {
	c, err := s.Get(context.Background(), id)
	if err != nil {
		return nil, err
	}
	c.Status = status
	return c, nil
}

type stubBilling struct{}

func (stubBilling) Create(_ context.Context, _ string, items []plandomain.ItemInput, _ string) (plandomain.CreatedInvoice, error) {
	return plandomain.CreatedInvoice{ID: "inv-p", Number: "INV-P", Total: items[0].Price}, nil
}

func testSvc() *planapp.Service {
	svc := planapp.NewService(planapp.Deps{
		Accounts: stubAccounts{
			accts: map[string]*plandomain.Account{
				"acc1": {ID: "acc1", CustomerID: "cust1", PackageID: "basic"},
			},
			pkgs: map[string]*plandomain.Package{
				"basic": {ID: "basic", Name: "Basic", Price: 50000, AllowSelfService: true, UpgradeGroup: "g1"},
				"pro":   {ID: "pro", Name: "Pro", Price: 100000, AllowSelfService: true, UpgradeGroup: "g1"},
				"lite":  {ID: "lite", Name: "Lite", Price: 30000, AllowSelfService: true, UpgradeGroup: "g1"},
			},
		},
		Plans:   &stubPlans{},
		Billing: stubBilling{},
	})
	svc.SetNow(func() time.Time { return time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC) })
	return svc
}

func testApp(svc *planapp.Service) *fiber.App {
	h := planpres.NewHandler(svc, nil)
	app := fiber.New()
	app.Use(func(c fiber.Ctx) error {
		c.Locals("billing_customer_id", "cust1")
		return c.Next()
	})
	app.Post("/portal/plan-changes", h.Create)
	app.Get("/portal/plan-changes", h.List)
	app.Post("/portal/plan-changes/:id/cancel", h.Cancel)
	return app
}

func do(t *testing.T, app *fiber.App, method, path, body string) (int, map[string]any) {
	t.Helper()
	var r *strings.Reader
	if body == "" {
		r = strings.NewReader("")
	} else {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, r)
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decode %q: %v", raw, err)
	}
	return resp.StatusCode, decoded
}

func TestCreateShapes(t *testing.T) {
	// Upgrade: applied + prorata id, exact dictated keys.
	code, body := do(t, testApp(testSvc()), "POST", "/portal/plan-changes", `{"pppoe_account_id":"acc1","to_package_id":"pro"}`)
	if code != 201 {
		t.Fatalf("code = %d (%v)", code, body)
	}
	data := body["data"].(map[string]any)
	if data["status"] != "applied" || data["effective_at"] != "2026-11-07T12:00:00Z" || data["prorata_invoice_id"] != "inv-p" {
		t.Fatalf("data = %v", data)
	}
	if len(data) != 3 {
		t.Fatalf("extra keys: %v", data)
	}
	// Downgrade: pending, no prorata key.
	code, body = do(t, testApp(testSvc()), "POST", "/portal/plan-changes", `{"pppoe_account_id":"acc1","to_package_id":"lite"}`)
	if code != 201 {
		t.Fatalf("code = %d (%v)", code, body)
	}
	data = body["data"].(map[string]any)
	if data["status"] != "pending" || data["effective_at"] != "2026-12-01T00:00:00Z" {
		t.Fatalf("data = %v", data)
	}
	if _, ok := data["prorata_invoice_id"]; ok {
		t.Fatalf("unexpected prorata: %v", data)
	}
}

func TestCreateErrors(t *testing.T) {
	app := testApp(testSvc())
	if code, _ := do(t, app, "POST", "/portal/plan-changes", `{"pppoe_account_id":"acc1","to_package_id":"basic"}`); code != 400 {
		t.Fatalf("same = %d", code)
	}
	if code, _ := do(t, app, "POST", "/portal/plan-changes", `{"pppoe_account_id":"acc1","to_package_id":"ghost"}`); code != 400 {
		t.Fatalf("ghost = %d", code)
	}
	if code, _ := do(t, app, "POST", "/portal/plan-changes", `{"to_package_id":"pro"}`); code != 400 {
		t.Fatalf("missing = %d", code)
	}
}

func TestListAndCancel(t *testing.T) {
	svc := testSvc()
	app := testApp(svc)
	if code, _ := do(t, app, "POST", "/portal/plan-changes", `{"pppoe_account_id":"acc1","to_package_id":"lite"}`); code != 201 {
		t.Fatalf("create = %d", code)
	}
	code, body := do(t, app, "GET", "/portal/plan-changes", "")
	if code != 200 || body["total"] != float64(1) {
		t.Fatalf("list = %d %v", code, body)
	}
	// Cancel via the recorded id (stub: reach into service plans indirectly
	// through a second list is overkill; use the known single row id via
	// the fake: cancel with wrong id 404s, right id cancels).
	if code, _ := do(t, app, "POST", "/portal/plan-changes/ghost/cancel", ""); code != 404 {
		t.Fatalf("ghost cancel = %d", code)
	}
	rows, _, _ := svc.ListMine(context.Background(), "cust1", 10, 0)
	code, body = do(t, app, "POST", "/portal/plan-changes/"+rows[0].ID+"/cancel", "")
	if code != 200 {
		t.Fatalf("cancel = %d %v", code, body)
	}
	if body["data"].(map[string]any)["status"] != "cancelled" {
		t.Fatalf("cancelled = %v", body)
	}
	if code, _ := do(t, app, "POST", "/portal/plan-changes/"+rows[0].ID+"/cancel", ""); code != 409 {
		t.Fatalf("recancel = %d", code)
	}
}
