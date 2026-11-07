package radius

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
)

// Queries below use the $1 placeholder subset that runs on both sqlite and
// postgres (boilerplate convention).
const (
	qListNAS = `SELECT id, name, ip, secret, coa_port FROM nas`
	qGetNAS  = `SELECT id, name, ip, secret, coa_port FROM nas WHERE id = $1`

	qListAccounts = `SELECT id, customer_id, package_id, nas_id, username, password, status, expires_at, static_ip, created_at
		FROM pppoe_accounts WHERE status IN ('active', 'isolated')`
	qGetAccount = `SELECT id, customer_id, package_id, nas_id, username, password, status, expires_at, static_ip, created_at
		FROM pppoe_accounts WHERE id = $1`

	qListPackages = `SELECT id, name, price, period, rate_limit, address_pool, validity_seconds,
		online_seconds_limit, quota_bytes, quota_reset, fup_threshold_bytes, fup_rate_limit,
		on_exhausted, is_public, allow_self_service, upgrade_group FROM packages`
	qGetPackage = `SELECT id, name, price, period, rate_limit, address_pool, validity_seconds,
		online_seconds_limit, quota_bytes, quota_reset, fup_threshold_bytes, fup_rate_limit,
		on_exhausted, is_public, allow_self_service, upgrade_group FROM packages WHERE id = $1`

	qListSettings = `SELECT key, value FROM settings`
)

// Cache holds the NAS, account, package and settings rows in memory. The
// auth path only touches the cache, never the database.
type Cache struct {
	mu       sync.RWMutex
	nasByIP  map[string]*NAS
	nasByID  map[string]*NAS
	accounts map[string]*Account // by username
	packages map[string]*Package // by id
	settings map[string]string
}

// NewCache returns an empty cache.
func NewCache() *Cache {
	return &Cache{
		nasByIP:  make(map[string]*NAS),
		nasByID:  make(map[string]*NAS),
		accounts: make(map[string]*Account),
		packages: make(map[string]*Package),
		settings: make(map[string]string),
	}
}

// LoadAll reloads every cached table and swaps the maps atomically. On
// error the previous content is kept untouched.
func (c *Cache) LoadAll(ctx context.Context, db *sql.DB) error {
	fresh := NewCache()
	if err := fresh.loadNAS(ctx, db); err != nil {
		return err
	}
	if err := fresh.loadAccounts(ctx, db); err != nil {
		return err
	}
	if err := fresh.loadPackages(ctx, db); err != nil {
		return err
	}
	if err := fresh.loadSettings(ctx, db); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nasByIP, c.nasByID = fresh.nasByIP, fresh.nasByID
	c.accounts = fresh.accounts
	c.packages = fresh.packages
	c.settings = fresh.settings
	return nil
}

func (c *Cache) loadNAS(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, qListNAS)
	if err != nil {
		return fmt.Errorf("load nas: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		n, err := scanNAS(rows)
		if err != nil {
			return err
		}
		c.storeNAS(&n)
	}
	return rows.Err()
}

func (c *Cache) loadAccounts(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, qListAccounts)
	if err != nil {
		return fmt.Errorf("load pppoe_accounts: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return err
		}
		c.accounts[a.Username] = &a
	}
	return rows.Err()
}

func (c *Cache) loadPackages(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, qListPackages)
	if err != nil {
		return fmt.Errorf("load packages: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		p, err := scanPackage(rows)
		if err != nil {
			return err
		}
		c.packages[p.ID] = &p
	}
	return rows.Err()
}

func (c *Cache) loadSettings(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, qListSettings)
	if err != nil {
		return fmt.Errorf("load settings: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return fmt.Errorf("scan settings: %w", err)
		}
		c.settings[k] = v
	}
	return rows.Err()
}

func (c *Cache) storeNAS(n *NAS) {
	c.nasByID[n.ID] = n
	c.nasByIP[n.IP] = n
	if norm := normIP(n.IP); norm != n.IP {
		c.nasByIP[norm] = n
	}
}

// normIP canonicalises an IP literal so NAS lookup matches regardless of
// textual form (and strips any zone suffix).
func normIP(s string) string {
	if i := strings.IndexByte(s, '%'); i >= 0 {
		s = s[:i]
	}
	if ip := net.ParseIP(s); ip != nil {
		return ip.String()
	}
	return s
}

// NASByIP returns the NAS record for a source address. ok is false for
// unknown senders, whose packets must be dropped without a response.
func (c *Cache) NASByIP(ip string) (nas *NAS, ok bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if n, found := c.nasByIP[ip]; found {
		return n, true
	}
	n, found := c.nasByIP[normIP(ip)]
	return n, found
}

// NASByID returns the NAS record for an id.
func (c *Cache) NASByID(id string) (nas *NAS, ok bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	n, found := c.nasByID[id]
	return n, found
}

// Account returns the cached account for a username.
func (c *Cache) Account(username string) (acct *Account, ok bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	a, found := c.accounts[username]
	return a, found
}

// Package returns the cached package for an id.
func (c *Cache) Package(id string) (pkg *Package, ok bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	p, found := c.packages[id]
	return p, found
}

