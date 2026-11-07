// Package httpserver wires the Fiber app: security middleware, API routes,
// and the embedded SPA.
package httpserver

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/csrf"
	"github.com/gofiber/fiber/v3/middleware/helmet"
	"github.com/gofiber/fiber/v3/middleware/logger"
	"github.com/gofiber/fiber/v3/middleware/recover"
	"github.com/gofiber/fiber/v3/middleware/static"

	auditapp "boilerplate/api/internal/audit/application"
	auditinfra "boilerplate/api/internal/audit/infrastructure"
	auditpres "boilerplate/api/internal/audit/presentation"
	authapp "boilerplate/api/internal/auth/application"
	authinfra "boilerplate/api/internal/auth/infrastructure"
	authpres "boilerplate/api/internal/auth/presentation"
	billapp "boilerplate/api/internal/billing/application"
	billinfra "boilerplate/api/internal/billing/infrastructure"
	billpres "boilerplate/api/internal/billing/presentation"
	custapp "boilerplate/api/internal/customers/application"
	custinfra "boilerplate/api/internal/customers/infrastructure"
	custpres "boilerplate/api/internal/customers/presentation"
	dashapp "boilerplate/api/internal/dashboard/application"
	dashdomain "boilerplate/api/internal/dashboard/domain"
	dashinfra "boilerplate/api/internal/dashboard/infrastructure"
	dashpres "boilerplate/api/internal/dashboard/presentation"
	jwtapp "boilerplate/api/internal/jwtauth/application"
	jwtauthdomain "boilerplate/api/internal/jwtauth/domain"
	jwtinfra "boilerplate/api/internal/jwtauth/infrastructure"
	jwtpres "boilerplate/api/internal/jwtauth/presentation"
	kriskrispres "boilerplate/api/internal/kriskris/presentation"
	nasapp "boilerplate/api/internal/nas/application"
	nasinfra "boilerplate/api/internal/nas/infrastructure"
	naspres "boilerplate/api/internal/nas/presentation"
	"boilerplate/api/internal/network/coa"
	"boilerplate/api/internal/network/notify"
	notifyapp "boilerplate/api/internal/notify/application"
	notifydomain "boilerplate/api/internal/notify/domain"
	notifyinfra "boilerplate/api/internal/notify/infrastructure"
	orderapp "boilerplate/api/internal/order/application"
	orderinfra "boilerplate/api/internal/order/infrastructure"
	orderpres "boilerplate/api/internal/order/presentation"
	pkgapp "boilerplate/api/internal/packages/application"
	pkginfra "boilerplate/api/internal/packages/infrastructure"
	pkgpres "boilerplate/api/internal/packages/presentation"
	planchangesapp "boilerplate/api/internal/planchanges/application"
	planchangesinfra "boilerplate/api/internal/planchanges/infrastructure"
	planchangespres "boilerplate/api/internal/planchanges/presentation"
	"boilerplate/api/internal/platform/db/sqlcgen"
	"boilerplate/api/internal/platform/session"
	portalpres "boilerplate/api/internal/portal/portalpres"
	pppoeapp "boilerplate/api/internal/pppoe/application"
	pppoeinfra "boilerplate/api/internal/pppoe/infrastructure"
	pppoepres "boilerplate/api/internal/pppoe/presentation"
	reportsapp "boilerplate/api/internal/reports/application"
	reportsinfra "boilerplate/api/internal/reports/infrastructure"
	reportspres "boilerplate/api/internal/reports/presentation"
	schedapp "boilerplate/api/internal/scheduler/application"
	schedinfra "boilerplate/api/internal/scheduler/infrastructure"
	schedpres "boilerplate/api/internal/scheduler/presentation"
	"boilerplate/api/internal/security"
	sessapp "boilerplate/api/internal/sessions/application"
	sessinfra "boilerplate/api/internal/sessions/infrastructure"
	sesspres "boilerplate/api/internal/sessions/presentation"
	setapp "boilerplate/api/internal/settings/application"
	setinfra "boilerplate/api/internal/settings/infrastructure"
	setpres "boilerplate/api/internal/settings/presentation"
	"boilerplate/api/internal/shared/config"
	topupapp "boilerplate/api/internal/topup/application"
	topupinfra "boilerplate/api/internal/topup/infrastructure"
	topuppres "boilerplate/api/internal/topup/presentation"
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
		// Explicit 1MB cap for all API JSON (incl. kriskris webhooks) and
		// multipart uploads (avatars allow 512KB + envelope, still below).
		BodyLimit: 1 << 20,
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
		// G2 (agen C1, ADITIF): Fiber v3 group middleware matches by path
		// prefix, so the public kriskris webhook (which cannot send our
		// CSRF token and authenticates via HMAC instead) needs an explicit
		// skip. Every other /api route keeps CSRF protection unchanged.
		Next: func(c fiber.Ctx) bool {
			return c.Path() == "/api/v1/webhooks/kriskris"
		},
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
	registerBillingRoutes(app, api, cfg, pool)

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

