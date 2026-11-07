// Package presentation adapts auth use cases to HTTP.
package presentation

import (
	"crypto/subtle"
	"errors"
	"io"
	"time"

	"github.com/gofiber/fiber/v3"

	authapp "boilerplate/api/internal/auth/application"
	"boilerplate/api/internal/auth/domain"
	"boilerplate/api/internal/platform/session"
	"boilerplate/api/internal/shared/httpx"
	"boilerplate/api/internal/shared/id"
)

// Handler serves /auth endpoints.
type Handler struct {
	svc           *authapp.Service
	sessions      *session.Manager
	oidc          authapp.OIDCProvider // nil when OIDC is disabled
	oidcLabel     string
	ldap          authapp.LDAPAuthenticator // nil when LDAP is disabled
	ldapLabel     string
	avatars       authapp.AvatarStorage
	secureCookies bool
}

// NewHandler builds a Handler. Nil oidc/ldap providers disable those routes.
func NewHandler(svc *authapp.Service, sessions *session.Manager, oidc authapp.OIDCProvider, oidcLabel string, ldap authapp.LDAPAuthenticator, ldapLabel string, avatars authapp.AvatarStorage, secureCookies bool) *Handler {
	return &Handler{svc: svc, sessions: sessions, oidc: oidc, oidcLabel: oidcLabel, ldap: ldap, ldapLabel: ldapLabel, avatars: avatars, secureCookies: secureCookies}
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// Login verifies credentials and sets the HttpOnly session cookie.
func (h *Handler) Login(c fiber.Ctx) error {
	var req loginRequest
	if err := c.Bind().JSON(&req); err != nil {
		return httpx.Error(c, fiber.StatusBadRequest, "invalid JSON body")
	}
	user, err := h.svc.Login(c.Context(), authapp.LoginInput{
		Username: req.Username,
		Password: req.Password,
	})
	if err != nil {
		if handled, herr := httpx.FieldError(c, err); handled {
			return herr
		}
		if errors.Is(err, authapp.ErrInvalidCredentials) {
			return httpx.Error(c, fiber.StatusUnauthorized, "invalid username or password")
		}
		return err
	}
	h.sessions.SetCookie(c, h.sessions.Issue(user.ID))
	return c.JSON(user)
}

// Me returns the authenticated user profile.
func (h *Handler) Me(c fiber.Ctx) error {
	user, err := h.svc.Me(c.Context(), session.UserID(c))
	if err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			return httpx.Error(c, fiber.StatusUnauthorized, "unauthorized")
		}
		return err
	}
	return c.JSON(user)
}

// Logout clears the session cookie.
func (h *Handler) Logout(c fiber.Ctx) error {
	h.sessions.ClearCookie(c)
	return c.JSON(fiber.Map{"ok": true})
}

// CSRF exists so SPAs can obtain a CSRF cookie with a safe GET.
// The csrf middleware sets the cookie; the body is just an ack.
func (h *Handler) CSRF(c fiber.Ctx) error {
	return c.JSON(fiber.Map{"ok": true})
}

// Methods reports which login methods are enabled so the SPA can adapt.
func (h *Handler) Methods(c fiber.Ctx) error {
	return c.JSON(fiber.Map{
		"password": true,
		"oidc":     fiber.Map{"enabled": h.oidc != nil, "label": h.oidcLabel},
		"ldap":     fiber.Map{"enabled": h.ldap != nil, "label": h.ldapLabel},
	})
}

// LDAPLogin verifies directory credentials and sets the HttpOnly session cookie.
func (h *Handler) LDAPLogin(c fiber.Ctx) error {
	if h.ldap == nil {
		return httpx.Error(c, fiber.StatusNotFound, "ldap disabled")
	}
	var req loginRequest
	if err := c.Bind().JSON(&req); err != nil {
		return httpx.Error(c, fiber.StatusBadRequest, "invalid JSON body")
	}
	user, err := h.svc.LDAPLogin(c.Context(), h.ldap, req.Username, req.Password)
	if err != nil {
		if handled, herr := httpx.FieldError(c, err); handled {
			return herr
		}
		switch {
		case errors.Is(err, authapp.ErrInvalidCredentials):
			return httpx.Error(c, fiber.StatusUnauthorized, "invalid username or password")
		case errors.Is(err, authapp.ErrLoginConflict):
			return httpx.Error(c, fiber.StatusConflict, "this username uses password login")
		}
		return err
	}
	h.sessions.SetCookie(c, h.sessions.Issue(user.ID))
	return c.JSON(user)
}

