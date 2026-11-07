package httpserver

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/gofiber/fiber/v3"

	"boilerplate/api/internal/platform/db"
	"boilerplate/api/internal/shared/config"
)

// fakeOIDC is a minimal OIDC provider for tests: discovery, JWKS and token
// endpoints with real RS256-signed ID tokens.
type fakeOIDC struct {
	t        *testing.T
	server   *httptest.Server
	key      *rsa.PrivateKey
	clientID string
	nonce    string
	email    string
	picture  string
}

type testClaims struct {
	jwt.Claims
	Email   string `json:"email"`
	Name    string `json:"name"`
	Nonce   string `json:"nonce"`
	Picture string `json:"picture"`
}

func newFakeOIDC(t *testing.T, clientID string) *fakeOIDC {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeOIDC{t: t, key: key, clientID: clientID, nonce: "test-nonce", email: "oidc.user@example.com", picture: "https://example.com/pic1.jpg"}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"issuer":                 f.server.URL,
			"authorization_endpoint": f.server.URL + "/authorize",
			"token_endpoint":         f.server.URL + "/token",
			"jwks_uri":               f.server.URL + "/jwks",
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		pub := &f.key.PublicKey
		n := base64.RawURLEncoding.EncodeToString(pub.N.Bytes())
		e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes())
		_ = json.NewEncoder(w).Encode(map[string]any{
			"keys": []map[string]string{{
				"kty": "RSA", "kid": "test-key", "use": "sig", "alg": "RS256", "n": n, "e": e,
			}},
		})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		now := time.Now()
		signer, err := jose.NewSigner(
			jose.SigningKey{Algorithm: jose.RS256, Key: f.key},
			(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "test-key"),
		)
		if err != nil {
			t.Errorf("signer: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		raw, err := jwt.Signed(signer).Claims(&testClaims{
			Claims: jwt.Claims{
				Issuer:   f.server.URL,
				Subject:  "oidc-sub-1",
				Audience: jwt.Audience{f.clientID},
				Expiry:   jwt.NewNumericDate(now.Add(time.Hour)),
				IssuedAt: jwt.NewNumericDate(now),
			},
			Email:   f.email,
			Name:    "OIDC User",
			Nonce:   f.nonce,
			Picture: f.picture,
		}).Serialize()
		if err != nil {
			t.Errorf("serialize: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "fake-at", "token_type": "Bearer", "expires_in": 3600, "id_token": raw,
		})
	})
	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)
	return f
}

func newOIDCTestApp(t *testing.T, issuer, clientID string) *fiber.App {
	t.Helper()
	cfg := config.Config{
		Env:           "test",
		Port:          "0",
		DBDriver:      "sqlite",
		SessionSecret: "test-secret-0123456789abcdef",
		SessionTTL:    time.Hour,
		UploadDir:     filepath.Join(t.TempDir(), "uploads"),
		OIDC: config.OIDCConfig{
			Enabled:      true,
			Issuer:       issuer,
			ClientID:     clientID,
			ClientSecret: "test-secret",
			RedirectURL:  "http://example.com/api/auth/oidc/callback",
			Label:        "TestID",
		},
	}
	pool, err := db.Open("sqlite", filepath.Join(t.TempDir(), "app.db"), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Close() })
	if err := db.Migrate(t.Context(), pool, "sqlite"); err != nil {
		t.Fatal(err)
	}
	app, err := NewWithFS(cfg, pool, fstest.MapFS{})
	if err != nil {
		t.Fatal(err)
	}
	return app
}

