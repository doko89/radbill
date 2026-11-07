// Package infrastructure implements pppoe ports on SQL and network/coa.
package infrastructure

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"layeh.com/radius"

	authdomain "boilerplate/api/internal/jwtauth/domain"
	"boilerplate/api/internal/network/coa"
	"boilerplate/api/internal/network/notify"
	"boilerplate/api/internal/platform/db/sqlcgen"
	"boilerplate/api/internal/pppoe/domain"
	"boilerplate/api/internal/shared/id"
)

// SQLRepository implements domain.Repository on sqlc queries.
type SQLRepository struct {
	q      *sqlcgen.Queries
	events *notify.Notifier
}

// NewSQLRepository builds a SQLRepository.
func NewSQLRepository(q *sqlcgen.Queries, events *notify.Notifier) *SQLRepository {
	return &SQLRepository{q: q, events: events}
}

// Create inserts a PPPoE account and emits a pppoe/upsert event.
func (r *SQLRepository) Create(ctx context.Context, p *domain.PPPoE) error {
	_, err := r.q.CreateBillingPPPoE(ctx, sqlcgen.CreateBillingPPPoEParams{
		ID:         p.ID,
		CustomerID: p.CustomerID,
		PackageID:  p.PackageID,
		NasID:      nullString(p.NASID),
		Username:   p.Username,
		Password:   p.Password,
		Status:     p.Status,
		ExpiresAt:  nullTime(p.ExpiresAt),
		StaticIp:   nullString(p.StaticIP),
		CreatedAt:  p.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:  p.UpdatedAt.UTC().Format(time.RFC3339),
	})
	if err != nil {
		if isUniqueViolation(err) {
			return domain.ErrUsernameTaken
		}
		return err
	}
	r.events.Emit(ctx, notify.TypePPPoE, notify.ActionUpsert, p.ID)
	return nil
}

// Get returns one account or domain.ErrPPPoENotFound.
func (r *SQLRepository) Get(ctx context.Context, pid string) (*domain.PPPoE, error) {
	row, err := r.q.GetBillingPPPoE(ctx, pid)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrPPPoENotFound
		}
		return nil, err
	}
	return toPPPoE(row)
}

// GetByUsername returns one account or domain.ErrPPPoENotFound.
func (r *SQLRepository) GetByUsername(ctx context.Context, username string) (*domain.PPPoE, error) {
	row, err := r.q.GetBillingPPPoEByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrPPPoENotFound
		}
		return nil, err
	}
	return toPPPoE(row)
}

