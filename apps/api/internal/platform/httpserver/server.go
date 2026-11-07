// Package httpserver wires the Fiber app: security middleware, API routes,
// and the embedded SPA.
package httpserver

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/csrf"
	"github.com/gofiber/fiber/v3/middleware/helmet"
	"github.com/gofiber/fiber/v3/middleware/logger"
	"github.com/gofiber/fiber/v3/middleware/recover"
	"github.com/gofiber/fiber/v3/middleware/static"

	authapp "boilerplate/api/internal/auth/application"
	authinfra "boilerplate/api/internal/auth/infrastructure"
	authpres "boilerplate/api/internal/auth/presentation"
	orderapp "boilerplate/api/internal/order/application"
	orderinfra "boilerplate/api/internal/order/infrastructure"
	orderpres "boilerplate/api/internal/order/presentation"
	"boilerplate/api/internal/platform/db/sqlcgen"
	"boilerplate/api/internal/platform/session"
	"boilerplate/api/internal/shared/config"
	"boilerplate/api/web"
)

// contentSecurityPolicy locks the SPA to same-origin resources.
// style-src needs 'unsafe-inline' for React inline styles; img-src allows
// https: for remote provider avatars (e.g. Google pictures).
const contentSecurityPolicy = "default-src 'self'; " +
	"base-uri 'self'; " +
	"frame-ancestors 'none'; " +
	"form-action 'self'; " +
	"object-src 'none'; " +
	"script-src 'self'; " +
	"style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data: https:; " +
	"font-src 'self'; " +
	"connect-src 'self'"

// New builds the Fiber app with all middleware and routes,
// serving the embedded frontend bundle.
func New(cfg config.Config, pool *sql.DB) (*fiber.App, error) {
	dist, err := fs.Sub(web.Dist, "dist")
	if err != nil {
		return nil, err
	}
	return NewWithFS(cfg, pool, dist)
}