// SchedulerStateKey is the app.State key holding the G2 *schedapp.Service
// built by registerBillingRoutes. cmd/api retrieves it to start the ticker;
// the status endpoint reads the same instance. (Fase 2, agen C1, ADITIF.)
const SchedulerStateKey = "g2_scheduler"

// registerBillingRoutes wires billing Fase 1 contexts (ADITIF) plus the Fase 2
// (agen C1) routes appended at the end. app is needed for the CSRF-exempt
// public webhook route and the scheduler state stash.
func registerBillingRoutes(app *fiber.App, api fiber.Router, cfg config.Config, pool *sql.DB) {
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
	// G4 (agen E1, ADITIF): E2 rate limiter, 30 req/min (burst 15). One
	// instance per app (per Limit call) so buckets are shared by every
	// request; each test builds a fresh app, so no G1/G2/G3 test trips
	// it. The G1 login limiter stays as defense in depth.
	auth.Use(security.Limit(30, 15))
	auth.Post("/login", authHandler.Login)
	auth.Post("/refresh", authHandler.Refresh)
	auth.Post("/logout", authHandler.Logout)
	auth.Get("/me", jwtpres.Authenticate(authSvc), authHandler.Me)
	auth.Post("/password/reset-request", authHandler.RequestPasswordReset)
	auth.Post("/password/reset", authHandler.ResetPassword)

	admin := v1.Group("/admin")

	// G4 (agen E1, ADITIF): one audit sink per register func (the
	// service is a stateless wrapper over queries, so sharing one
	// instance across funcs would only add plumbing).
	g4audit := auditapp.NewService(auditinfra.NewStore(queries))

	custHandler := custpres.NewHandler(custapp.NewService(custinfra.NewSQLRepository(queries)))
	custHandler.SetAuditor(g4audit)
	customers := admin.Group("/customers")
	customers.Get("/", jwtpres.Require(authSvc, jwtauthdomain.PermCustomerRead), custHandler.List)
	customers.Get("/:id", jwtpres.Require(authSvc, jwtauthdomain.PermCustomerRead), custHandler.Get)
	customers.Post("/", jwtpres.Require(authSvc, jwtauthdomain.PermCustomerWrite), custHandler.Create)
	customers.Put("/:id", jwtpres.Require(authSvc, jwtauthdomain.PermCustomerWrite), custHandler.Update)
	customers.Patch("/:id", jwtpres.Require(authSvc, jwtauthdomain.PermCustomerWrite), custHandler.Update)
	customers.Delete("/:id", jwtpres.Require(authSvc, jwtauthdomain.PermCustomerWrite), custHandler.Delete)

	pkgHandler := pkgpres.NewHandler(pkgapp.NewService(pkginfra.NewSQLRepository(queries, events)))
	pkgHandler.SetAuditor(g4audit)
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
	pppoeHandler.SetAuditor(g4audit)
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
	nasHandler.SetAuditor(g4audit)
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
	// G2 (agen C1): named so the paid hook can be installed below.
	billSvc := billapp.NewService(billRepo, billRepo, billRepo)
	billHandler := billpres.NewHandler(billSvc)
	billHandler.SetAuditor(g4audit)
	invoices := admin.Group("/invoices")
	invoices.Get("/", jwtpres.Require(authSvc, jwtauthdomain.PermInvoiceRead), billHandler.List)
	invoices.Get("/:id", jwtpres.Require(authSvc, jwtauthdomain.PermInvoiceRead), billHandler.Get)
	invoices.Post("/", jwtpres.Require(authSvc, jwtauthdomain.PermInvoiceWrite), billHandler.Create)
	invoices.Post("/:id/void", jwtpres.Require(authSvc, jwtauthdomain.PermInvoiceWrite), billHandler.Void)
	invoices.Post("/:id/manual-payment", jwtpres.Require(authSvc, jwtauthdomain.PermPaymentWrite), billHandler.ManualPayment)

	registerG2Routes(app, v1, admin, auth, cfg, pool, queries, events, coaSender, authSvc, authHandler, billSvc, pppoeRepo)
}

