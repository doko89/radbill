package radius

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
)

// NotifyChannel is the postgres LISTEN/NOTIFY channel billing uses to
// announce pppoe/nas/package row changes.
const NotifyChannel = "radbill_events"

// RunWatcher keeps the cache fresh until ctx is cancelled. On postgres it
// LISTENs for row-level invalidations with a periodic full reload as
// backup; on sqlite it only polls. It never touches the auth path.
func RunWatcher(ctx context.Context, log *slog.Logger, db *sql.DB, cache *Cache, cfg Config) {
	go runPoller(ctx, log, db, cache, cfg.PollInterval())
	if cfg.DBDriver != "postgres" {
		<-ctx.Done()
		return
	}
	runListener(ctx, log, db, cache, cfg.DatabaseURL)
}

func runPoller(ctx context.Context, log *slog.Logger, db *sql.DB, cache *Cache, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			pctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			err := cache.LoadAll(pctx, db)
			cancel()
			if err != nil {
				log.Warn("cache poll reload failed", "err", err)
				continue
			}
			accts, nas := cache.Counts()
			log.Debug("cache poll reload ok", "accounts", accts, "nas", nas)
		}
	}
}

// runListener holds one native pgx connection in LISTEN state and applies
// each notification to the cache. Any connection failure is logged and
// retried; the poller covers the gap.
func runListener(ctx context.Context, log *slog.Logger, db *sql.DB, cache *Cache, databaseURL string) {
	const retryDelay = 5 * time.Second
	for {
		if err := listenOnce(ctx, log, db, cache, databaseURL); err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Warn("notify listener failed, retrying", "err", err, "retry_in", retryDelay)
			select {
			case <-ctx.Done():
				return
			case <-time.After(retryDelay):
			}
			continue
		}
		return
	}
}

func listenOnce(ctx context.Context, log *slog.Logger, db *sql.DB, cache *Cache, databaseURL string) error {
	conn, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer conn.Close(context.WithoutCancel(ctx))
	if _, err := conn.Exec(ctx, "LISTEN "+NotifyChannel); err != nil {
		return err
	}
	log.Info("listening for cache invalidations", "channel", NotifyChannel)
	for {
		n, err := conn.WaitForNotification(ctx)
		if err != nil {
			return err
		}
		applyNotification(ctx, log, db, cache, n.Payload)
	}
}

func applyNotification(ctx context.Context, log *slog.Logger, db *sql.DB, cache *Cache, payload string) {
	ev, err := ParseNotify([]byte(payload))
	if err != nil {
		log.Warn("ignoring malformed notify", "err", err)
		return
	}
	if ev.Action == "delete" {
		cache.DropRow(ev.Type, ev.ID)
		log.Debug("notify drop ok", "type", ev.Type, "id", ev.ID)
		return
	}
	rctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	rerr := cache.ReloadRow(rctx, db, ev.Type, ev.ID)
	cancel()
	if rerr != nil {
		log.Warn("notify reload failed", "type", ev.Type, "id", ev.ID, "err", rerr)
		return
	}
	log.Debug("notify reload ok", "type", ev.Type, "id", ev.ID)
}
