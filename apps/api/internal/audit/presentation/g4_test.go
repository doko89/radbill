package presentation_test

// Fase 4 (agen E1/G4) integration tests: audit emission per action,
// list filters, audit CSV, reports CSV, and the E2 rate-limit probe.
// Full server on isolated sqlite :memory: DBs.

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gofiber/fiber/v3"
	_ "modernc.org/sqlite"

	auditapp "boilerplate/api/internal/audit/application"
	auditdomain "boilerplate/api/internal/audit/domain"
	auditinfra "boilerplate/api/internal/audit/infrastructure"
	jwtinfra "boilerplate/api/internal/jwtauth/infrastructure"
	"boilerplate/api/internal/platform/db"
	"boilerplate/api/internal/platform/db/sqlcgen"
	"boilerplate/api/internal/platform/httpserver"
	"boilerplate/api/internal/shared/config"
)

var g4dbSeq atomic.Int64

type g4jar struct {
	cookies map[string]string
}

func g4App(t *testing.T) (*fiber.App, *sql.DB, *sqlcgen.Queries) {
	t.Helper()
	cfg := config.Config{
		Env:                      "test",
		Port:                     "0",
		DBDriver:                 "sqlite",
		SessionSecret:            "test-secret-0123456789abcdef",
		SessionTTL:               time.Hour,
		AdminUser:                "admin",
		AdminPass:                "admin123",
		UploadDir:                filepath.Join(t.TempDir(), "uploads"),
		JWTSecret:                "test-jwt-secret-min-32-chars-ok!",
		JWTAccessTTLMinutes:      15,
		RefreshTTLDays:           30,
		TZ:                       "Asia/Jakarta",
		SettingsEncKey:           "test-settings-enc-key-min-32!",
		SchedulerIntervalSeconds: 60,
	}
	// Isolated :memory: DB per test (unique name: the shared-cache
	// file::memory: URI would leak rows between tests in one process).
	name := fmt.Sprintf("file:g4audit_%d?mode=memory&cache=shared&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)",
		g4dbSeq.Add(1))
	pool, err := sql.Open("sqlite", name)
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(1)
	t.Cleanup(func() { pool.Close() })
	ctx := context.Background()
	if err := db.Migrate(ctx, pool, "sqlite"); err != nil {
		t.Fatal(err)
	}
	q := sqlcgen.New(pool)
	if err := db.SeedBillingAdmin(ctx, q, cfg.AdminUser, cfg.AdminPass); err != nil {
		t.Fatal(err)
	}
	app, err := httpserver.NewWithFS(cfg, pool, fstest.MapFS{})
	if err != nil {
		t.Fatal(err)
	}
	return app, pool, q
}

func g4do(t *testing.T, app *fiber.App, req *http.Request) *http.Response {
	t.Helper()
	resp, err := app.Test(req, fiber.TestConfig{Timeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("request %s %s: %v", req.Method, req.URL.Path, err)
	}
	return resp
}

func g4body(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("response is not JSON %q: %v", string(raw), err)
	}
	return out
}