// registerG2Routes wires billing Fase 2 (agen C1, ADITIF): settings, password
// change, notify dispatcher, scheduler, and the C2/C3 portal routes. Fase 1
// routes above are untouched.
func registerG2Routes(
	app *fiber.App,
	v1, admin, auth fiber.Router,
	cfg config.Config,
	pool *sql.DB,
	queries *sqlcgen.Queries,
	events *notify.Notifier,
	coaSender *coa.Sender,
	authSvc *jwtapp.Service,
	authHandler *jwtpres.Handler,
	billSvc *billapp.Service,
	pppoeRepo *pppoeinfra.SQLRepository,
) {
	// G4 (agen E1, ADITIF): audit sink for the G2 handlers.
	g4audit := auditapp.NewService(auditinfra.NewStore(queries))
	authHandler.SetAuditor(g4audit)

	// Authenticated password change (any role: admin and portal users).
	auth.Post("/password/change", jwtpres.Authenticate(authSvc), authHandler.ChangePassword)

	// Settings (admin). GET and PUT both require settings:write: no
	// settings:read permission exists because the Fase 1 RBAC permission
	// counts are locked by jwtauth tests.
	setSvc := setapp.NewService(setinfra.NewSQLRepository(queries), setinfra.NewCrypter(cfg.SettingsEncKey))
	setHandler := setpres.NewHandler(setSvc)
	setHandler.SetAuditor(g4audit)
	settings := admin.Group("/settings")
	settingsRead := jwtpres.Require(authSvc, jwtauthdomain.PermSettingsWrite)
	settings.Get("/", settingsRead, setHandler.List)
	settings.Put("/", settingsRead, setHandler.Update)

	// Notify dispatcher (no HTTP routes of its own).
	notifyDispatcher := notifyapp.NewDispatcher(
		notifyinfra.NewSettingsAdapter(setSvc),
		notifyinfra.NewSQLLogRepository(queries),
		notifyinfra.LogSender{},
		func(smtpCfg notifyapp.SMTPConfig) notifydomain.Sender {
			return notifyinfra.NewSMTPSender(smtpCfg)
		},
	)

	// G3 (agen D2, ADITIF): top-up service, built before the paid-hook
	// wiring below so manual payments can credit top-ups.
	topupStore := topupinfra.NewStore(queries)
	topupSvc := topupapp.NewService(topupapp.Deps{
		Accounts: topupStore,
		Prices:   topupStore,
		Topups:   topupStore,
		Invoices: topupStore,
		Billing:  topupinfra.NewBillingAdapter(billSvc),
		Events:   events,
	})

	// Scheduler: paid hook on manual payments, service, status route.
	schedStore := schedinfra.NewStore(queries)
	schedConfig := schedinfra.NewConfigAdapter(setSvc)
	// G3: the G2 paid notification keeps its place; top-up apply-on-paid
	// joins as a second fanout branch (selective + idempotent no-op for
	// non-top-up invoices).
	billSvc.SetPaidNotifier(billapp.FanoutNotifiers(
		schedinfra.NewPaidHook(schedStore, notifyDispatcher, schedConfig),
		topupinfra.NewPaidHook(topupSvc),
	))
	schedSvc := schedapp.NewService(schedapp.Deps{
		Store:   schedStore,
		Billing: schedinfra.NewBillingAdapter(billSvc),
		PPPoE:   schedinfra.NewPPPoEAdapter(pppoeRepo),
		Notify:  notifyDispatcher,
		CoA:     pppoeinfra.NewCoADisconnecter(queries, coaSender),
		Events:  events,
		Config:  schedConfig,
	})
	app.State().Set(SchedulerStateKey, schedSvc)
	schedHandler := schedpres.NewHandler(schedSvc)
	scheduler := admin.Group("/scheduler")
	scheduler.Get("/status", jwtpres.Require(authSvc, jwtauthdomain.PermSettingsWrite), schedHandler.Status)

	// C2 kriskris + C3 portal (agen C1 wiring; handler milik C2/C3).
	// Portal routes go through RequireWithBillingContext, which fills
	// c.Locals("billing_customer_id") and c.Locals("billing_role") for the
	// handlers (they never parse JWT themselves).
	krisHandler := kriskrispres.NewHandler(pool)
	// G3 (agen D2, ADITIF): confirmed QRIS payments credit top-ups.
	krisHandler.SetPaidHook(func(ctx context.Context, invoiceID string) {
		if err := topupSvc.ApplyOnInvoicePaid(ctx, invoiceID); err != nil {
			log.Printf("topup: apply on paid %s: %v", invoiceID, err)
		}
	})
	portalHandler := portalpres.NewHandler(pool)
	portal := v1.Group("/portal")
	portal.Get("/summary", jwtpres.RequireWithBillingContext(authSvc, jwtauthdomain.PermCustomerReadOwn), portalHandler.Summary)
	portal.Get("/invoices", jwtpres.RequireWithBillingContext(authSvc, jwtauthdomain.PermInvoiceReadOwn), portalHandler.Invoices)
	portal.Get("/invoices/:id", jwtpres.RequireWithBillingContext(authSvc, jwtauthdomain.PermInvoiceReadOwn), portalHandler.InvoiceDetail)
	portal.Get("/usage", jwtpres.RequireWithBillingContext(authSvc, jwtauthdomain.PermSessionReadOwn), portalHandler.Usage)
	portal.Post("/invoices/:id/pay", jwtpres.RequireWithBillingContext(authSvc, jwtauthdomain.PermPaymentWriteOwn), krisHandler.PayInvoice)
	portal.Get("/payment-attempts/:id", jwtpres.RequireWithBillingContext(authSvc, jwtauthdomain.PermInvoiceReadOwn), krisHandler.AttemptStatus)

	registerG3Routes(admin, portal, queries, events, coaSender, authSvc, billSvc, pppoeRepo, schedSvc, topupSvc)

	// Public kriskris webhook: no JWT. Registered directly on app (not on
	// the /api group) so the group's CSRF middleware does not apply —
	// kriskris cannot send our CSRF token. App-level middleware (helmet,
	// recover, logger) still applies; all pre-existing routes keep their
	// CSRF protection unchanged.
	// G4 (agen E1, ADITIF): E2 rate limiter, 120 req/min (burst 60),
	// first in chain.
	app.Post("/api/v1/webhooks/kriskris", security.Limit(120, 60), krisHandler.Webhook)

	registerG4Routes(admin, pool, queries, authSvc, schedSvc)
}