// List returns a page of accounts, newest first.
func (r *SQLRepository) List(ctx context.Context, limit, offset int) ([]*domain.PPPoE, error) {
	rows, err := r.q.ListBillingPPPoE(ctx, sqlcgen.ListBillingPPPoEParams{
		Limit:  int32(limit),
		Offset: int32(offset),
	})
	if err != nil {
		return nil, err
	}
	out := make([]*domain.PPPoE, 0, len(rows))
	for _, row := range rows {
		p, err := toPPPoE(row)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// Count returns the number of PPPoE accounts.
func (r *SQLRepository) Count(ctx context.Context) (int64, error) {
	return r.q.CountBillingPPPoE(ctx)
}

// Update persists an account and emits a pppoe/upsert event.
func (r *SQLRepository) Update(ctx context.Context, p *domain.PPPoE) error {
	_, err := r.q.UpdateBillingPPPoE(ctx, sqlcgen.UpdateBillingPPPoEParams{
		ID:         p.ID,
		CustomerID: p.CustomerID,
		PackageID:  p.PackageID,
		NasID:      nullString(p.NASID),
		Username:   p.Username,
		Password:   p.Password,
		Status:     p.Status,
		ExpiresAt:  nullTime(p.ExpiresAt),
		StaticIp:   nullString(p.StaticIP),
		UpdatedAt:  p.UpdatedAt.UTC().Format(time.RFC3339),
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.ErrPPPoENotFound
		}
		if isUniqueViolation(err) {
			return domain.ErrUsernameTaken
		}
		return err
	}
	r.events.Emit(ctx, notify.TypePPPoE, notify.ActionUpsert, p.ID)
	return nil
}

// Delete removes an account and emits a pppoe/delete event.
func (r *SQLRepository) Delete(ctx context.Context, pid string) error {
	if err := r.q.DeleteBillingPPPoE(ctx, pid); err != nil {
		if isForeignKeyViolation(err) {
			return domain.ErrPPPoEInUse
		}
		return err
	}
	r.events.Emit(ctx, notify.TypePPPoE, notify.ActionDelete, pid)
	return nil
}

// ResetQuota clears usage counters for the account.
func (r *SQLRepository) ResetQuota(ctx context.Context, pid string) error {
	return r.q.DeleteBillingUsageByAccount(ctx, pid)
}

// CustomerExists reports whether the customer exists.
func (r *SQLRepository) CustomerExists(ctx context.Context, cid string) bool {
	_, err := r.q.GetBillingCustomer(ctx, cid)
	return err == nil
}

// PackageExists reports whether the package exists.
func (r *SQLRepository) PackageExists(ctx context.Context, pid string) bool {
	_, err := r.q.GetBillingPackage(ctx, pid)
	return err == nil
}

// NASExists reports whether the NAS exists.
func (r *SQLRepository) NASExists(ctx context.Context, nasID string) bool {
	_, err := r.q.GetBillingNAS(ctx, nasID)
	return err == nil
}

// SQLPortalStore implements domain.PortalStore on sqlc transactions.
type SQLPortalStore struct {
	db *sql.DB
	q  *sqlcgen.Queries
}

// NewSQLPortalStore builds a SQLPortalStore.
func NewSQLPortalStore(db *sql.DB, q *sqlcgen.Queries) *SQLPortalStore {
	return &SQLPortalStore{db: db, q: q}
}

// CreatePortalAccount creates a role=user account and links the customer.
func (s *SQLPortalStore) CreatePortalAccount(ctx context.Context, customerID, username, passwordHash string, now time.Time) (string, error) {
	cust, err := s.q.GetBillingCustomer(ctx, customerID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", domain.ErrUnknownCustomer
		}
		return "", err
	}
	if cust.AccountID.Valid && cust.AccountID.String != "" {
		return "", domain.ErrPortalExists
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	qtx := s.q.WithTx(tx)
	stamp := now.UTC().Format(time.RFC3339)
	accountID := id.New()
	if _, err := qtx.CreateBillingAccount(ctx, sqlcgen.CreateBillingAccountParams{
		ID:           accountID,
		Username:     username,
		PasswordHash: passwordHash,
		Role:         authdomain.RoleUser,
		Status:       authdomain.StatusActive,
		CreatedAt:    stamp,
		UpdatedAt:    stamp,
	}); err != nil {
		if isUniqueViolation(err) {
			return "", authdomain.ErrUsernameTaken
		}
		return "", err
	}
	if err := qtx.SetBillingCustomerAccount(ctx, sqlcgen.SetBillingCustomerAccountParams{
		ID:        customerID,
		AccountID: sql.NullString{String: accountID, Valid: true},
		UpdatedAt: stamp,
	}); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return accountID, nil
}

// ResetPortalPassword replaces the portal password and revokes sessions.
func (s *SQLPortalStore) ResetPortalPassword(ctx context.Context, customerID, passwordHash string, now time.Time) (string, error) {
	cust, err := s.q.GetBillingCustomer(ctx, customerID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", domain.ErrUnknownCustomer
		}
		return "", err
	}
	if !cust.AccountID.Valid || cust.AccountID.String == "" {
		return "", domain.ErrNoPortalAccount
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	qtx := s.q.WithTx(tx)
	stamp := now.UTC().Format(time.RFC3339)
	acct, err := qtx.GetBillingAccountByID(ctx, cust.AccountID.String)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", domain.ErrNoPortalAccount
		}
		return "", err
	}
	if err := qtx.UpdateBillingAccountPassword(ctx, sqlcgen.UpdateBillingAccountPasswordParams{
		ID:           acct.ID,
		PasswordHash: passwordHash,
		UpdatedAt:    stamp,
	}); err != nil {
		return "", err
	}
	if err := qtx.RevokeBillingRefreshTokensForAccount(ctx, sqlcgen.RevokeBillingRefreshTokensForAccountParams{
		AccountID: acct.ID,
		RevokedAt: sql.NullString{String: stamp, Valid: true},
	}); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return acct.Username, nil
}

