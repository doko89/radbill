package presentation_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"

	topupapp "boilerplate/api/internal/topup/application"
	topupdomain "boilerplate/api/internal/topup/domain"
	topuppres "boilerplate/api/internal/topup/presentation"
)

type stubAccounts struct{}

func (stubAccounts) GetAccount(_ context.Context, id string) (*topupdomain.Account, error) {
	if id != "acc1" {
		return nil, topupdomain.ErrAccountNotFound
	}
	return &topupdomain.Account{ID: "acc1", CustomerID: "cust1", PackageID: "pkg-q", CreatedAt: "2026-10-05T00:00:00Z"}, nil
}

func (stubAccounts) GetPackage(_ context.Context, _ string) (*topupdomain.Package, error) {
	q := int64(10 * topupdomain.GB)
	return &topupdomain.Package{ID: "pkg-q", QuotaBytes: &q, QuotaReset: "monthly"}, nil
}

type stubPrices struct{}

func (stubPrices) PricePerGB(_ context.Context) (int64, error) { return 10000, nil }

type stubTopups struct{}

func (stubTopups) FindByInvoice(_ context.Context, _ string) (*topupdomain.Topup, error) {
	return nil, nil
}
func (stubTopups) Insert(_ context.Context, _ *topupdomain.Topup) error { return nil }
func (stubTopups) SumByAccountPeriod(_ context.Context, _, _ string) (int64, error) {
	return 0, nil
}

type stubInvoices struct{}

func (stubInvoices) GetInvoice(_ context.Context, _ string) (*topupdomain.Invoice, error) {
	return nil, topupdomain.ErrInvoiceNotFound
}
func (stubInvoices) ListItems(_ context.Context, _ string) ([]topupdomain.Item, error) {
	return nil, nil
}

type stubBilling struct{}

func (stubBilling) Create(_ context.Context, _ string, items []topupdomain.ItemInput, _ string) (topupdomain.CreatedInvoice, error) {
	return topupdomain.CreatedInvoice{ID: "inv-9", Number: "INV-9", Total: items[0].Price}, nil
}

type stubEvents struct{}

func (stubEvents) Emit(_ context.Context, _, _, _ string) {}

func testApp() *fiber.App {
	svc := topupapp.NewService(topupapp.Deps{
		Accounts: stubAccounts{}, Prices: stubPrices{}, Topups: stubTopups{},
		Invoices: stubInvoices{}, Billing: stubBilling{}, Events: stubEvents{},
	})
	h := topuppres.NewHandler(svc)
	app := fiber.New()
	app.Use(func(c fiber.Ctx) error {
		c.Locals("billing_customer_id", "cust1")
		return c.Next()
	})
	app.Post("/api/v1/portal/topup", h.Topup)
	return app
}

func post(t *testing.T, app *fiber.App, body string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest("POST", "/api/v1/portal/topup", strings.NewReader(body))
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

func TestTopupEndpointShape(t *testing.T) {
	app := testApp()
	code, body := post(t, app, `{"pppoe_account_id":"acc1","bytes":5368709120}`)
	if code != 201 {
		t.Fatalf("code = %d (%v)", code, body)
	}
	inv := body["data"].(map[string]any)["invoice"].(map[string]any)
	if inv["id"] != "inv-9" || inv["number"] != "INV-9" || inv["total"] != float64(50000) {
		t.Fatalf("invoice = %v", inv)
	}
}

func TestTopupEndpointErrors(t *testing.T) {
	app := testApp()
	if code, _ := post(t, app, `{"pppoe_account_id":"acc1","bytes":123}`); code != 400 {
		t.Fatalf("bad preset code = %d", code)
	}
	if code, _ := post(t, app, `{"pppoe_account_id":"ghost","bytes":5368709120}`); code != 404 {
		t.Fatalf("ghost code = %d", code)
	}
	if code, _ := post(t, app, `{"bytes":5368709120}`); code != 400 {
		t.Fatalf("missing account code = %d", code)
	}
}