// registerG4Routes wires billing Fase 4 (agen E1/G4, ADITIF): the admin
// audit log, dashboard, and metrics endpoints. G1/G2/G3 routes above
// are untouched.
func registerG4Routes(
	admin fiber.Router,
	pool *sql.DB,
	queries *sqlcgen.Queries,
	authSvc *jwtapp.Service,
	schedSvc *schedapp.Service,
) {
	auditSvc := auditapp.NewService(auditinfra.NewStore(queries))
	auditHandler := auditpres.NewHandler(auditSvc)
	auditLogs := admin.Group("/audit-logs")
	auditLogs.Get("/", jwtpres.Require(authSvc, jwtauthdomain.PermAuditRead), auditHandler.List)

	dashSvc := dashapp.NewService(dashinfra.NewStore(queries))
	dashSvc.SetPing(func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		return pool.PingContext(ctx)
	})
	dashSvc.SetScheduler(func() []dashdomain.SchedulerJob {
		return dashapp.SchedulerJobs(schedSvc.Status())
	})
	dashHandler := dashpres.NewHandler(dashSvc)
	admin.Get("/dashboard", jwtpres.Require(authSvc, jwtauthdomain.PermInvoiceRead), dashHandler.Dashboard)
	admin.Get("/metrics", jwtpres.Require(authSvc, jwtauthdomain.PermInvoiceRead), dashHandler.Metrics)
}