// NewWithFS builds the app serving static files from dist.
// Tests use it to control the bundle without touching the embed.
func NewWithFS(cfg config.Config, pool *sql.DB, dist fs.FS) (*fiber.App, error) {
	queries := sqlcgen.New(pool)
	sessions := session.New(cfg.SessionSecret, cfg.SessionTTL, cfg.IsProd())

	var oidcProvider authapp.OIDCProvider
	if cfg.OIDC.Enabled {
		p, err := authinfra.NewOIDCProvider(context.Background(),
			cfg.OIDC.Issuer, cfg.OIDC.ClientID, cfg.OIDC.ClientSecret, cfg.OIDC.RedirectURL)
		if err != nil {
			return nil, fmt.Errorf("oidc discovery: %w", err)
		}
		oidcProvider = p
	}

	var ldapAuth authapp.LDAPAuthenticator
	if cfg.LDAP.Enabled {
		ldapAuth = authinfra.NewLDAPAuthenticator(cfg.LDAP)
	}

	var avatarStorage authapp.AvatarStorage
	switch cfg.StorageDriver {
	case "s3":
		s3store, err := authinfra.NewS3AvatarStorage(context.Background(), cfg.S3)
		if err != nil {
			return nil, fmt.Errorf("s3 storage: %w", err)
		}
		avatarStorage = s3store
	default: // local
		disk, err := authinfra.NewDiskAvatarStorage(cfg.UploadDir)
		if err != nil {
			return nil, err
		}
		avatarStorage = disk
	}

	authSvc := authapp.NewService(authinfra.NewSQLUserRepository(queries), authinfra.NewBcryptHasher())
	orderSvc := orderapp.NewService(orderinfra.NewSQLOrderRepository(queries))
	authHandler := authpres.NewHandler(authSvc, sessions, oidcProvider, cfg.OIDC.Label, ldapAuth, cfg.LDAP.Label, avatarStorage, cfg.IsProd())
	orderHandler := orderpres.NewHandler(orderSvc)

	app := fiber.New(fiber.Config{
		AppName:      "boilerplate",
		ErrorHandler: jsonErrorHandler,
	})

	app.Use(recover.New())
	app.Use(logger.New())
	app.Use(helmet.New(helmet.Config{
		ContentSecurityPolicy: contentSecurityPolicy,
		XFrameOptions:         "DENY",
		HSTSMaxAge:            hstsMaxAge(cfg),
	}))

	api := app.Group("/api")
	api.Use(csrf.New(csrf.Config{
		CookieName:     "csrf_token",
		CookiePath:     "/",
		CookieSameSite: "Lax",
		CookieSecure:   cfg.IsProd(),
		CookieHTTPOnly: false, // SPA must read it to send X-CSRF-Token
		ErrorHandler: func(c fiber.Ctx, _ error) error {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "csrf_invalid"})
		},
	}))

	auth := api.Group("/auth")
	auth.Post("/login", authHandler.Login)
	auth.Post("/ldap/login", authHandler.LDAPLogin)
	auth.Get("/csrf", authHandler.CSRF)
	auth.Get("/methods", authHandler.Methods)
	auth.Get("/oidc/login", authHandler.OIDCSignIn)
	auth.Get("/oidc/callback", authHandler.OIDCCallback)
	auth.Get("/me", sessions.AuthRequired, authHandler.Me)
	auth.Post("/logout", sessions.AuthRequired, authHandler.Logout)

	orders := api.Group("/orders", sessions.AuthRequired)
	orders.Get("/", orderHandler.List)
	orders.Post("/", orderHandler.Create)
	orders.Patch("/:id/status", orderHandler.SetStatus)

	uploads := api.Group("/uploads", sessions.AuthRequired)
	uploads.Post("/avatar", authHandler.UploadAvatar)

	app.Get("/uploads/*", serveUploads(avatarStorage))

	app.Use(static.New("/", static.Config{
		FS:     dist,
		MaxAge: 0,
		ModifyResponse: func(c fiber.Ctx) error {
			if strings.HasPrefix(c.Path(), "/assets/") {
				c.Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				c.Set("Cache-Control", "no-store")
			}
			setStaticSecurityHeaders(c)
			return nil
		},
		NotFoundHandler: spaFallback(dist),
	}))

	return app, nil
}

func hstsMaxAge(cfg config.Config) int {
	if cfg.IsProd() {
		return 31536000
	}
	return 0
}

// spaFallback serves index.html for client-side routes. When the frontend
// was never built (`make build-web`), it explains that instead of 404ing.
func spaFallback(dist fs.FS) fiber.Handler {
	index, err := fs.ReadFile(dist, "index.html")
	notBuilt := err != nil
	return func(c fiber.Ctx) error {
		setStaticSecurityHeaders(c)
		if notBuilt {
			return c.Status(fiber.StatusServiceUnavailable).SendString(
				"frontend not built: run `make build-web` then rebuild the API")
		}
		c.Set("Content-Type", "text/html; charset=utf-8")
		c.Set("Cache-Control", "no-store")
		// The file handler leaves a 404 status behind; the fallback is a hit.
		return c.Status(fiber.StatusOK).Send(index)
	}
}

// setStaticSecurityHeaders re-applies critical headers on static responses:
// the file handler resets headers set by earlier middleware.
func setStaticSecurityHeaders(c fiber.Ctx) {
	c.Set(fiber.HeaderContentSecurityPolicy, contentSecurityPolicy)
	c.Set(fiber.HeaderXContentTypeOptions, "nosniff")
	c.Set(fiber.HeaderXFrameOptions, "DENY")
}

func jsonErrorHandler(c fiber.Ctx, err error) error {
	code := fiber.StatusInternalServerError
	var ferr *fiber.Error
	if errors.As(err, &ferr) {
		code = ferr.Code
	}
	if code == fiber.StatusNotFound {
		return c.Status(code).JSON(fiber.Map{"error": "not found"})
	}
	return c.Status(code).JSON(fiber.Map{"error": "internal error"})
}
