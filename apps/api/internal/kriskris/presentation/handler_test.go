package kriskrispres_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	kriskrisapp "boilerplate/api/internal/kriskris/application"
	kriskrisinfra "boilerplate/api/internal/kriskris/infrastructure"
	kriskrispres "boilerplate/api/internal/kriskris/presentation"
	kriskristest "boilerplate/api/internal/kriskris/testkit"
)

func newTestApp(t *testing.T, pool *sql.DB, fake *kriskristest.FakeKriskris, customerID *string) *fiber.App {
	t.Helper()
	client := kriskrisinfra.NewClient(fake.Server.URL, "test-key", "test-secret")
	svc := kriskrisapp.NewService(kriskrisinfra.NewStore(pool), client,
		kriskrisinfra.Config{BaseURL: fake.Server.URL, APIKey: "test-key", APISecret: "test-secret", CallbackSecret: "cb-secret"})
	svc.SetSleep(func(time.Duration) {})
	h := kriskrispres.NewHandlerWithService(pool, svc)
	app := fiber.New()
	// Stub for C1's Require middleware: sets billing_customer_id Locals.
	app.Use(func(c fiber.Ctx) error {
		if customerID != nil {
			c.Locals("billing_customer_id", *customerID)
		}
		c.Locals("billing_role", "user")
		return c.Next()
	})
	app.Post("/pay/:id", h.PayInvoice)
	app.Get("/status/:id", h.AttemptStatus)
	app.Post("/webhook", h.Webhook)
	return app
}

func do(t *testing.T, app *fiber.App, req *http.Request) (int, map[string]any) {
	t.Helper()
	resp, err := app.Test(req, fiber.TestConfig{Timeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("non-JSON %d %q", resp.StatusCode, string(raw))
	}
	return resp.StatusCode, out
}

func jsonReq(t *testing.T, method, path, body string) *http.Request {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rdr)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	return req
}

