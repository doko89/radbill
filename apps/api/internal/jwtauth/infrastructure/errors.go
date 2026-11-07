package infrastructure

import "strings"

// isUniqueViolation reports whether err looks like a unique-constraint
// violation on either sqlite ("UNIQUE constraint failed") or postgres
// ("duplicate key value violates unique constraint").
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") ||
		strings.Contains(msg, "unique constraint") ||
		strings.Contains(msg, "duplicate key")
}
