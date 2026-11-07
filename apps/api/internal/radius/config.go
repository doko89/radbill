// Package radius implements the radbill RADIUS server (auth + accounting).
package radius

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

// Config is the runtime configuration of radiusd. All values come from the
// environment; see LoadConfig.
type Config struct {
	DBDriver    string // sqlite | postgres
	SQLitePath  string
	DatabaseURL string

	AuthAddr   string // UDP, default :1812
	AcctAddr   string // UDP, default :1813
	HealthAddr string // TCP, default :8081

	CachePollSeconds int // full cache reload backup, default 60
	TZ               string
}

// LoadConfig reads radiusd configuration from the environment.
func LoadConfig() (Config, error) {
	cfg := Config{
		DBDriver:         getenv("DB_DRIVER", "sqlite"),
		SQLitePath:       getenv("SQLITE_PATH", "./data/app.db"),
		DatabaseURL:      getenv("DATABASE_URL", ""),
		AuthAddr:         getenv("RADIUS_AUTH_ADDR", ":1812"),
		AcctAddr:         getenv("RADIUS_ACCT_ADDR", ":1813"),
		HealthAddr:       getenv("RADIUS_HEALTH_ADDR", ":8081"),
		CachePollSeconds: 60,
		TZ:               getenv("TZ", ""),
	}
	switch cfg.DBDriver {
	case "sqlite", "postgres":
	default:
		return Config{}, fmt.Errorf("DB_DRIVER must be sqlite or postgres, got %q", cfg.DBDriver)
	}
	if cfg.DBDriver == "postgres" && cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required when DB_DRIVER=postgres")
	}
	if v, ok := os.LookupEnv("RADIUS_CACHE_POLL_SECONDS"); ok && v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return Config{}, fmt.Errorf("RADIUS_CACHE_POLL_SECONDS must be a positive integer, got %q", v)
		}
		cfg.CachePollSeconds = n
	}
	if cfg.TZ != "" {
		if _, err := time.LoadLocation(cfg.TZ); err != nil {
			return Config{}, fmt.Errorf("invalid TZ %q: %w", cfg.TZ, err)
		}
	}
	return cfg, nil
}

// PollInterval returns the periodic full-cache-reload interval.
func (c Config) PollInterval() time.Duration {
	return time.Duration(c.CachePollSeconds) * time.Second
}

func getenv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

// OpenDB connects to sqlite (file path) or postgres (URL) based on the
// driver. DSN shape mirrors platform/db.Open (same drivers, same pragmas).
func OpenDB(cfg Config) (*sql.DB, error) {
	switch cfg.DBDriver {
	case "sqlite":
		var dsn string
		if cfg.SQLitePath == ":memory:" {
			dsn = "file::memory:?cache=shared&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
		} else {
			if err := os.MkdirAll(filepath.Dir(cfg.SQLitePath), 0o755); err != nil {
				return nil, fmt.Errorf("create sqlite dir: %w", err)
			}
			dsn = "file:" + cfg.SQLitePath + "?cache=shared&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
		}
		pool, err := sql.Open("sqlite", dsn)
		if err != nil {
			return nil, fmt.Errorf("open sqlite: %w", err)
		}
		pool.SetMaxOpenConns(1)
		return pool, nil
	case "postgres":
		pool, err := sql.Open("pgx", cfg.DatabaseURL)
		if err != nil {
			return nil, fmt.Errorf("open postgres: %w", err)
		}
		pool.SetMaxOpenConns(10)
		pool.SetConnMaxLifetime(30 * time.Minute)
		return pool, nil
	default:
		return nil, fmt.Errorf("unknown DB_DRIVER %q", cfg.DBDriver)
	}
}