// OIDCSignIn starts the authorization-code flow: short-lived state/nonce
// cookies, then a 302 to the provider.
func (h *Handler) OIDCSignIn(c fiber.Ctx) error {
	if h.oidc == nil {
		return httpx.Error(c, fiber.StatusNotFound, "oidc disabled")
	}
	state, nonce := id.New(), id.New()
	for _, ck := range []struct{ name, value string }{{"oidc_state", state}, {"oidc_nonce", nonce}} {
		c.Cookie(&fiber.Cookie{
			Name: ck.name, Value: ck.value, Path: "/",
			HTTPOnly: true, Secure: h.secureCookies, SameSite: "Lax",
			MaxAge: 300, Expires: time.Now().Add(5 * time.Minute),
		})
	}
	c.Set("Location", h.oidc.AuthURL(state, nonce))
	return c.SendStatus(fiber.StatusFound)
}

// OIDCCallback completes the flow: verifies state, exchanges the code,
// finds-or-creates the user, sets the session cookie and lands on the SPA.
// Failures redirect to login with ?error=oidc_failed (fixed targets only).
func (h *Handler) OIDCCallback(c fiber.Ctx) error {
	if h.oidc == nil {
		return httpx.Error(c, fiber.StatusNotFound, "oidc disabled")
	}
	fail := func() error {
		c.Set("Location", "/login?error=oidc_failed")
		return c.SendStatus(fiber.StatusFound)
	}
	state, want := c.Query("state"), c.Cookies("oidc_state")
	nonce := c.Cookies("oidc_nonce")
	code := c.Query("code")
	h.clearOIDCCookies(c)
	if state == "" || want == "" || subtle.ConstantTimeCompare([]byte(state), []byte(want)) != 1 {
		return fail()
	}
	if nonce == "" || code == "" {
		return fail()
	}
	claims, err := h.oidc.Exchange(c.Context(), code, nonce)
	if err != nil {
		return fail()
	}
	user, err := h.svc.OIDCLogin(c.Context(), h.oidc.Issuer(), claims)
	if err != nil {
		return fail()
	}
	h.sessions.SetCookie(c, h.sessions.Issue(user.ID))
	c.Set("Location", "/")
	return c.SendStatus(fiber.StatusFound)
}

// UploadAvatar stores multipart field "file" as the user's avatar.
// The SPA converts to webp client-side; the server re-validates magic + size.
func (h *Handler) UploadAvatar(c fiber.Ctx) error {
	form, err := c.MultipartForm()
	if err != nil {
		return httpx.Error(c, fiber.StatusBadRequest, "invalid multipart body")
	}
	files := form.File["file"]
	if len(files) == 0 {
		return httpx.Error(c, fiber.StatusBadRequest, "file required")
	}
	src, err := files[0].Open()
	if err != nil {
		return httpx.Error(c, fiber.StatusBadRequest, "cannot read file")
	}
	defer src.Close()
	data, err := io.ReadAll(io.LimitReader(src, authapp.MaxAvatarBytes+1))
	if err != nil {
		return err
	}
	dto, err := h.svc.SetAvatar(c.Context(), h.avatars, session.UserID(c), data)
	if err != nil {
		if handled, herr := httpx.FieldError(c, err); handled {
			return herr
		}
		if errors.Is(err, domain.ErrUserNotFound) {
			return httpx.Error(c, fiber.StatusUnauthorized, "unauthorized")
		}
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(dto)
}

func (h *Handler) clearOIDCCookies(c fiber.Ctx) {
	for _, name := range []string{"oidc_state", "oidc_nonce"} {
		c.Cookie(&fiber.Cookie{
			Name: name, Value: "", Path: "/",
			HTTPOnly: true, Secure: h.secureCookies, SameSite: "Lax", MaxAge: -1,
		})
	}
}