func TestOIDCSignInRedirect(t *testing.T) {
	fake := newFakeOIDC(t, "test-client")
	app := newOIDCTestApp(t, fake.server.URL, "test-client")

	resp := do(t, app, jsonReq(t, http.MethodGet, "/api/auth/oidc/login", ""))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("expected 302, got %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if !strings.HasPrefix(loc, fake.server.URL+"/authorize") ||
		!strings.Contains(loc, "state=") || !strings.Contains(loc, "nonce=") {
		t.Fatalf("bad authorize redirect: %q", loc)
	}
	var state, nonce *http.Cookie
	for _, c := range resp.Cookies() {
		switch c.Name {
		case "oidc_state":
			state = c
		case "oidc_nonce":
			nonce = c
		}
	}
	if state == nil || nonce == nil || state.Value == "" || nonce.Value == "" {
		t.Fatal("expected oidc_state + oidc_nonce cookies")
	}
	if !state.HttpOnly || !nonce.HttpOnly {
		t.Fatal("oidc cookies must be HttpOnly")
	}
}

// callbackReq builds a callback request with caller-controlled cookies,
// mirroring what the login step would have set in a browser.
func callbackReq(state, nonce string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/api/auth/oidc/callback?code=any-code&state="+state, nil)
	req.AddCookie(&http.Cookie{Name: "oidc_state", Value: state})
	req.AddCookie(&http.Cookie{Name: "oidc_nonce", Value: nonce})
	return req
}

func TestOIDCCallbackFullFlow(t *testing.T) {
	fake := newFakeOIDC(t, "test-client")
	app := newOIDCTestApp(t, fake.server.URL, "test-client")
	j := newJar()

	resp := do(t, app, callbackReq("s1", fake.nonce))
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/" {
		t.Fatalf("expected 302 to /, got %d -> %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	var sessionCookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == "session" {
			sessionCookie = c
		}
	}
	if sessionCookie == nil || !sessionCookie.HttpOnly || sessionCookie.Value == "" {
		t.Fatal("expected HttpOnly session cookie from OIDC callback")
	}
	j.store(resp)

	req := jsonReq(t, http.MethodGet, "/api/auth/me", "")
	j.apply(req)
	resp = do(t, app, req)
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK || body["username"] != "oidc.user" {
		t.Fatalf("me: expected oidc.user, got %d %v", resp.StatusCode, body)
	}
	if body["avatarUrl"] != fake.picture {
		t.Fatalf("me.avatarUrl = %v, want %q", body["avatarUrl"], fake.picture)
	}
	firstID, _ := body["id"].(string)

	// Second login with the same subject reuses the linked user.
	resp = do(t, app, callbackReq("s2", fake.nonce))
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("second login: expected 302, got %d", resp.StatusCode)
	}
	j2 := newJar()
	j2.store(resp)
	req = jsonReq(t, http.MethodGet, "/api/auth/me", "")
	j2.apply(req)
	resp = do(t, app, req)
	body = readBody(t, resp)
	if body["id"] != firstID {
		t.Fatalf("expected same user %q, got %v", firstID, body)
	}
}

func TestOIDCAvatarPolicy(t *testing.T) {
	fake := newFakeOIDC(t, "test-client")
	app := newOIDCTestApp(t, fake.server.URL, "test-client")
	j := newJar()
	fetchCSRF(t, app, j)

	login := func(state string) {
		t.Helper()
		req := callbackReq(state, fake.nonce)
		j.apply(req)
		resp := do(t, app, req)
		resp.Body.Close()
		if resp.StatusCode != http.StatusFound {
			t.Fatalf("callback: expected 302, got %d", resp.StatusCode)
		}
		j.store(resp)
	}
	avatarOf := func() string {
		t.Helper()
		req := jsonReq(t, http.MethodGet, "/api/auth/me", "")
		j.apply(req)
		resp := do(t, app, req)
		body := readBody(t, resp)
		avatar, _ := body["avatarUrl"].(string)
		return avatar
	}

	login("s1")
	if got := avatarOf(); got != "https://example.com/pic1.jpg" {
		t.Fatalf("initial avatar = %q", got)
	}

	// Remote picture changes refresh the avatar.
	fake.picture = "https://example.com/pic2.jpg"
	login("s2")
	if got := avatarOf(); got != "https://example.com/pic2.jpg" {
		t.Fatalf("refreshed avatar = %q", got)
	}

	// A locally uploaded avatar wins over later provider pictures.
	resp := do(t, app, postAvatar(t, j, webpBytes(150)))
	uploaded := readBody(t, resp)
	localURL, _ := uploaded["url"].(string)
	if resp.StatusCode != http.StatusCreated || localURL == "" {
		t.Fatalf("upload: expected 201 url, got %d %v", resp.StatusCode, uploaded)
	}
	fake.picture = "https://example.com/pic3.jpg"
	login("s3")
	if got := avatarOf(); got != localURL {
		t.Fatalf("local avatar must win, got %q want %q", got, localURL)
	}
}

func TestOIDCCallbackRejects(t *testing.T) {
	fake := newFakeOIDC(t, "test-client")
	app := newOIDCTestApp(t, fake.server.URL, "test-client")

	cases := map[string]*http.Request{
		"state mismatch": callbackReq("wrong", fake.nonce),
		"nonce mismatch": callbackReq("s1", "wrong-nonce"),
	}
	// state mismatch needs a cookie holding a *different* state than the query.
	mismatch := httptest.NewRequest(http.MethodGet, "/api/auth/oidc/callback?code=x&state=query-state", nil)
	mismatch.AddCookie(&http.Cookie{Name: "oidc_state", Value: "cookie-state"})
	mismatch.AddCookie(&http.Cookie{Name: "oidc_nonce", Value: fake.nonce})
	cases["state mismatch"] = mismatch

	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			resp := do(t, app, req)
			resp.Body.Close()
			if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/login?error=oidc_failed" {
				t.Fatalf("expected 302 to login error, got %d -> %q", resp.StatusCode, resp.Header.Get("Location"))
			}
			for _, c := range resp.Cookies() {
				if c.Name == "session" && c.Value != "" {
					t.Fatal("must not set a session on failed callback")
				}
			}
		})
	}
}