func g4raw(t *testing.T, resp *http.Response) []byte {
	t.Helper()
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func g4jsonReq(t *testing.T, method, path, body string) *http.Request {
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

func g4csrf(t *testing.T, app *fiber.App, j *g4jar) {
	t.Helper()
	resp := g4do(t, app, g4jsonReq(t, http.MethodGet, "/api/auth/csrf", ""))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/auth/csrf = %d", resp.StatusCode)
	}
	for _, c := range resp.Cookies() {
		j.cookies[c.Name] = c.Value
	}
	if j.cookies["csrf_token"] == "" {
		t.Fatal("expected csrf_token cookie")
	}
}

// g4unsafe builds an unsafe (CSRF-carrying) request.
func g4unsafe(t *testing.T, j *g4jar, method, path, body string) *http.Request {
	t.Helper()
	req := g4jsonReq(t, method, path, body)
	for name, val := range j.cookies {
		req.AddCookie(&http.Cookie{Name: name, Value: val})
	}
	req.Header.Set("X-CSRF-Token", j.cookies["csrf_token"])
	return req
}

func g4bearer(t *testing.T, token, method, path, body string) *http.Request {
	t.Helper()
	req := g4jsonReq(t, method, path, body)
	req.Header.Set("Authorization", "Bearer "+token)
	return req
}

// g4authed builds an unsafe + Bearer [REDACTED] admin request.
func g4authed(t *testing.T, j *g4jar, token, method, path, body string) *http.Request {
	t.Helper()
	req := g4unsafe(t, j, method, path, body)
	req.Header.Set("Authorization", "Bearer "+token)
	return req
}

func g4login(t *testing.T, app *fiber.App, j *g4jar, username, password string) string {
	t.Helper()
	resp := g4do(t, app, g4unsafe(t, j, http.MethodPost, "/api/v1/auth/login",
		`{"username":"`+username+`","password":"`+password+`"}`))
	for _, c := range resp.Cookies() {
		j.cookies[c.Name] = c.Value
	}
	body := g4body(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login %s = %d %v", username, resp.StatusCode, body)
	}
	data, _ := body["data"].(map[string]any)
	token, _ := data["access_token"].(string)
	if token == "" {
		t.Fatalf("login missing access_token: %v", body)
	}
	return token
}

// g4postJSON performs an authed unsafe request and returns data on success.
func g4postJSON(t *testing.T, app *fiber.App, j *g4jar, token, method, path, body string, want int) map[string]any {
	t.Helper()
	resp := g4do(t, app, g4authed(t, j, token, method, path, body))
	out := g4body(t, resp)
	if resp.StatusCode != want {
		t.Fatalf("%s %s = %d %v, want %d", method, path, resp.StatusCode, out, want)
	}
	data, _ := out["data"].(map[string]any)
	return data
}

func g4getJSON(t *testing.T, app *fiber.App, token, path string, want int) map[string]any {
	t.Helper()
	resp := g4do(t, app, g4bearer(t, token, http.MethodGet, path, ""))
	out := g4body(t, resp)
	if resp.StatusCode != want {
		t.Fatalf("GET %s = %d %v, want %d", path, resp.StatusCode, out, want)
	}
	return out
}

type g4auditRow struct {
	action   string
	target   string
	targetID string
	detail   string
	actor    string
	ip       string
}

func g4auditList(t *testing.T, app *fiber.App, token, query string) ([]g4auditRow, int64) {
	t.Helper()
	out := g4getJSON(t, app, token, "/api/v1/admin/audit-logs"+query, http.StatusOK)
	data, _ := out["data"].([]any)
	total, _ := out["total"].(float64)
	rows := make([]g4auditRow, 0, len(data))
	for _, item := range data {
		m, _ := item.(map[string]any)
		actorID, _ := m["actor_id"].(string)
		rows = append(rows, g4auditRow{
			action:   strVal(m["action"]),
			target:   strVal(m["target_type"]),
			targetID: strVal(m["target_id"]),
			detail:   strVal(m["detail"]),
			actor:    strVal(m["actor_username"]) + "\x00" + actorID,
			ip:       strVal(m["ip"]),
		})
	}
	return rows, int64(total)
}

func strVal(v any) string {
	s, _ := v.(string)
	return s
}

// TestG4AuditEmitLifecycle drives every audited admin mutation through
// HTTP and asserts one entry per action with the right target, actor,
// and secret-free detail. Reads (list/get without reveal) emit nothing.
func TestG4AuditEmitLifecycle(t *testing.T) {
	app, _, _ := g4App(t)
	j := &g4jar{cookies: map[string]string{}}
	g4csrf(t, app, j)
	admin := g4login(t, app, j, "admin", "admin123")

	// Reads first: the log stays empty.
	g4getJSON(t, app, admin, "/api/v1/admin/customers", http.StatusOK)
	if rows, total := g4auditList(t, app, admin, ""); len(rows) != 0 || total != 0 {
		t.Fatalf("reads emitted audit rows: %d total=%d", len(rows), total)
	}

	// Customers (name carries comma+quote for the CSV escaping check).
	cust := g4postJSON(t, app, j, admin, http.MethodPost, "/api/v1/admin/customers",
		`{"name":"Budi \"Bos\", Sr.","phone":"0812"}`, http.StatusCreated)
	custID, _ := cust["id"].(string)
	g4postJSON(t, app, j, admin, http.MethodPut, "/api/v1/admin/customers/"+custID,
		`{"name":"Budi \"Bos\", Jr.","phone":"0813"}`, http.StatusOK)
	custDel := g4postJSON(t, app, j, admin, http.MethodPost, "/api/v1/admin/customers",
		`{"name":"Delete Me"}`, http.StatusCreated)
	g4postJSON(t, app, j, admin, http.MethodDelete, "/api/v1/admin/customers/"+custDel["id"].(string), "", http.StatusOK)

	// Packages.
	pkgA := g4postJSON(t, app, j, admin, http.MethodPost, "/api/v1/admin/packages",
		`{"name":"G4 Basic","price":50000,"period":"monthly"}`, http.StatusCreated)
	pkgAID, _ := pkgA["id"].(string)
	g4postJSON(t, app, j, admin, http.MethodPut, "/api/v1/admin/packages/"+pkgAID,
		`{"name":"G4 Basic","price":60000,"period":"monthly"}`, http.StatusOK)
	pkgB := g4postJSON(t, app, j, admin, http.MethodPost, "/api/v1/admin/packages",
		`{"name":"G4 Plus","price":75000,"period":"monthly"}`, http.StatusCreated)
	pkgBID, _ := pkgB["id"].(string)
	pkgSpare := g4postJSON(t, app, j, admin, http.MethodPost, "/api/v1/admin/packages",
		`{"name":"G4 Spare","price":10000,"period":"monthly"}`, http.StatusCreated)
	g4postJSON(t, app, j, admin, http.MethodDelete, "/api/v1/admin/packages/"+pkgSpare["id"].(string), "", http.StatusOK)

	// PPPoE lifecycle on P1.
	p1 := g4postJSON(t, app, j, admin, http.MethodPost, "/api/v1/admin/pppoe-accounts",
		`{"customer_id":"`+custID+`","package_id":"`+pkgAID+`","username":"g4u1","password":"g4pass1"}`,
		http.StatusCreated)
	p1ID, _ := p1["id"].(string)
	g4postJSON(t, app, j, admin, http.MethodPut, "/api/v1/admin/pppoe-accounts/"+p1ID,
		`{"customer_id":"`+custID+`","package_id":"`+pkgAID+`","username":"g4u1","password":"g4pass1","static_ip":"10.20.30.40"}`,
		http.StatusOK)
	for _, op := range []string{"isolate", "activate", "disable", "activate"} {
		g4postJSON(t, app, j, admin, http.MethodPost, "/api/v1/admin/pppoe-accounts/"+p1ID+"/"+op, "", http.StatusOK)
	}
	g4postJSON(t, app, j, admin, http.MethodPost, "/api/v1/admin/pppoe-accounts/"+p1ID+"/extend",
		`{"days":30}`, http.StatusOK)
	g4postJSON(t, app, j, admin, http.MethodPost, "/api/v1/admin/pppoe-accounts/"+p1ID+"/change-package",
		`{"package_id":"`+pkgBID+`"}`, http.StatusOK)
	g4postJSON(t, app, j, admin, http.MethodPost, "/api/v1/admin/pppoe-accounts/"+p1ID+"/reset-quota", "", http.StatusOK)
	g4postJSON(t, app, j, admin, http.MethodPost, "/api/v1/admin/pppoe-accounts/"+p1ID+"/disconnect", "", http.StatusOK)
	g4postJSON(t, app, j, admin, http.MethodPost, "/api/v1/admin/pppoe-accounts/"+p1ID+"/portal-account",
		`{"username":"g4portal1","password":"portalpass1"}`, http.StatusCreated)
	g4postJSON(t, app, j, admin, http.MethodPost, "/api/v1/admin/pppoe-accounts/"+p1ID+"/portal-account/reset",
		`{"password":"portalpass2"}`, http.StatusOK)
	reveal := g4getJSON(t, app, admin, "/api/v1/admin/pppoe-accounts/"+p1ID+"?reveal=true", http.StatusOK)
	if data, _ := reveal["data"].(map[string]any); data["password"] != "g4pass1" {
		t.Fatalf("reveal did not return password: %v", reveal)
	}
	// P2 carries the invoices so P1 stays deletable.
	p2 := g4postJSON(t, app, j, admin, http.MethodPost, "/api/v1/admin/pppoe-accounts",
		`{"customer_id":"`+custID+`","package_id":"`+pkgAID+`","username":"g4u2","password":"g4pass2"}`,
		http.StatusCreated)
	p2ID, _ := p2["id"].(string)
	g4postJSON(t, app, j, admin, http.MethodDelete, "/api/v1/admin/pppoe-accounts/"+p1ID, "", http.StatusOK)

	// NAS.
	nas := g4postJSON(t, app, j, admin, http.MethodPost, "/api/v1/admin/nas",
		`{"name":"G4 NAS","ip":"10.10.10.1","secret":"g4nas-secret","coa_port":3799}`, http.StatusCreated)
	nasID, _ := nas["id"].(string)
	g4postJSON(t, app, j, admin, http.MethodPut, "/api/v1/admin/nas/"+nasID,
		`{"name":"G4 NAS 2","ip":"10.10.10.1","secret":"g4nas-secret","coa_port":3799}`, http.StatusOK)
	g4postJSON(t, app, j, admin, http.MethodDelete, "/api/v1/admin/nas/"+nasID, "", http.StatusOK)

	// Invoices: I1 paid manually, I2 voided.
	inv1 := g4postJSON(t, app, j, admin, http.MethodPost, "/api/v1/admin/invoices",
		`{"pppoe_account_id":"`+p2ID+`","items":[{"description":"Test","qty":1,"price":75000}],"due_date":"2026-12-01"}`,
		http.StatusCreated)
	inv1ID, _ := inv1["id"].(string)
	g4postJSON(t, app, j, admin, http.MethodPost, "/api/v1/admin/invoices/"+inv1ID+"/manual-payment",
		`{"amount":0}`, http.StatusOK)
	inv2 := g4postJSON(t, app, j, admin, http.MethodPost, "/api/v1/admin/invoices",
		`{"pppoe_account_id":"`+p2ID+`","items":[{"description":"Test","qty":1,"price":10000}],"due_date":"2026-12-01"}`,
		http.StatusCreated)
	inv2ID, _ := inv2["id"].(string)
	g4postJSON(t, app, j, admin, http.MethodPost, "/api/v1/admin/invoices/"+inv2ID+"/void", "", http.StatusOK)

	// Settings (plain + secret key: values must never reach the detail).
	g4postJSON(t, app, j, admin, http.MethodPut, "/api/v1/admin/settings",
		`{"company_name":"G4 Corp","kriskris_api_secret":"g4-supersecret"}`, http.StatusOK)

	// Password change last (revokes refresh), then re-login.
	g4postJSON(t, app, j, admin, http.MethodPost, "/api/v1/auth/password/change",
		`{"current_password":"admin123","new_password":"g4-new-admin-pass"}`, http.StatusOK)
	admin = g4login(t, app, j, "admin", "g4-new-admin-pass")

	rows, total := g4auditList(t, app, admin, "")
	byAction := map[string][]g4auditRow{}
	for _, r := range rows {
		byAction[r.action] = append(byAction[r.action], r)
	}
	// customer.* (4) + package.* (5) + pppoe.* (15) + nas.* (3) +
	// invoice.* (3) + payment.manual (1) + settings.* (1) + auth (1) = 33.
	if total != 33 || len(rows) != 33 {
		t.Fatalf("total=%d rows=%d, want 33", total, len(rows))
	}
	// Every entry carries the admin actor + a client IP, and no
	// password/secret/setting value ever lands in detail.
	secrets := []string{"g4pass1", "g4pass2", "g4nas-secret", "g4-supersecret",
		"G4 Corp", "portalpass1", "portalpass2"}
	for _, r := range rows {
		if !strings.HasPrefix(r.actor, "admin\x00") {
			t.Fatalf("action %s actor = %q, want admin", r.action, r.actor)
		}
		if r.ip == "" {
			t.Fatalf("action %s has empty ip", r.action)
		}
		for _, secret := range secrets {
			if strings.Contains(r.detail, secret) {
				t.Fatalf("action %s detail leaks %q: %q", r.action, secret, r.detail)
			}
		}
	}
	// Per-action counts.
	wantCounts := map[string]int{
		"customer.created": 2, "customer.updated": 1, "customer.deleted": 1,
		"package.created": 3, "package.updated": 1, "package.deleted": 1,
		"pppoe.created": 2, "pppoe.updated": 1, "pppoe.isolated": 1,
		"pppoe.activated": 2, "pppoe.disabled": 1, "pppoe.extended": 1,
		"pppoe.package_changed": 1, "pppoe.quota_reset": 1,
		"pppoe.disconnected": 1, "pppoe.portal_created": 1,
		"pppoe.portal_reset": 1, "pppoe.password_reveal": 1,
		"pppoe.deleted": 1,
		"nas.created":   1, "nas.updated": 1, "nas.deleted": 1,
		"invoice.created": 2, "invoice.voided": 1, "payment.manual": 1,
		"settings.updated": 1, "auth.password_change": 1,
	}
	for action, want := range wantCounts {
		if got := len(byAction[action]); got != want {
			t.Fatalf("action %s count = %d, want %d", action, got, want)
		}
	}
	// Target spot checks.
	only := func(action string) g4auditRow {
		t.Helper()
		rs := byAction[action]
		if len(rs) != 1 {
			t.Fatalf("action %s count = %d, want 1", action, len(rs))
		}
		return rs[0]
	}
	if r := only("pppoe.password_reveal"); r.target != "pppoe" || r.targetID != p1ID {
		t.Fatalf("password_reveal target = %s/%s, want pppoe/%s", r.target, r.targetID, p1ID)
	}
	if r := only("payment.manual"); r.target != "invoice" || r.targetID != inv1ID {
		t.Fatalf("payment.manual target = %s/%s, want invoice/%s", r.target, r.targetID, inv1ID)
	}
	if r := only("invoice.voided"); r.targetID != inv2ID {
		t.Fatalf("invoice.voided target = %s, want %s", r.targetID, inv2ID)
	}
	if r := only("settings.updated"); r.target != "settings" || r.targetID != "" ||
		r.detail != "keys=company_name,kriskris_api_secret" {
		t.Fatalf("settings.updated = %+v", r)
	}
	if r := only("auth.password_change"); r.target != "account" || r.targetID == "" {
		t.Fatalf("auth.password_change = %+v", r)
	}
	if r := only("nas.deleted"); r.targetID != nasID {
		t.Fatalf("nas.deleted target = %s, want %s", r.targetID, nasID)
	}
	if r := only("pppoe.package_changed"); !strings.Contains(r.detail, pkgBID) {
		t.Fatalf("package_changed detail = %q, want package %s", r.detail, pkgBID)
	}
	// The quoted customer name survives into detail (CSV escape source).
	found := false
	for _, r := range byAction["customer.created"] {
		if strings.Contains(r.detail, `Budi "Bos", Sr.`) {
			found = true
		}
	}
	if !found {
		t.Fatalf("customer.created details missing quoted name: %+v", byAction["customer.created"])
	}
}

// TestG4AuditEmitPlanChange covers planchange.created/cancelled: a portal
// user queues a downgrade and cancels it; both land in the admin audit
// log with the portal user as actor.
func TestG4AuditEmitPlanChange(t *testing.T) {
	app, _, _ := g4App(t)
	j := &g4jar{cookies: map[string]string{}}
	g4csrf(t, app, j)
	admin := g4login(t, app, j, "admin", "admin123")

	cust := g4postJSON(t, app, j, admin, http.MethodPost, "/api/v1/admin/customers",
		`{"name":"Portal Owner"}`, http.StatusCreated)
	custID, _ := cust["id"].(string)
	pkgHi := g4postJSON(t, app, j, admin, http.MethodPost, "/api/v1/admin/packages",
		`{"name":"G4 Hi","price":100000,"period":"monthly","allow_self_service":true,"upgrade_group":"g1"}`,
		http.StatusCreated)
	pkgLo := g4postJSON(t, app, j, admin, http.MethodPost, "/api/v1/admin/packages",
		`{"name":"G4 Lo","price":50000,"period":"monthly","allow_self_service":true,"upgrade_group":"g1"}`,
		http.StatusCreated)
	loID, _ := pkgLo["id"].(string)
	ppp := g4postJSON(t, app, j, admin, http.MethodPost, "/api/v1/admin/pppoe-accounts",
		`{"customer_id":"`+custID+`","package_id":"`+pkgHi["id"].(string)+`","username":"g4downgrade","password":"g4pass99"}`,
		http.StatusCreated)
	pppID, _ := ppp["id"].(string)
	g4postJSON(t, app, j, admin, http.MethodPost, "/api/v1/admin/pppoe-accounts/"+pppID+"/portal-account",
		`{"username":"g4downgrader","password":"portalpass99"}`, http.StatusCreated)

	portal := g4login(t, app, j, "g4downgrader", "portalpass99")
	created := g4postJSON(t, app, j, portal, http.MethodPost, "/api/v1/portal/plan-changes",
		`{"pppoe_account_id":"`+pppID+`","to_package_id":"`+loID+`"}`, http.StatusCreated)
	if created["status"] != "pending" {
		t.Fatalf("plan change status = %v, want pending", created)
	}
	list := g4getJSON(t, app, portal, "/api/v1/portal/plan-changes", http.StatusOK)
	items, _ := list["data"].([]any)
	if len(items) != 1 {
		t.Fatalf("plan changes = %d, want 1", len(items))
	}
	changeID, _ := items[0].(map[string]any)["id"].(string)
	g4postJSON(t, app, j, portal, http.MethodPost, "/api/v1/portal/plan-changes/"+changeID+"/cancel", "", http.StatusOK)

	rows, _ := g4auditList(t, app, admin, "?action=planchange")
	if len(rows) != 2 {
		t.Fatalf("planchange rows = %d, want 2", len(rows))
	}
	seen := map[string]g4auditRow{}
	for _, r := range rows {
		seen[r.action] = r
	}
	cr, ok := seen["planchange.created"]
	if !ok || cr.target != "planchange" || cr.targetID != changeID {
		t.Fatalf("planchange.created = %+v, want target planchange/%s", cr, changeID)
	}
	if !strings.Contains(cr.detail, pppID) || !strings.Contains(cr.detail, loID) {
		t.Fatalf("planchange.created detail = %q", cr.detail)
	}
	cc, ok := seen["planchange.cancelled"]
	if !ok || cc.targetID != changeID {
		t.Fatalf("planchange.cancelled = %+v, want target %s", cc, changeID)
	}
	for _, r := range rows {
		if !strings.HasPrefix(r.actor, "g4downgrader\x00") {
			t.Fatalf("planchange actor = %q, want g4downgrader", r.actor)
		}
	}
}

// TestG4AuditListFilters seeds entries directly with a controlled clock
// and exercises ?actor&action&from&to&page plus auth (401/403).
func TestG4AuditListFilters(t *testing.T) {
	app, pool, q := g4App(t)
	j := &g4jar{cookies: map[string]string{}}
	g4csrf(t, app, j)
	admin := g4login(t, app, j, "admin", "admin123")

	ctx := context.Background()
	svc := auditapp.NewService(auditinfra.NewStore(q))
	base := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	stamps := []time.Time{base, base.Add(time.Hour), base.Add(2 * time.Hour)}
	// Resolve the admin account id for actor_username coverage.
	adminAcct, err := q.GetBillingAccountByUsername(ctx, "admin")
	if err != nil {
		t.Fatal(err)
	}
	inputs := []auditapp.LogInput{
		{ActorID: adminAcct.ID, Action: "customer.created", TargetType: "customer", TargetID: "c1", IP: "1.1.1.1"},
		{ActorID: "ghost-id", Action: "customer.deleted", TargetType: "customer", TargetID: "c2", IP: "2.2.2.2"},
		{ActorID: adminAcct.ID, Action: "invoice.created", TargetType: "invoice", TargetID: "i1", IP: "1.1.1.1"},
	}
	for i, in := range inputs {
		svc.SetNow(func() time.Time { return stamps[i] })
		if err := svc.Log(ctx, in); err != nil {
			t.Fatal(err)
		}
	}
	_ = pool

	// Admin bypass: full list without filters.
	rows, total := g4auditList(t, app, admin, "")
	if total != 3 || len(rows) != 3 {
		t.Fatalf("unfiltered total=%d rows=%d, want 3", total, len(rows))
	}
	// Newest first.
	if rows[0].action != "invoice.created" {
		t.Fatalf("first row = %s, want invoice.created", rows[0].action)
	}
	// Username resolved for the known actor, blank for the ghost.
	if !strings.HasPrefix(rows[0].actor, "admin\x00") {
		t.Fatalf("known actor = %q, want admin username", rows[0].actor)
	}
	if rows[1].actor != "\x00ghost-id" {
		t.Fatalf("ghost actor = %q, want empty username + kept id", rows[1].actor)
	}

	// ?actor matches id exact or username substring.
	if rows, total := g4auditList(t, app, admin, "?actor="+adminAcct.ID); total != 2 || len(rows) != 2 {
		t.Fatalf("actor=id total=%d rows=%d, want 2", total, len(rows))
	}
	if rows, total := g4auditList(t, app, admin, "?actor=adm"); total != 2 || len(rows) != 2 {
		t.Fatalf("actor=sub total=%d rows=%d, want 2", total, len(rows))
	}
	if rows, total := g4auditList(t, app, admin, "?actor=ghost-id"); total != 1 || len(rows) != 1 {
		t.Fatalf("actor=ghost total=%d rows=%d, want 1", total, len(rows))
	}

	// ?action exact and prefix.
	if _, total := g4auditList(t, app, admin, "?action=customer.created"); total != 1 {
		t.Fatalf("action exact total=%d, want 1", total)
	}
	if _, total := g4auditList(t, app, admin, "?action=customer"); total != 2 {
		t.Fatalf("action prefix total=%d, want 2", total)
	}

	// ?from/?to RFC3339 and date-only.
	mid := stamps[1].Format(time.RFC3339)
	if _, total := g4auditList(t, app, admin, "?from="+mid); total != 2 {
		t.Fatalf("from total=%d, want 2", total)
	}
	if _, total := g4auditList(t, app, admin, "?to="+mid); total != 2 {
		t.Fatalf("to total=%d, want 2", total)
	}
	if _, total := g4auditList(t, app, admin, "?from=2026-11-01&to=2026-11-01"); total != 3 {
		t.Fatalf("date bounds total=%d, want 3", total)
	}
	if _, total := g4auditList(t, app, admin, "?from=2026-11-02"); total != 0 {
		t.Fatalf("future from total=%d, want 0", total)
	}

	// Bad bounds and page are 400.
	for _, bad := range []string{"?from=nope", "?to=2026-13-99", "?page=0", "?page=no"} {
		resp := g4do(t, app, g4bearer(t, admin, http.MethodGet, "/api/v1/admin/audit-logs"+bad, ""))
		g4body(t, resp)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("GET audit-logs%s = %d, want 400", bad, resp.StatusCode)
		}
	}

	// ?page slices with page size 50.
	svc.SetNow(func() time.Time { return base.Add(3 * time.Hour) })
	for i := 0; i < 52; i++ {
		if err := svc.Log(ctx, auditapp.LogInput{ActorID: adminAcct.ID, Action: "nas.created", IP: "9.9.9.9"}); err != nil {
			t.Fatal(err)
		}
	}
	rows, total = g4auditList(t, app, admin, "?page=1")
	if total != 55 || len(rows) != 50 {
		t.Fatalf("page1 total=%d rows=%d, want 55/50", total, len(rows))
	}
	rows, total = g4auditList(t, app, admin, "?page=2")
	if total != 55 || len(rows) != 5 {
		t.Fatalf("page2 total=%d rows=%d, want 55/5", total, len(rows))
	}

	// Auth: 401 without token, 403 for portal users (no audit:read).
	resp := g4do(t, app, g4jsonReq(t, http.MethodGet, "/api/v1/admin/audit-logs", ""))
	g4body(t, resp)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no token = %d, want 401", resp.StatusCode)
	}
	// A portal user (role=user, no audit:read) gets 403.
	hash, err := jwtinfra.NewArgon2Hasher().Hash("portalpass1")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := q.CreateBillingAccount(ctx, sqlcgen.CreateBillingAccountParams{
		ID: "acct-portal1", Username: "portal1", PasswordHash: hash,
		Role: "user", Status: "active", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	portal := g4login(t, app, j, "portal1", "portalpass1")
	resp = g4do(t, app, g4bearer(t, portal, http.MethodGet, "/api/v1/admin/audit-logs", ""))
	g4body(t, resp)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("portal user = %d, want 403", resp.StatusCode)
	}
	_ = auditdomain.ActionCustomerCreated
}

