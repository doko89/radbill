// Command api boots the monolith: config, database, seed, HTTP server.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/gofiber/fiber/v3"

	"boilerplate/api/internal/kriskris/application"
	"boilerplate/api/internal/platform/db"
	"boilerplate/api/internal/platform/httpserver"
	schedapp "boilerplate/api/internal/scheduler/application"
	"boilerplate/api/internal/shared/config"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	pool, err := db.Open(cfg.DBDriver, cfg.SQLitePath, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := pool.PingContext(ctx); err != nil {
		return fmt.Errorf("ping db: %w", err)
	}
	if err := db.Migrate(ctx, pool, cfg.DBDriver); err != nil {
		return err
	}
	if err := db.SeedAdmin(ctx, queriesOf(pool), cfg.AdminUser, cfg.AdminPass, bcrypt.DefaultCost); err != nil {
		return err
	}
	if err := db.SeedBillingAdmin(ctx, queriesOf(pool), cfg.AdminUser, cfg.AdminPass); err != nil {
		return err
	}

	app, err := httpserver.New(cfg, pool)
	if err != nil {
		return err
	}

	// Billing Fase 2 (agen C1, ADITIF): start the in-process scheduler
	// (invoice generation + grace isolation) on the same service instance
	// the status endpoint reads. Stopped gracefully below.
	stopScheduler := startG2Scheduler(app, cfg)
	// C2 kriskris backup poller (agen C1 wiring; poller milik C2).
	stopPoller := kriskrisapp.StartPoller(context.Background(), pool)

	go func() {
		if err := app.Listen(":" + cfg.Port); err != nil {
			fmt.Fprintln(os.Stderr, "listen:", err)
		}
	}()
	fmt.Printf("listening on :%s (env=%s driver=%s)\n", cfg.Port, cfg.Env, cfg.DBDriver)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	fmt.Println("shutting down...")
	stopScheduler()
	stopPoller()
	return app.ShutdownWithTimeout(5 * time.Second)
}

// startG2Scheduler runs the billing ticker on the scheduler service stashed
// in app state by registerBillingRoutes. It returns a stop func (no-op when
// the service is missing, e.g. in tests that never start the loop).
func startG2Scheduler(app *fiber.App, cfg config.Config) func() {
	noop := func() {}
	v, ok := app.State().Get(httpserver.SchedulerStateKey)
	if !ok {
		return noop
	}
	svc, ok := v.(*schedapp.Service)
	if !ok || svc == nil {
		return noop
	}
	interval := time.Duration(cfg.SchedulerIntervalSeconds) * time.Second
	return schedapp.NewRunner(svc, interval).Start(context.Background())
}
