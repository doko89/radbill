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
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/csrf"
	"github.com/gofiber/fiber/v3/middleware/helmet"
	"github.com/gofiber/fiber/v3/middleware/logger"
	"github.com/gofiber/fiber/v3/middleware/recover"
	"github.com/gofiber/fiber/v3/middleware/static"

	authapp "boilerplate/api/internal/auth/application"
	authinfra "boilerplate/api/internal/auth/infrastructure"
	authpres "boilerplate/api/internal/auth/presentation"
	billapp "boilerplate/api/internal/billing/application"
	billinfra "boilerplate/api/internal/billing/infrastructure"
	billpres "boilerplate/api/internal/billing/presentation"
	custapp "boilerplate/api/internal/customers/application"
	custinfra "boilerplate/api/internal/customers/infrastructure"
	custpres "boilerplate/api/internal/customers/presentation"
	jwtapp "boilerplate/api/internal/jwtauth/application"
	jwtauthdomain "boilerplate/api/internal/jwtauth/domain"
	jwtinfra "boilerplate/api/internal/jwtauth/infrastructure"
	jwtpres "boilerplate/api/internal/jwtauth/presentation"
	nasapp "boilerplate/api/internal/nas/application"
	nasinfra "boilerplate/api/internal/nas/infrastructure"
	naspres "boilerplate/api/internal/nas/presentation"
	"boilerplate/api/internal/network/coa"
	"boilerplate/api/internal/network/notify"
	orderapp "boilerplate/api/internal/order/application"
	orderinfra "boilerplate/api/internal/order/infrastructure"
	orderpres "boilerplate/api/internal/order/presentation"
	pkgapp "boilerplate/api/internal/packages/application"
	pkginfra "boilerplate/api/internal/packages/infrastructure"
	pkgpres "boilerplate/api/internal/packages/presentation"
	"boilerplate/api/internal/platform/db/sqlcgen"
	"boilerplate/api/internal/platform/session"
	pppoeapp "boilerplate/api/internal/pppoe/application"
	pppoeinfra "boilerplate/api/internal/pppoe/infrastructure"
	pppoepres "boilerplate/api/internal/pppoe/presentation"
	sessapp "boilerplate/api/internal/sessions/application"
	sessinfra "boilerplate/api/internal/sessions/infrastructure"
	sesspres "boilerplate/api/internal/sessions/presentation"
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

	// Billing Fase 1 (radbill, ADITIF): liveness probe + JWT/billing routes.
	// API routes stay under /api so CSRF + helmet + error handling apply.
	app.Get("/healthz", healthz(pool))
	registerBillingRoutes(api, cfg, pool)

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

// healthz reports liveness plus DB reachability.
func healthz(pool *sql.DB) fiber.Handler {
	return func(c fiber.Ctx) error {
		ctx, cancel := context.WithTimeout(c.Context(), 2*time.Second)
		defer cancel()
		if err := pool.PingContext(ctx); err != nil {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"status": "degraded"})
		}
		return c.JSON(fiber.Map{"status": "ok"})
	}
}

