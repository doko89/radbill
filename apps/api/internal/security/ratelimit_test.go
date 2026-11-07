package security

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
)

// newLimitApp builds a minimal app with Limit wired before a 200 handler.
func newLimitApp(rpm, burst int) *fiber.App {
	app := fiber.New()
	app.Use(Limit(rpm, burst))
	app.Get("/", func(c fiber.Ctx) error { return c.SendString("ok") })
	return app
}

func get(t *testing.T, app *fiber.App, headers map[string]string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := app.Test(req, fiber.TestConfig{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	return resp
}

func TestLimitBurstThen429(t *testing.T) {
	app := newLimitApp(60, 3)
	for i := 0; i < 3; i++ {
		resp := get(t, app, nil)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("burst request %d = %d, want 200", i+1, resp.StatusCode)
		}
	}
	resp := get(t, app, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("over-burst = %d, want 429", resp.StatusCode)
	}
	if ra := resp.Header.Get("Retry-After"); ra == "" {
		t.Fatal("429 missing Retry-After header")
	} else if n, err := strconv.Atoi(ra); err != nil || n < 1 {
		t.Fatalf("Retry-After = %q, want positive integer seconds", ra)
	}
	raw, _ := io.ReadAll(resp.Body)
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("429 body is not JSON: %q", string(raw))
	}
	errObj, _ := body["error"].(map[string]any)
	if errObj["code"] != "rate_limited" {
		t.Fatalf("429 envelope = %v, want error.code rate_limited", body)
	}
}

func TestLimitRefill(t *testing.T) {
	now := time.Now()
	l := newLimiter(60, 1) // 1 token/sec
	l.now = func() time.Time { return now }

	if ok, _ := l.allow("10.0.0.1"); !ok {
		t.Fatal("first request denied, want allow")
	}
	if ok, _ := l.allow("10.0.0.1"); ok {
		t.Fatal("second immediate request allowed, want limit")
	}
	now = now.Add(1100 * time.Millisecond) // >1 token refilled
	if ok, _ := l.allow("10.0.0.1"); !ok {
		t.Fatal("request after refill denied, want allow")
	}
}

func TestLimitIsolatesIPs(t *testing.T) {
	l := newLimiter(60, 2)
	if ok, _ := l.allow("10.0.0.1"); !ok {
		t.Fatal("ip A request 1 denied")
	}
	if ok, _ := l.allow("10.0.0.1"); !ok {
		t.Fatal("ip A request 2 denied")
	}
	if ok, _ := l.allow("10.0.0.1"); ok {
		t.Fatal("ip A request 3 allowed, want limit")
	}
	// A second IP has its own full bucket.
	if ok, _ := l.allow("10.0.0.2"); !ok {
		t.Fatal("ip B request denied, want allow (per-IP isolation)")
	}
	if ok, _ := l.allow("10.0.0.2"); !ok {
		t.Fatal("ip B request 2 denied")
	}
	if ok, _ := l.allow("10.0.0.2"); ok {
		t.Fatal("ip B request 3 allowed, want limit")
	}
}

func TestLimitIgnoresForwardedFor(t *testing.T) {
	app := newLimitApp(60, 1)
	resp := get(t, app, map[string]string{"X-Forwarded-For": "9.9.9.9"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("first = %d, want 200", resp.StatusCode)
	}
	// A spoofed X-Forwarded-For must not grant a fresh bucket: the key is
	// the direct peer (RemoteIP), identical for both test requests.
	resp = get(t, app, map[string]string{"X-Forwarded-For": "1.2.3.4"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("spoofed XFF = %d, want 429 (header must be ignored)", resp.StatusCode)
	}
}

func TestLimitSanitizesInput(t *testing.T) {
	for _, tc := range [][2]int{{0, 0}, {-5, -2}, {60, 0}} {
		app := newLimitApp(tc[0], tc[1])
		resp := get(t, app, nil)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Limit(%d,%d): first request = %d, want 200", tc[0], tc[1], resp.StatusCode)
		}
	}
}

func TestCleanupDropsIdle(t *testing.T) {
	now := time.Now()
	l := newLimiter(60, 2)
	l.now = func() time.Time { return now }
	if ok, _ := l.allow("10.0.0.1"); !ok {
		t.Fatal("first request denied")
	}
	now = now.Add(l.ttl + time.Second)
	l.cleanup()
	if len(l.buckets) != 0 {
		t.Fatalf("idle bucket not swept: %d buckets remain", len(l.buckets))
	}
}