func sign(t *testing.T, secret string, body []byte) string {
	t.Helper()
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestPayInvoiceEndpoint(t *testing.T) {
	pool := kriskristest.OpenDB(t)
	fake := kriskristest.NewFake(t, "test-key", "test-secret")
	seed := kriskristest.Seed(t, pool, kriskristest.SeedOpts{Tag: "http1", Total: 86000})
	other := kriskristest.Seed(t, pool, kriskristest.SeedOpts{Tag: "http2"})
	cust := seed.CustomerID
	app := newTestApp(t, pool, fake, &cust)

	code, body := do(t, app, jsonReq(t, http.MethodPost, "/pay/"+seed.InvoiceID, ""))
	if code != http.StatusCreated {
		t.Fatalf("pay = %d %v", code, body)
	}
	data, _ := body["data"].(map[string]any)
	if data["qr_payload"] == "" || data["expires_at"] == "" || data["attempt_id"] == "" {
		t.Fatalf("data = %v", body)
	}
	if data["amount"].(float64) != 86000+data["unique_code"].(float64) {
		t.Fatalf("amounts = %v", data)
	}
	if !strings.Contains(data["message"].(string), "kode unik") {
		t.Fatalf("message = %v", data["message"])
	}

	// Other customer's invoice -> 404 (IDOR-safe).
	code, _ = do(t, app, jsonReq(t, http.MethodPost, "/pay/"+other.InvoiceID, ""))
	if code != http.StatusNotFound {
		t.Fatalf("other's = %d, want 404", code)
	}
	// Unknown invoice -> 404.
	code, _ = do(t, app, jsonReq(t, http.MethodPost, "/pay/inv-nope", ""))
	if code != http.StatusNotFound {
		t.Fatalf("missing = %d, want 404", code)
	}
}

func TestPayInvoiceGobizOffline503(t *testing.T) {
	pool := kriskristest.OpenDB(t)
	fake := kriskristest.NewFake(t, "test-key", "test-secret")
	fake.OnCreate = func(amount int64, label string) (int, any) {
		return http.StatusServiceUnavailable, map[string]any{"error": map[string]any{"code": "gobiz_offline"}}
	}
	seed := kriskristest.Seed(t, pool, kriskristest.SeedOpts{Tag: "http3"})
	cust := seed.CustomerID
	app := newTestApp(t, pool, fake, &cust)
	code, body := do(t, app, jsonReq(t, http.MethodPost, "/pay/"+seed.InvoiceID, ""))
	if code != http.StatusServiceUnavailable {
		t.Fatalf("gobiz = %d %v", code, body)
	}
	errObj, _ := body["error"].(map[string]any)
	if errObj["code"] != "gobiz_offline" {
		t.Fatalf("code = %v", body)
	}
}

func TestAttemptStatusEndpoint(t *testing.T) {
	pool := kriskristest.OpenDB(t)
	fake := kriskristest.NewFake(t, "test-key", "test-secret")
	seed := kriskristest.Seed(t, pool, kriskristest.SeedOpts{Tag: "http4"})
	cust := seed.CustomerID
	app := newTestApp(t, pool, fake, &cust)

	code, body := do(t, app, jsonReq(t, http.MethodGet, "/status/"+seed.InvoiceID, ""))
	if code != http.StatusOK {
		t.Fatalf("status = %d %v", code, body)
	}
	data, _ := body["data"].(map[string]any)
	if data["invoice_status"] != "unpaid" || data["attempt"] != nil {
		t.Fatalf("fresh = %v", data)
	}
	code, body = do(t, app, jsonReq(t, http.MethodPost, "/pay/"+seed.InvoiceID, ""))
	if code != http.StatusCreated {
		t.Fatalf("pay = %d", code)
	}
	payData, _ := body["data"].(map[string]any)
	attemptID := payData["attempt_id"].(string)

	code, body = do(t, app, jsonReq(t, http.MethodGet, "/status/"+seed.InvoiceID, ""))
	data, _ = body["data"].(map[string]any)
	att, _ := data["attempt"].(map[string]any)
	if code != http.StatusOK || att["id"] != attemptID || att["status"] != "pending" {
		t.Fatalf("after pay = %d %v", code, body)
	}
	// ?attempt_id= pin works and cross-attempt probing 404s.
	code, _ = do(t, app, jsonReq(t, http.MethodGet, "/status/"+seed.InvoiceID+"?attempt_id="+attemptID, ""))
	if code != http.StatusOK {
		t.Fatalf("pinned = %d", code)
	}
	code, _ = do(t, app, jsonReq(t, http.MethodGet, "/status/"+seed.InvoiceID+"?attempt_id=att-nope", ""))
	if code != http.StatusNotFound {
		t.Fatalf("pinned missing = %d, want 404", code)
	}
}

// TestAttemptStatusByAttemptID covers portal polling: :id is an attempt id
// and returns the flat attempt view the portal parses.
func TestAttemptStatusByAttemptID(t *testing.T) {
	pool := kriskristest.OpenDB(t)
	fake := kriskristest.NewFake(t, "test-key", "test-secret")
	seed := kriskristest.Seed(t, pool, kriskristest.SeedOpts{Tag: "http-att"})
	cust := seed.CustomerID
	app := newTestApp(t, pool, fake, &cust)

	code, body := do(t, app, jsonReq(t, http.MethodPost, "/pay/"+seed.InvoiceID, ""))
	if code != http.StatusCreated {
		t.Fatalf("pay = %d", code)
	}
	payData, _ := body["data"].(map[string]any)
	attemptID := payData["attempt_id"].(string)

	code, body = do(t, app, jsonReq(t, http.MethodGet, "/status/"+attemptID, ""))
	data, _ := body["data"].(map[string]any)
	if code != http.StatusOK || data["id"] != attemptID ||
		data["invoice_id"] != seed.InvoiceID || data["status"] != "pending" {
		t.Fatalf("by attempt = %d %v", code, body)
	}

	code, _ = do(t, app, jsonReq(t, http.MethodGet, "/status/att-nope", ""))
	if code != http.StatusNotFound {
		t.Fatalf("unknown = %d, want 404", code)
	}

	other := "customer-other"
	appOther := newTestApp(t, pool, fake, &other)
	code, _ = do(t, appOther, jsonReq(t, http.MethodGet, "/status/"+attemptID, ""))
	if code != http.StatusNotFound {
		t.Fatalf("cross-customer = %d, want 404", code)
	}
}

func TestWebhookEndpoint(t *testing.T) {
	pool := kriskristest.OpenDB(t)
	fake := kriskristest.NewFake(t, "test-key", "test-secret")
	seed := kriskristest.Seed(t, pool, kriskristest.SeedOpts{Tag: "http5", PPPoEStatus: "isolated"})
	cust := seed.CustomerID
	app := newTestApp(t, pool, fake, &cust)

	code, body := do(t, app, jsonReq(t, http.MethodPost, "/pay/"+seed.InvoiceID, ""))
	if code != http.StatusCreated {
		t.Fatalf("pay = %d", code)
	}
	payData, _ := body["data"].(map[string]any)
	attID := payData["attempt_id"].(string)
	var kid string
	if err := pool.QueryRow(`SELECT kriskris_invoice_id FROM payment_attempts WHERE id=$1`, attID).Scan(&kid); err != nil {
		t.Fatal(err)
	}

	webhook := func(t *testing.T, raw string, sig string, headers map[string]string) (int, map[string]any) {
		t.Helper()
		req := jsonReq(t, http.MethodPost, "/webhook", raw)
		if sig != "" {
			req.Header.Set("X-Kriskris-Signature", sig)
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		return do(t, app, req)
	}

	payload := `{"delivery_id":"dlv-http-1","event":"invoice.paid","invoice_id":"` + kid + `","merchant_id":"m1","amount":86500}`

	// No signature / bad signature -> 401.
	if code, _ := webhook(t, payload, "", nil); code != http.StatusUnauthorized {
		t.Fatalf("unsigned = %d, want 401", code)
	}
	if code, _ := webhook(t, payload, "sha256=deadbeef", nil); code != http.StatusUnauthorized {
		t.Fatalf("bad sig = %d, want 401", code)
	}
	// Stale timestamp header -> 401.
	stale := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	if code, _ := webhook(t, payload, sign(t, "cb-secret", []byte(payload)),
		map[string]string{"X-Kriskris-Timestamp": stale}); code != http.StatusUnauthorized {
		t.Fatalf("stale ts = %d, want 401", code)
	}

	// Valid signature but remote still pending: 200, NOT applied (untrusted).
	code, body = webhook(t, payload, sign(t, "cb-secret", []byte(payload)), nil)
	if code != http.StatusOK {
		t.Fatalf("webhook = %d %v", code, body)
	}
	time.Sleep(300 * time.Millisecond) // let the async confirm run
	if st := kriskristest.InvoiceStatus(t, pool, seed.InvoiceID); st != "unpaid" {
		t.Fatalf("invoice = %q (must stay unpaid)", st)
	}

	// Duplicate delivery -> 200 duplicate.
	code, body = webhook(t, payload, sign(t, "cb-secret", []byte(payload)), nil)
	if code != http.StatusOK {
		t.Fatalf("dup = %d", code)
	}
	dupData, _ := body["data"].(map[string]any)
	if dupData["duplicate"] != true {
		t.Fatalf("dup body = %v", body)
	}

	// Remote flips to paid; new delivery applies asynchronously.
	fake.SetStatus(kid, "paid", payAmount(t, pool, attID))
	payload2 := `{"delivery_id":"dlv-http-2","event":"invoice.paid","invoice_id":"` + kid + `"}`
	code, _ = webhook(t, payload2, sign(t, "cb-secret", []byte(payload2)), nil)
	if code != http.StatusOK {
		t.Fatalf("webhook2 = %d", code)
	}
	deadline := time.Now().Add(5 * time.Second)
	for kriskristest.InvoiceStatus(t, pool, seed.InvoiceID) != "paid" {
		if time.Now().After(deadline) {
			t.Fatal("webhook confirm did not apply")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if pays := kriskristest.ListPayments(t, pool, seed.InvoiceID); len(pays) != 2 {
		t.Fatalf("payments = %+v", pays)
	}
	// Unknown events: 200 ignored.
	other := `{"delivery_id":"dlv-http-3","event":"test.ping"}`
	if code, _ := webhook(t, other, sign(t, "cb-secret", []byte(other)), nil); code != http.StatusOK {
		t.Fatalf("other event = %d, want 200", code)
	}
}

func TestWebhookExpiredClosesAttempt(t *testing.T) {
	pool := kriskristest.OpenDB(t)
	fake := kriskristest.NewFake(t, "test-key", "test-secret")
	seed := kriskristest.Seed(t, pool, kriskristest.SeedOpts{Tag: "http7"})
	cust := seed.CustomerID
	app := newTestApp(t, pool, fake, &cust)

	code, body := do(t, app, jsonReq(t, http.MethodPost, "/pay/"+seed.InvoiceID, ""))
	if code != http.StatusCreated {
		t.Fatalf("pay = %d", code)
	}
	attID := body["data"].(map[string]any)["attempt_id"].(string)
	var kid string
	if err := pool.QueryRow(`SELECT kriskris_invoice_id FROM payment_attempts WHERE id=$1`, attID).Scan(&kid); err != nil {
		t.Fatal(err)
	}
	raw := `{"delivery_id":"dlv-exp-1","event":"invoice.expired","invoice_id":"` + kid + `"}`
	req := jsonReq(t, http.MethodPost, "/webhook", raw)
	req.Header.Set("X-Kriskris-Signature", sign(t, "cb-secret", []byte(raw)))
	if code, _ := do(t, app, req); code != http.StatusOK {
		t.Fatalf("expired = %d, want 200", code)
	}
	if st := kriskristest.AttemptStatus(t, pool, attID); st != "expired" {
		t.Fatalf("attempt = %q", st)
	}
}

func TestWebhookDisabledInPollMode(t *testing.T) {
	pool := kriskristest.OpenDB(t)
	kriskristest.SetSetting(t, pool, "kriskris_mode", "poll")
	fake := kriskristest.NewFake(t, "test-key", "test-secret")
	app := newTestApp(t, pool, fake, nil)
	raw := `{"delivery_id":"dlv-p-1","event":"invoice.paid","invoice_id":"kk-x"}`
	req := jsonReq(t, http.MethodPost, "/webhook", raw)
	req.Header.Set("X-Kriskris-Signature", sign(t, "cb-secret", []byte(raw)))
	code, body := do(t, app, req)
	if code != http.StatusNotFound {
		t.Fatalf("poll mode webhook = %d (%v), want 404", code, body)
	}
	// Flip back to webhook live (no restart): signature verified again.
	kriskristest.SetSetting(t, pool, "kriskris_mode", "webhook")
	req2 := jsonReq(t, http.MethodPost, "/webhook", raw)
	req2.Header.Set("X-Kriskris-Signature", sign(t, "cb-secret", []byte(raw)))
	if code, _ := do(t, app, req2); code != http.StatusOK {
		t.Fatalf("webhook mode = %d, want 200", code)
	}
}

func payAmount(t *testing.T, pool *sql.DB, attemptID string) int64 {
	t.Helper()
	var a int64
	if err := pool.QueryRow(`SELECT amount FROM payment_attempts WHERE id=$1`, attemptID).Scan(&a); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestAdminWithoutLocals(t *testing.T) {
	pool := kriskristest.OpenDB(t)
	fake := kriskristest.NewFake(t, "test-key", "test-secret")
	seed := kriskristest.Seed(t, pool, kriskristest.SeedOpts{Tag: "http6"})
	app := newTestApp(t, pool, fake, nil) // no billing_customer_id -> admin
	code, _ := do(t, app, jsonReq(t, http.MethodPost, "/pay/"+seed.InvoiceID, ""))
	if code != http.StatusCreated {
		t.Fatalf("admin pay = %d", code)
	}
}

func TestUnconfiguredHandler500(t *testing.T) {
	pool := kriskristest.OpenDB(t) // no env, no settings
	t.Setenv("KRISKRIS_BASE_URL", "")
	t.Setenv("KRISKRIS_API_KEY", "")
	t.Setenv("KRISKRIS_API_SECRET", "")
	t.Setenv("KRISKRIS_CALLBACK_SECRET", "")
	t.Setenv("SETTINGS_ENC_KEY", "")
	h := kriskrispres.NewHandler(pool)
	app := fiber.New()
	app.Post("/pay/:id", h.PayInvoice)
	app.Post("/webhook", h.Webhook)
	code, body := do(t, app, jsonReq(t, http.MethodPost, "/pay/inv-x", ""))
	if code != http.StatusInternalServerError {
		t.Fatalf("unconfigured pay = %d %v", code, body)
	}
	errObj, _ := body["error"].(map[string]any)
	if errObj["code"] != "internal" || strings.Contains(bodyString(body), "KRISKRIS") {
		t.Fatalf("must be generic 500: %v", body)
	}
}

func bodyString(body map[string]any) string {
	raw, _ := json.Marshal(body)
	return string(raw)
}
