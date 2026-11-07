// Package domain holds the audit bounded-context core: log entries,
// filters, action vocabulary, and the store port.
//
// Fase 4 (agen E1/G4, ADITIF). Pure: no fiber/sql/os imports.
package domain

import (
	"context"
	"time"
)

// Audited actions. Only mutations are logged, plus the explicit
// pppoe.password_reveal read (PRD 5.2). Reads are never audited.
const (
	ActionCustomerCreated = "customer.created"
	ActionCustomerUpdated = "customer.updated"
	ActionCustomerDeleted = "customer.deleted"

	ActionPackageCreated = "package.created"
	ActionPackageUpdated = "package.updated"
	ActionPackageDeleted = "package.deleted"

	ActionPPPoECreated        = "pppoe.created"
	ActionPPPoEUpdated        = "pppoe.updated"
	ActionPPPoEDeleted        = "pppoe.deleted"
	ActionPPPoEIsolated       = "pppoe.isolated"
	ActionPPPoEActivated      = "pppoe.activated"
	ActionPPPoEDisabled       = "pppoe.disabled"
	ActionPPPoEExtended       = "pppoe.extended"
	ActionPPPoEPackageChanged = "pppoe.package_changed"
	ActionPPPoEQuotaReset     = "pppoe.quota_reset"
	ActionPPPoEDisconnected   = "pppoe.disconnected"
	ActionPPPoEPortalCreated  = "pppoe.portal_created"
	ActionPPPoEPortalReset    = "pppoe.portal_reset"
	ActionPPPoEPasswordReveal = "pppoe.password_reveal"

	ActionNASCreated = "nas.created"
	ActionNASUpdated = "nas.updated"
	ActionNASDeleted = "nas.deleted"

	ActionInvoiceCreated = "invoice.created"
	ActionInvoiceVoided  = "invoice.voided"
	ActionPaymentManual  = "payment.manual"

	ActionSettingsUpdated = "settings.updated"

	ActionAuthPasswordChange = "auth.password_change"

	ActionPlanChangeCreated   = "planchange.created"
	ActionPlanChangeCancelled = "planchange.cancelled"
)

// Target types stored in audit_logs.target_type.
const (
	TargetCustomer   = "customer"
	TargetPackage    = "package"
	TargetPPPoE      = "pppoe"
	TargetNAS        = "nas"
	TargetInvoice    = "invoice"
	TargetSettings   = "settings"
	TargetAccount    = "account"
	TargetPlanChange = "planchange"
)

// Entry is one audit log row.
type Entry struct {
	ID            string
	ActorID       string // "" = NULL (unknown/system)
	ActorUsername string
	Action        string
	TargetType    string
	TargetID      string
	Detail        string // short k=v pairs, never secrets
	IP            string
	CreatedAt     time.Time
}

// Filter selects list rows. Empty fields match everything.
type Filter struct {
	Actor  string // actor_id exact or actor_username substring
	Action string // exact or dotted prefix ("customer" matches "customer.*")
	From   string // RFC3339 inclusive, "" = no lower bound
	To     string // RFC3339 inclusive, "" = no upper bound
	Limit  int
	Offset int
}

// Store persists audit entries. Implemented in infrastructure.
type Store interface {
	Insert(ctx context.Context, e *Entry) error
	List(ctx context.Context, f Filter) (rows []Entry, total int64, err error)
	// UsernameForAccount resolves the actor username for display.
	// Best-effort: callers ignore its error and keep "".
	UsernameForAccount(ctx context.Context, accountID string) (string, error)
}
