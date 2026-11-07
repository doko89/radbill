package httpserver

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// webpBytes builds a minimal RIFF/WEBP payload of at least size bytes.
// It satisfies magic-byte validation; it is not a viewable image.
func webpBytes(size int) []byte {
	buf := []byte{'R', 'I', 'F', 'F', 0, 0, 0, 0, 'W', 'E', 'B', 'P'}
	for len(buf) < size {
		buf = append(buf, 0)
	}
	return buf
}

// postAvatar uploads data as multipart field "file" with session + CSRF.
func postAvatar(t *testing.T, j *jar, data []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("file", "avatar.webp")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/uploads/avatar", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	j.apply(req)
	req.Header.Set("X-CSRF-Token", j.csrfToken())
	return req
}

func TestUploadAvatarFlow(t *testing.T) {
	app := newTestApp(t)
	j := newJar()
	fetchCSRF(t, app, j)
	resp := do(t, app, unsafeReq(t, j, http.MethodPost, "/api/auth/login", `{"username":"admin","password":"admin123"}`))
	resp.Body.Close()
	j.store(resp)

	// Valid webp -> 201 with public URL.
	resp = do(t, app, postAvatar(t, j, webpBytes(200)))
	created := readBody(t, resp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("upload: expected 201, got %d %v", resp.StatusCode, created)
	}
	url, _ := created["url"].(string)
	if !strings.HasPrefix(url, "/uploads/avatar/") || !strings.HasSuffix(url, ".webp") {
		t.Fatalf("bad avatar url %q", url)
	}

	// Served with webp content type, immutable cache and CSP.
	req := httptest.NewRequest(http.MethodGet, url, nil)
	resp = do(t, app, req)
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "image/webp" {
		t.Fatalf("serve: expected 200 webp, got %d %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	if !bytes.Equal(raw, webpBytes(200)) {
		t.Fatal("serve: body differs from upload")
	}
	if cc := resp.Header.Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Fatalf("serve Cache-Control = %q", cc)
	}
	if csp := resp.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'self'") {
		t.Fatal("serve: missing CSP")
	}

	// me reflects the avatar.
	req = jsonReq(t, http.MethodGet, "/api/auth/me", "")
	j.apply(req)
	resp = do(t, app, req)
	me := readBody(t, resp)
	if me["avatarUrl"] != url {
		t.Fatalf("me.avatarUrl = %v, want %q", me["avatarUrl"], url)
	}

	// Re-upload replaces: new URL, old file gone.
	resp = do(t, app, postAvatar(t, j, webpBytes(300)))
	replaced := readBody(t, resp)
	newURL, _ := replaced["url"].(string)
	if resp.StatusCode != http.StatusCreated || newURL == "" || newURL == url {
		t.Fatalf("re-upload: expected new 201 url, got %d %v", resp.StatusCode, replaced)
	}
	resp = do(t, app, httptest.NewRequest(http.MethodGet, url, nil))
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("old avatar: expected 404, got %d", resp.StatusCode)
	}
}

func TestUploadAvatarRejects(t *testing.T) {
	app := newTestApp(t)
	j := newJar()
	fetchCSRF(t, app, j)
	resp := do(t, app, unsafeReq(t, j, http.MethodPost, "/api/auth/login", `{"username":"admin","password":"admin123"}`))
	resp.Body.Close()
	j.store(resp)

	// Non-webp bytes -> 400.
	resp = do(t, app, postAvatar(t, j, []byte("definitely not webp..............")))
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("non-webp: expected 400, got %d %v", resp.StatusCode, body)
	}

	// Oversize -> 400.
	resp = do(t, app, postAvatar(t, j, webpBytes(512*1024+1)))
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("oversize: expected 400, got %d", resp.StatusCode)
	}

	// No session -> 401.
	anon := newJar()
	fetchCSRF(t, app, anon)
	resp = do(t, app, postAvatar(t, anon, webpBytes(100)))
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous: expected 401, got %d", resp.StatusCode)
	}

	// Bad serve paths -> 404, never a file read.
	for _, path := range []string{
		"/uploads/avatar/not-a-uuid.png",
		"/uploads/avatar/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.txt",
		"/uploads/other/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.webp",
		"/uploads/avatar/%2e%2e/x.webp",
	} {
		resp = do(t, app, httptest.NewRequest(http.MethodGet, path, nil))
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("GET %s: expected 404, got %d", path, resp.StatusCode)
		}
	}

	// Well-formed but missing file -> 404.
	resp = do(t, app, httptest.NewRequest(http.MethodGet, "/uploads/avatar/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.webp", nil))
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("missing file: expected 404, got %d", resp.StatusCode)
	}
}