// Setting returns a settings value or the fallback when absent.
func (c *Cache) Setting(key, fallback string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if v, ok := c.settings[key]; ok {
		return v
	}
	return fallback
}

// InterimInterval returns the Acct-Interim-Interval from settings, or the
// default when missing or unparseable.
func (c *Cache) InterimInterval() uint32 {
	v := c.Setting(SettingInterimInterval, "")
	if v == "" {
		return DefaultInterimInterval
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n <= 0 || n > 86400 {
		return DefaultInterimInterval
	}
	return uint32(n)
}

// Counts returns the number of cached accounts and NAS rows (for /healthz).
func (c *Cache) Counts() (accounts, nas int) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.accounts), len(c.nasByID)
}

// Notify types carried on the radbill_events channel.
const (
	NotifyPPPoE   = "pppoe"
	NotifyNAS     = "nas"
	NotifyPackage = "package"
)

// NotifyEvent is one parsed LISTEN/NOTIFY payload:
// {"type":"pppoe|nas|package","action":"upsert|delete","id":"..."}.
type NotifyEvent struct {
	Type   string `json:"type"`
	Action string `json:"action"`
	ID     string `json:"id"`
}

// ParseNotify parses a radbill_events payload.
func ParseNotify(payload []byte) (NotifyEvent, error) {
	var ev NotifyEvent
	if err := json.Unmarshal(payload, &ev); err != nil {
		return NotifyEvent{}, fmt.Errorf("parse notify: %w", err)
	}
	switch ev.Type {
	case NotifyPPPoE, NotifyNAS, NotifyPackage:
	default:
		return NotifyEvent{}, fmt.Errorf("parse notify: unknown type %q", ev.Type)
	}
	switch ev.Action {
	case "upsert", "delete":
	default:
		return NotifyEvent{}, fmt.Errorf("parse notify: unknown action %q", ev.Action)
	}
	if ev.ID == "" {
		return NotifyEvent{}, fmt.Errorf("parse notify: missing id")
	}
	return ev, nil
}

// ReloadRow re-reads one row by id after an upsert notification. When the
// row no longer exists (or a cached account left active/isolated status)
// the entry is dropped instead.
func (c *Cache) ReloadRow(ctx context.Context, db *sql.DB, typ, id string) error {
	switch typ {
	case NotifyNAS:
		return c.reloadOne(ctx, db, qGetNAS, id, func(rows *sql.Rows) error {
			n, err := scanNAS(rows)
			if err != nil {
				return err
			}
			c.mu.Lock()
			defer c.mu.Unlock()
			if old, ok := c.nasByID[n.ID]; ok && old.IP != n.IP {
				delete(c.nasByIP, old.IP)
				delete(c.nasByIP, normIP(old.IP))
			}
			c.storeNAS(&n)
			return nil
		}, func() { c.dropNASLocked(id) })
	case NotifyPPPoE:
		return c.reloadOne(ctx, db, qGetAccount, id, func(rows *sql.Rows) error {
			a, err := scanAccount(rows)
			if err != nil {
				return err
			}
			c.mu.Lock()
			defer c.mu.Unlock()
			if a.Status != StatusActive && a.Status != StatusIsolated {
				deleteByAccountID(c.accounts, id)
				return nil
			}
			deleteByAccountID(c.accounts, id) // username may have changed
			c.accounts[a.Username] = &a
			return nil
		}, func() {
			c.mu.Lock()
			defer c.mu.Unlock()
			deleteByAccountID(c.accounts, id)
		})
	case NotifyPackage:
		return c.reloadOne(ctx, db, qGetPackage, id, func(rows *sql.Rows) error {
			p, err := scanPackage(rows)
			if err != nil {
				return err
			}
			c.mu.Lock()
			defer c.mu.Unlock()
			c.packages[p.ID] = &p
			return nil
		}, func() {
			c.mu.Lock()
			defer c.mu.Unlock()
			delete(c.packages, id)
		})
	default:
		return fmt.Errorf("reload: unknown type %q", typ)
	}
}

// reloadOne runs a by-id query: store(row) when found, onMissing when the
// row is gone.
func (c *Cache) reloadOne(ctx context.Context, db *sql.DB, query, id string, store func(*sql.Rows) error, onMissing func()) error {
	rows, err := db.QueryContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("reload %s: %w", id, err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return fmt.Errorf("reload %s: %w", id, err)
		}
		onMissing()
		return nil
	}
	if err := store(rows); err != nil {
		return err
	}
	return rows.Err()
}

// DropRow removes one cached entry after a delete notification.
func (c *Cache) DropRow(typ, id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch typ {
	case NotifyNAS:
		c.dropNASLocked(id)
	case NotifyPPPoE:
		deleteByAccountID(c.accounts, id)
	case NotifyPackage:
		delete(c.packages, id)
	}
}

func (c *Cache) dropNASLocked(id string) {
	if old, ok := c.nasByID[id]; ok {
		delete(c.nasByIP, old.IP)
		delete(c.nasByIP, normIP(old.IP))
		delete(c.nasByID, id)
	}
}

func deleteByAccountID(m map[string]*Account, id string) {
	for username, a := range m {
		if a.ID == id {
			delete(m, username)
		}
	}
}