// CoADisconnecter implements domain.Disconnecter via network/coa.
type CoADisconnecter struct {
	q      *sqlcgen.Queries
	sender *coa.Sender
}

// NewCoADisconnecter builds a CoADisconnecter.
func NewCoADisconnecter(q *sqlcgen.Queries, sender *coa.Sender) *CoADisconnecter {
	return &CoADisconnecter{q: q, sender: sender}
}

// DisconnectAll drops every online session of the PPPoE account,
// continuing past per-session failures. It returns ACKed and total counts.
func (d *CoADisconnecter) DisconnectAll(ctx context.Context, pppoeID string) (int, int, error) {
	acct, err := d.q.GetBillingPPPoE(ctx, pppoeID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, 0, domain.ErrPPPoENotFound
		}
		return 0, 0, err
	}
	sessions, err := d.q.ListBillingSessionsByPPPoE(ctx, sqlcgen.ListBillingSessionsByPPPoEParams{
		PppoeAccountID: pppoeID,
		Limit:          1000,
		Offset:         0,
	})
	if err != nil {
		return 0, 0, err
	}
	disconnected, total := 0, 0
	for _, s := range sessions {
		if s.StoppedAt.Valid {
			continue
		}
		total++
		nas, err := d.q.GetBillingNAS(ctx, s.NasID)
		if err != nil {
			log.Printf("pppoe disconnect: nas %s: %v", s.NasID, err)
			continue
		}
		code, err := d.sender.Disconnect(ctx,
			coa.NAS{Address: nas.Ip, Port: int(nas.CoaPort), Secret: nas.Secret},
			acct.Username, s.SessionID)
		if err != nil {
			log.Printf("pppoe disconnect: session %s: %v", s.SessionID, err)
			continue
		}
		if code == radius.CodeDisconnectACK {
			disconnected++
		}
	}
	return disconnected, total, nil
}

func toPPPoE(row sqlcgen.PppoeAccount) (*domain.PPPoE, error) {
	created, err := time.Parse(time.RFC3339, row.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse pppoe created_at: %w", err)
	}
	updated, err := time.Parse(time.RFC3339, row.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse pppoe updated_at: %w", err)
	}
	p := &domain.PPPoE{
		ID:         row.ID,
		CustomerID: row.CustomerID,
		PackageID:  row.PackageID,
		NASID:      row.NasID.String,
		Username:   row.Username,
		Password:   row.Password,
		Status:     row.Status,
		StaticIP:   row.StaticIp.String,
		CreatedAt:  created,
		UpdatedAt:  updated,
	}
	if row.ExpiresAt.Valid {
		exp, err := time.Parse(time.RFC3339, row.ExpiresAt.String)
		if err != nil {
			return nil, fmt.Errorf("parse pppoe expires_at: %w", err)
		}
		p.ExpiresAt = &exp
	}
	return p, nil
}

func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

func nullTime(t *time.Time) sql.NullString {
	if t == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: t.UTC().Format(time.RFC3339), Valid: true}
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") ||
		strings.Contains(msg, "unique constraint") ||
		strings.Contains(msg, "duplicate key")
}

func isForeignKeyViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "FOREIGN KEY constraint failed") ||
		strings.Contains(msg, "foreign key constraint") ||
		strings.Contains(msg, "violates foreign key")
}