// registerBillingRoutes wires billing Fase 1 contexts (ADITIF).
func registerBillingRoutes(api fiber.Router, cfg config.Config, pool *sql.DB) {
	queries := sqlcgen.New(pool)
	events := notify.New(pool, cfg.DBDriver)
	coaSender := coa.NewSender(coa.DefaultTimeout)
	hasher := jwtinfra.NewArgon2Hasher()

	authSvc := jwtapp.NewService(
		jwtinfra.NewSQLAccountRepository(queries),
		jwtinfra.NewSQLRefreshTokenRepository(queries),
		jwtinfra.NewSQLPasswordResetRepository(queries),
		hasher, cfg.JWTSecret, cfg.JWTAccessTTL(), cfg.RefreshTTL(),
	)
	authHandler := jwtpres.NewHandler(authSvc, cfg.RefreshTTL(), cfg.IsProd(), !cfg.IsProd())

	v1 := api.Group("/v1")
	auth := v1.Group("/auth")
	auth.Post("/login", authHandler.Login)
	auth.Post("/refresh", authHandler.Refresh)
	auth.Post("/logout", authHandler.Logout)
	auth.Get("/me", jwtpres.Authenticate(authSvc), authHandler.Me)
	auth.Post("/password/reset-request", authHandler.RequestPasswordReset)
	auth.Post("/password/reset", authHandler.ResetPassword)

	admin := v1.Group("/admin")

	custHandler := custpres.NewHandler(custapp.NewService(custinfra.NewSQLRepository(queries)))
	customers := admin.Group("/customers")
	customers.Get("/", jwtpres.Require(authSvc, jwtauthdomain.PermCustomerRead), custHandler.List)
	customers.Get("/:id", jwtpres.Require(authSvc, jwtauthdomain.PermCustomerRead), custHandler.Get)
	customers.Post("/", jwtpres.Require(authSvc, jwtauthdomain.PermCustomerWrite), custHandler.Create)
	customers.Put("/:id", jwtpres.Require(authSvc, jwtauthdomain.PermCustomerWrite), custHandler.Update)
	customers.Patch("/:id", jwtpres.Require(authSvc, jwtauthdomain.PermCustomerWrite), custHandler.Update)
	customers.Delete("/:id", jwtpres.Require(authSvc, jwtauthdomain.PermCustomerWrite), custHandler.Delete)

	pkgHandler := pkgpres.NewHandler(pkgapp.NewService(pkginfra.NewSQLRepository(queries, events)))
	packages := admin.Group("/packages")
	packages.Get("/", jwtpres.Require(authSvc, jwtauthdomain.PermPackageRead), pkgHandler.List)
	packages.Get("/:id", jwtpres.Require(authSvc, jwtauthdomain.PermPackageRead), pkgHandler.Get)
	packages.Post("/", jwtpres.Require(authSvc, jwtauthdomain.PermPackageWrite), pkgHandler.Create)
	packages.Put("/:id", jwtpres.Require(authSvc, jwtauthdomain.PermPackageWrite), pkgHandler.Update)
	packages.Patch("/:id", jwtpres.Require(authSvc, jwtauthdomain.PermPackageWrite), pkgHandler.Update)
	packages.Delete("/:id", jwtpres.Require(authSvc, jwtauthdomain.PermPackageWrite), pkgHandler.Delete)

	pppoeRepo := pppoeinfra.NewSQLRepository(queries, events)
	pppoeHandler := pppoepres.NewHandler(pppoeapp.NewService(
		pppoeRepo,
		pppoeinfra.NewSQLPortalStore(pool, queries),
		pppoeinfra.NewCoADisconnecter(queries, coaSender),
		hasher,
	))
	pppoe := admin.Group("/pppoe-accounts")
	pppoeRead := jwtpres.Require(authSvc, jwtauthdomain.PermPPPoERead)
	pppoeWrite := jwtpres.Require(authSvc, jwtauthdomain.PermPPPoEWrite)
	pppoe.Get("/", pppoeRead, pppoeHandler.List)
	pppoe.Get("/:id", pppoeRead, pppoeHandler.Get)
	pppoe.Post("/", pppoeWrite, pppoeHandler.Create)
	pppoe.Put("/:id", pppoeWrite, pppoeHandler.Update)
	pppoe.Patch("/:id", pppoeWrite, pppoeHandler.Update)
	pppoe.Delete("/:id", pppoeWrite, pppoeHandler.Delete)
	pppoe.Post("/:id/isolate", pppoeWrite, pppoeHandler.Isolate)
	pppoe.Post("/:id/activate", pppoeWrite, pppoeHandler.Activate)
	pppoe.Post("/:id/disable", pppoeWrite, pppoeHandler.Disable)
	pppoe.Post("/:id/extend", pppoeWrite, pppoeHandler.Extend)
	pppoe.Post("/:id/change-package", pppoeWrite, pppoeHandler.ChangePackage)
	pppoe.Post("/:id/reset-quota", pppoeWrite, pppoeHandler.ResetQuota)
	pppoe.Post("/:id/disconnect", pppoeWrite, pppoeHandler.Disconnect)
	pppoe.Post("/:id/portal-account", pppoeWrite, pppoeHandler.CreatePortalAccount)
	pppoe.Post("/:id/portal-account/reset", pppoeWrite, pppoeHandler.ResetPortalAccount)

	nasHandler := naspres.NewHandler(nasapp.NewService(nasinfra.NewSQLRepository(queries, events), coaSender))
	nas := admin.Group("/nas")
	nasRead := jwtpres.Require(authSvc, jwtauthdomain.PermNASRead)
	nasWrite := jwtpres.Require(authSvc, jwtauthdomain.PermNASWrite)
	nas.Get("/", nasRead, nasHandler.List)
	nas.Get("/:id", nasRead, nasHandler.Get)
	nas.Post("/", nasWrite, nasHandler.Create)
	nas.Put("/:id", nasWrite, nasHandler.Update)
	nas.Patch("/:id", nasWrite, nasHandler.Update)
	nas.Delete("/:id", nasWrite, nasHandler.Delete)
	nas.Post("/:id/test-coa", nasWrite, nasHandler.TestCoA)

	sessHandler := sesspres.NewHandler(sessapp.NewService(sessinfra.NewSQLRepository(queries)))
	sessions := admin.Group("/sessions", jwtpres.Require(authSvc, jwtauthdomain.PermSessionRead))
	sessions.Get("/", sessHandler.List)
	sessions.Get("/history", sessHandler.History)

	billRepo := billinfra.NewSQLRepository(pool, queries, events)
	billHandler := billpres.NewHandler(billapp.NewService(billRepo, billRepo, billRepo))
	invoices := admin.Group("/invoices")
	invoices.Get("/", jwtpres.Require(authSvc, jwtauthdomain.PermInvoiceRead), billHandler.List)
	invoices.Get("/:id", jwtpres.Require(authSvc, jwtauthdomain.PermInvoiceRead), billHandler.Get)
	invoices.Post("/", jwtpres.Require(authSvc, jwtauthdomain.PermInvoiceWrite), billHandler.Create)
	invoices.Post("/:id/void", jwtpres.Require(authSvc, jwtauthdomain.PermInvoiceWrite), billHandler.Void)
	invoices.Post("/:id/manual-payment", jwtpres.Require(authSvc, jwtauthdomain.PermPaymentWrite), billHandler.ManualPayment)
}
