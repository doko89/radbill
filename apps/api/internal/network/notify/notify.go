// Package notify emits best-effort PostgreSQL LISTEN/NOTIFY events so the
// standalone radiusd process can refresh its in-memory cache. On sqlite
// the call is a no-op (radiusd falls back to polling).
package notify

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
)

// Channel is the postgres channel radiusd LISTENs on.
const Channel = "radbill_events"

// Event types and actions carried in the payload.
const (
	TypePPPoE   = "pppoe"
	TypeNAS     = "nas"
	TypePackage = "package"

	ActionUpsert = "upsert"
	ActionDelete = "delete"
)

// Notifier sends radbill_events; failures only log (best-effort).
type Notifier struct {
	db     *sql.DB
	driver string
}

// New builds a Notifier. driver is "postgres" or "sqlite".
func New(db *sql.DB, driver string) *Notifier {
	return &Notifier{db: db, driver: driver}
}

// Emit publishes {"type","action","id"} on postgres; a no-op on sqlite.
// It never returns an error: delivery failures are logged, not fatal.
func (n *Notifier) Emit(ctx context.Context, typ, action, id string) {
	if n == nil || n.driver != "postgres" || n.db == nil {
		return
	}
	payload, err := json.Marshal(map[string]string{"type": typ, "action": action, "id": id})
	if err != nil {
		log.Printf("notify: marshal %s/%s/%s: %v", typ, action, id, err)
		return
	}
	if _, err := n.db.ExecContext(ctx, "SELECT pg_notify('radbill_events', $1)", string(payload)); err != nil {
		log.Printf("notify: pg_notify %s: %v", payload, err)
	}
}
