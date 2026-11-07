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

	"boilerplate/api/internal/platform/db"
	"boilerplate/api/internal/platform/httpserver"
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

	app, err := httpserver.New(cfg, pool)
	if err != nil {
		return err
	}

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
	return app.ShutdownWithTimeout(5 * time.Second)
}
