package infrastructure

import (
	"context"
	"database/sql"
)

// SetPaymentNote replaces the free-text note of a payment. It uses raw SQL
// because payments.note is added by an additive migration (ensure* in db.go)
// rather than the sqlc schema, keeping Fase 1 generated code untouched.
func (r *SQLRepository) SetPaymentNote(ctx context.Context, paymentID, note string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE payments SET note = $1 WHERE id = $2`, note, paymentID)
	return err
}

// GetPaymentNote returns the free-text note of a payment ("" when unset).
func (r *SQLRepository) GetPaymentNote(ctx context.Context, paymentID string) (string, error) {
	var note string
	err := r.db.QueryRowContext(ctx, `SELECT note FROM payments WHERE id = $1`, paymentID).Scan(&note)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", sql.ErrNoRows
		}
		return "", err
	}
	return note, nil
}
