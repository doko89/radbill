// Package radius implements the radbill RADIUS server (auth + accounting).
//
// It is self-contained: stdlib plus layeh.com/radius, pgx/v5 and
// modernc.org/sqlite, and the stable boilerplate/api/internal/shared/id
// helper. It never imports the billing application packages, so radiusd
// keeps serving while billing is down.
package radius

import (
	"database/sql"
	"fmt"
)

// Account statuses stored in pppoe_accounts.status.
const (
	StatusActive   = "active"
	StatusIsolated = "isolated"
	StatusDisabled = "disabled"
)

// on_exhausted actions stored in packages.on_exhausted.
const (
	OnExhaustedReject   = "reject"
	OnExhaustedIsolate  = "isolate"
	OnExhaustedThrottle = "throttle"
)

// Settings keys in the settings table consumed by radiusd.
const (
	SettingInterimInterval   = "acct_interim_interval"
	SettingIsolirRateLimit   = "isolir_rate_limit"
	SettingIsolirAddressList = "isolir_address_list"
)

// Defaults applied when a settings row is absent.
const (
	DefaultInterimInterval   uint32 = 300
	DefaultIsolirRateLimit          = "128k/128k"
	DefaultIsolirAddressList        = "isolir"
)

// NAS is one row of the nas table.
type NAS struct {
	ID      string
	Name    string
	IP      string
	Secret  string
	CoAPort int
}

// Account is one cached row of pppoe_accounts. Only active/isolated rows
// are cached; disabled or unknown usernames always reject.
type Account struct {
	ID         string
	CustomerID string
	PackageID  string
	NASID      string // "" when NULL (NAS binding not enforced in Fase 1)
	Username   string
	Password   string // plaintext (product decision, PRD 5.2); never log
	Status     string
	ExpiresAt  string // "" when NULL, otherwise RFC3339
	StaticIP   string // "" when NULL
}

// Package is one cached row of the packages table.
type Package struct {
	ID                 string
	Name               string
	Price              int64
	Period             string
	RateLimit          string // "" when NULL
	AddressPool        string // "" when NULL
	ValiditySeconds    int64
	HasValidity        bool
	OnlineSecondsLimit int64
	HasOnlineLimit     bool
	QuotaBytes         int64
	HasQuota           bool
	QuotaReset         string
	FUPThresholdBytes  int64
	HasFUPThreshold    bool
	FUPRateLimit       string // "" when NULL
	OnExhausted        string
	IsPublic           bool
	AllowSelfService   bool
	UpgradeGroup       string // "" when NULL
}

func scanNAS(rows *sql.Rows) (NAS, error) {
	var n NAS
	var coa sql.NullInt64
	if err := rows.Scan(&n.ID, &n.Name, &n.IP, &n.Secret, &coa); err != nil {
		return NAS{}, fmt.Errorf("scan nas: %w", err)
	}
	if coa.Valid {
		n.CoAPort = int(coa.Int64)
	}
	return n, nil
}

func scanAccount(rows *sql.Rows) (Account, error) {
	var a Account
	var nasID, expiresAt, staticIP sql.NullString
	if err := rows.Scan(&a.ID, &a.CustomerID, &a.PackageID, &nasID, &a.Username,
		&a.Password, &a.Status, &expiresAt, &staticIP); err != nil {
		return Account{}, fmt.Errorf("scan pppoe_accounts: %w", err)
	}
	a.NASID = nasID.String
	a.ExpiresAt = expiresAt.String
	a.StaticIP = staticIP.String
	return a, nil
}

func nullInt64(v int64, ok bool) (int64, bool) { return v, ok }

func scanPackage(rows *sql.Rows) (Package, error) {
	var p Package
	var rateLimit, addressPool, fupRateLimit, upgradeGroup sql.NullString
	var validity, onlineLimit, quota, fupThreshold sql.NullInt64
	var isPublic, allowSelf sql.NullInt64
	if err := rows.Scan(&p.ID, &p.Name, &p.Price, &p.Period, &rateLimit,
		&addressPool, &validity, &onlineLimit, &quota, &p.QuotaReset,
		&fupThreshold, &fupRateLimit, &p.OnExhausted, &isPublic, &allowSelf,
		&upgradeGroup); err != nil {
		return Package{}, fmt.Errorf("scan packages: %w", err)
	}
	p.RateLimit = rateLimit.String
	p.AddressPool = addressPool.String
	p.ValiditySeconds, p.HasValidity = nullInt64(validity.Int64, validity.Valid)
	p.OnlineSecondsLimit, p.HasOnlineLimit = nullInt64(onlineLimit.Int64, onlineLimit.Valid)
	p.QuotaBytes, p.HasQuota = nullInt64(quota.Int64, quota.Valid)
	p.FUPThresholdBytes, p.HasFUPThreshold = nullInt64(fupThreshold.Int64, fupThreshold.Valid)
	p.FUPRateLimit = fupRateLimit.String
	p.IsPublic = isPublic.Valid && isPublic.Int64 != 0
	p.AllowSelfService = allowSelf.Valid && allowSelf.Int64 != 0
	p.UpgradeGroup = upgradeGroup.String
	return p, nil
}