// registerG3Routes wires billing Fase 3 (agen G3/D2, ADITIF): portal
// top-up, self-service plan changes + public package catalog, admin plan
// changes, JSON reports, and the scheduler downgrade applier. G1/G2
// routes above are untouched.
func registerG3Routes(
	admin, portal fiber.Router,
	queries *sqlcgen.Queries,
	events *notify.Notifier,
	coaSender *coa.Sender,
	authSvc *jwtapp.Service,
	billSvc *billapp.Service,
	pppoeRepo *pppoeinfra.SQLRepository,
	schedSvc *schedapp.Service,
	topupSvc *topupapp.Service,
) {
	topupHandler := topuppres.NewHandler(topupSvc)

	planStore := planchangesinfra.NewStore(queries, pppoeRepo)
	planSvc := planchangesapp.NewService(planchangesapp.Deps{
		Accounts: planStore,
		Plans:    planStore,
		Billing:  planchangesinfra.NewBillingAdapter(billSvc),
		Disco:    pppoeinfra.NewCoADisconnecter(queries, coaSender),
	})
	planHandler := planchangespres.NewHandler(planSvc, planchangesinfra.NewPackageCatalog(queries))
	// G4 (agen E1, ADITIF): audit sink for self-service plan changes.
	planHandler.SetAuditor(auditapp.NewService(auditinfra.NewStore(queries)))
	schedSvc.SetPlanChangeApplier(schedinfra.NewPlanChangesAdapter(planSvc))

	reportsSvc := reportsapp.NewService(reportsinfra.NewStore(queries))
	reportsHandler := reportspres.NewHandler(reportsSvc)

	// Portal self-service: same *_own permission style as the G2 portal.
	portal.Post("/topup", jwtpres.RequireWithBillingContext(authSvc, jwtauthdomain.PermPaymentWriteOwn), topupHandler.Topup)
	portal.Post("/plan-changes", jwtpres.RequireWithBillingContext(authSvc, jwtauthdomain.PermPaymentWriteOwn), planHandler.Create)
	portal.Get("/plan-changes", jwtpres.RequireWithBillingContext(authSvc, jwtauthdomain.PermInvoiceReadOwn), planHandler.List)
	portal.Post("/plan-changes/:id/cancel", jwtpres.RequireWithBillingContext(authSvc, jwtauthdomain.PermPaymentWriteOwn), planHandler.Cancel)
	portal.Get("/packages", jwtpres.RequireWithBillingContext(authSvc, jwtauthdomain.PermPackageRead), planHandler.Packages)

	// Admin plan changes + reports (admin holds every global permission).
	admin.Get("/plan-changes", jwtpres.Require(authSvc, jwtauthdomain.PermPPPoERead), planHandler.ListAdmin)
	reports := admin.Group("/reports")
	reportsRead := jwtpres.Require(authSvc, jwtauthdomain.PermInvoiceRead)
	reports.Get("/revenue", reportsRead, reportsHandler.Revenue)
	reports.Get("/usage", reportsRead, reportsHandler.Usage)
	reports.Get("/receivables", reportsRead, reportsHandler.Receivables)
	reports.Get("/growth", reportsRead, reportsHandler.Growth)
}