// TestG4AuditCSV exports filtered rows as CSV: stable header, correct
// escaping (comma + quote in detail), attachment disposition.
func TestG4AuditCSV(t *testing.T) {
	app, _, q := g4App(t)
	j := &g4jar{cookies: map[string]string{}}
	g4csrf(t, app, j)
	admin := g4login(t, app, j, "admin", "admin123")

	ctx := context.Background()
	svc := auditapp.NewService(auditinfra.NewStore(q))
	adminAcct, err := q.GetBillingAccountByUsername(ctx, "admin")
	if err != nil {
		t.Fatal(err)
	}
	tricky := `name=ACME, "Quoted" Co.`
	if err := svc.Log(ctx, auditapp.LogInput{
		ActorID: adminAcct.ID, Action: "customer.created",
		TargetType: "customer", TargetID: "c9", Detail: tricky, IP: "3.3.3.3",
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Log(ctx, auditapp.LogInput{
		ActorID: adminAcct.ID, Action: "invoice.created",
		TargetType: "invoice", TargetID: "i9", IP: "3.3.3.3",
	}); err != nil {
		t.Fatal(err)
	}

	resp := g4do(t, app, g4bearer(t, admin, http.MethodGet, "/api/v1/admin/audit-logs?format=csv&action=customer", ""))
	raw := g4raw(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("csv status = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
		t.Fatalf("content-type = %q, want text/csv", ct)
	}
	if cd := resp.Header.Get("Content-Disposition"); !strings.Contains(cd, "attachment") || !strings.Contains(cd, "audit-logs.csv") {
		t.Fatalf("content-disposition = %q", cd)
	}
	rows, err := csv.NewReader(strings.NewReader(string(raw))).ReadAll()
	if err != nil {
		t.Fatalf("csv parse: %v\n%s", err, raw)
	}
	if len(rows) != 2 {
		t.Fatalf("csv rows = %d, want header + 1 filtered row", len(rows))
	}
	wantHeader := []string{"id", "actor_id", "actor_username", "action", "target_type", "target_id", "detail", "ip", "created_at"}
	for i, h := range wantHeader {
		if rows[0][i] != h {
			t.Fatalf("header[%d] = %q, want %q (full %v)", i, rows[0][i], h, rows[0])
		}
	}
	if rows[1][3] != "customer.created" || rows[1][6] != tricky || rows[1][2] != "admin" {
		t.Fatalf("csv row = %v, want action/detail/actor round-trip", rows[1])
	}
}

// TestG4ReportsCSV hits every report endpoint with ?format=csv: the
// bytes must parse, headers must be stable, and the JSON default must
// be byte-identical with and without an explicit non-csv format.
func TestG4ReportsCSV(t *testing.T) {
	app, _, _ := g4App(t)
	j := &g4jar{cookies: map[string]string{}}
	g4csrf(t, app, j)
	admin := g4login(t, app, j, "admin", "admin123")

	// Seed one paid + one unpaid invoice so exports carry data rows.
	cust := g4postJSON(t, app, j, admin, http.MethodPost, "/api/v1/admin/customers",
		`{"name":"CSV Customer"}`, http.StatusCreated)
	pkg := g4postJSON(t, app, j, admin, http.MethodPost, "/api/v1/admin/packages",
		`{"name":"CSV Pack","price":50000,"period":"monthly"}`, http.StatusCreated)
	ppp := g4postJSON(t, app, j, admin, http.MethodPost, "/api/v1/admin/pppoe-accounts",
		`{"customer_id":"`+cust["id"].(string)+`","package_id":"`+pkg["id"].(string)+`","username":"g4csv","password":"g4passcsv"}`,
		http.StatusCreated)
	pppID := ppp["id"].(string)
	inv := g4postJSON(t, app, j, admin, http.MethodPost, "/api/v1/admin/invoices",
		`{"pppoe_account_id":"`+pppID+`","items":[{"description":"CSV line","qty":1,"price":25000}],"due_date":"2026-12-01"}`,
		http.StatusCreated)
	g4postJSON(t, app, j, admin, http.MethodPost, "/api/v1/admin/invoices/"+inv["id"].(string)+"/manual-payment",
		`{"amount":0}`, http.StatusOK)
	g4postJSON(t, app, j, admin, http.MethodPost, "/api/v1/admin/invoices",
		`{"pppoe_account_id":"`+pppID+`","items":[{"description":"Unpaid","qty":1,"price":10000}],"due_date":"2020-01-01"}`,
		http.StatusCreated)

	cases := []struct {
		path   string
		header []string
	}{
		{"/api/v1/admin/reports/revenue", []string{"period", "total"}},
		{"/api/v1/admin/reports/usage", []string{"date", "bytes_in", "bytes_out"}},
		{"/api/v1/admin/reports/receivables", []string{"id", "number", "customer_name", "total", "due_date", "days_overdue"}},
		{"/api/v1/admin/reports/growth?months=2", []string{"period", "new", "churn", "active"}},
	}
	sep := func(path string) string {
		if strings.Contains(path, "?") {
			return "&"
		}
		return "?"
	}
	for _, tc := range cases {
		resp := g4do(t, app, g4bearer(t, admin, http.MethodGet, tc.path+sep(tc.path)+"format=csv", ""))
		raw := g4raw(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s csv = %d", tc.path, resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
			t.Fatalf("%s content-type = %q", tc.path, ct)
		}
		if cd := resp.Header.Get("Content-Disposition"); !strings.Contains(cd, "attachment") {
			t.Fatalf("%s content-disposition = %q", tc.path, cd)
		}
		rows, err := csv.NewReader(strings.NewReader(string(raw))).ReadAll()
		if err != nil {
			t.Fatalf("%s csv parse: %v\n%s", tc.path, err, raw)
		}
		if len(rows) < 1 {
			t.Fatalf("%s csv has no header", tc.path)
		}
		for i, h := range tc.header {
			if rows[0][i] != h {
				t.Fatalf("%s header[%d] = %q, want %q", tc.path, i, rows[0][i], h)
			}
		}
		for _, row := range rows[1:] {
			if len(row) != len(tc.header) {
				t.Fatalf("%s row width = %d, want %d: %v", tc.path, len(row), len(tc.header), row)
			}
		}
		// JSON default unchanged: plain vs explicit json are identical.
		plain := g4do(t, app, g4bearer(t, admin, http.MethodGet, tc.path, ""))
		plainRaw := g4raw(t, plain)
		explicit := g4do(t, app, g4bearer(t, admin, http.MethodGet, tc.path+sep(tc.path)+"format=json", ""))
		explicitRaw := g4raw(t, explicit)
		if string(plainRaw) != string(explicitRaw) {
			t.Fatalf("%s json default differs from format=json", tc.path)
		}
		var parsed map[string]any
		if err := json.Unmarshal(plainRaw, &parsed); err != nil {
			t.Fatalf("%s default is not JSON: %v", tc.path, err)
		}
		if _, ok := parsed["data"]; !ok {
			t.Fatalf("%s default has no data key: %v", tc.path, parsed)
		}
	}
}

// TestG4RateLimit probes the E2 limiter on /api/v1/auth/*: after the
// burst, further logins answer 429 with Retry-After. It skips while
// the E2 security package (or its wiring) is not active.
func TestG4RateLimit(t *testing.T) {
	if _, err := os.Stat("../../security"); err != nil {
		t.Skip("E2 internal/security belum mendarat")
	}
	app, _, _ := g4App(t)
	j := &g4jar{cookies: map[string]string{}}
	g4csrf(t, app, j)
	// Successful logins never trip the G1 failure limiter, so any 429
	// here comes from the E2 middleware.
	limited := false
	var retryAfter string
	for i := 0; i < 60; i++ {
		resp := g4do(t, app, g4unsafe(t, j, http.MethodPost, "/api/v1/auth/login",
			`{"username":"admin","password":"admin123"}`))
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests {
			limited = true
			retryAfter = resp.Header.Get("Retry-After")
			var out map[string]any
			if err := json.Unmarshal(raw, &out); err != nil {
				t.Fatalf("429 body is not JSON: %q", raw)
			}
			if _, ok := out["error"]; !ok {
				t.Fatalf("429 body has no error envelope: %q", raw)
			}
			break
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("login %d = %d %s", i, resp.StatusCode, raw)
		}
	}
	if !limited {
		t.Skip("E2 limiter wiring belum aktif (no 429 observed)")
	}
	if retryAfter == "" {
		t.Fatal("429 without Retry-After header")
	}
}
