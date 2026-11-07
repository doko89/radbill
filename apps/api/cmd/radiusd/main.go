// Command radiusd boots the standalone RADIUS server: config, database,
// cache warmup, UDP auth + accounting, /healthz, graceful shutdown.
//
// radiusd shares the database with billing but never calls it, so
// authentication keeps working while billing is down.
package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	lradius "layeh.com/radius"

	"boilerplate/api/internal/radius"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func run() error {
	log.SetFlags(0) // layeh PacketServer ErrorLog fallback stays quiet; we pass our own
	setupLogging()

	cfg, err := radius.LoadConfig()
	if err != nil {
		return err
	}
	if cfg.TZ != "" {
		if loc, err := time.LoadLocation(cfg.TZ); err == nil {
			time.Local = loc
		}
	}
	logger := slog.Default()

	pool, err := radius.OpenDB(cfg)
	if err != nil {
		return err
	}
	defer pool.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := pool.PingContext(ctx); err != nil {
		return fmt.Errorf("ping db: %w", err)
	}

	cache := radius.NewCache()
	if err := cache.LoadAll(ctx, pool); err != nil {
		return fmt.Errorf("cache warmup: %w", err)
	}
	accts, nas := cache.Counts()
	logger.Info("cache warmup ok", "accounts", accts, "nas", nas)

	usage := radius.NewUsageMirror(logger)
	if err := usage.Warmup(ctx, pool); err != nil {
		return fmt.Errorf("usage warmup: %w", err)
	}
	metrics := &radius.Metrics{}
	enforcer := radius.NewEnforcer(logger, metrics)

	sessions := radius.NewSessionTracker()
	writer := radius.NewWriter(pool, logger)

	wctx, wcancel := context.WithCancel(context.Background())
	defer wcancel()
	go radius.RunWatcher(wctx, logger, pool, cache, cfg, usage)
	go writer.Run(wctx)

	secrets := &radius.Secrets{Cache: cache, Log: logger}
	authSrv := &lradius.PacketServer{
		Addr:         cfg.AuthAddr,
		SecretSource: secrets,
		Handler:      &radius.AuthServer{Cache: cache, Log: logger, Usage: usage},
		ErrorLog:     slogBridge(logger, "auth"),
	}
	acctSrv := &lradius.PacketServer{
		Addr:         cfg.AcctAddr,
		SecretSource: secrets,
		Handler:      &radius.AcctServer{Cache: cache, Log: logger, Sessions: sessions, Writer: writer, Usage: usage, Enforce: enforcer},
		ErrorLog:     slogBridge(logger, "acct"),
	}
	go func() {
		if err := authSrv.ListenAndServe(); err != nil && err != lradius.ErrServerShutdown {
			logger.Error("auth server exited", "err", err)
		}
	}()
	go func() {
		if err := acctSrv.ListenAndServe(); err != nil && err != lradius.ErrServerShutdown {
			logger.Error("acct server exited", "err", err)
		}
	}()

	healthSrv := &http.Server{
		Addr:              cfg.HealthAddr,
		Handler:           (&radius.Health{Cache: cache, Writer: writer, Metrics: metrics, Start: time.Now()}).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		if err := healthSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("health server exited", "err", err)
		}
	}()
	logger.Info("radiusd listening", "auth", cfg.AuthAddr, "acct", cfg.AcctAddr,
		"health", cfg.HealthAddr, "driver", cfg.DBDriver)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	logger.Info("shutting down...")
	signal.Stop(quit)

	sctx, scancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer scancel()
	_ = authSrv.Shutdown(sctx)
	_ = acctSrv.Shutdown(sctx)
	_ = healthSrv.Shutdown(sctx)
	wcancel() // stops the watcher; the writer drains its queue first
	select {
	case <-writer.Done():
	case <-sctx.Done():
		return fmt.Errorf("shutdown: writer drain timed out")
	}
	return nil
}

// setupLogging installs a text slog handler; LOG_LEVEL (debug|info|warn|
// error) selects verbosity, default info.
func setupLogging() {
	level := slog.LevelInfo
	switch strings.ToLower(strings.TrimSpace(os.Getenv("LOG_LEVEL"))) {
	case "debug":
		level = slog.LevelDebug
	case "warn", "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))
}

// slogBridge adapts a slog logger to the *log.Logger the RADIUS server
// wants for packet errors.
func slogBridge(logger *slog.Logger, scope string) *log.Logger {
	return log.New(slogWriter{logger: logger.With("scope", "radius-"+scope)}, "", 0)
}

type slogWriter struct{ logger *slog.Logger }

func (w slogWriter) Write(p []byte) (int, error) {
	w.logger.Warn("packet error", "err", strings.TrimSpace(string(p)))
	return len(p), nil
}
