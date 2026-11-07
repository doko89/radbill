// Package apperr holds shared application error types.
package apperr

// FieldError describes a single invalid input field.
type FieldError struct {
	Field   string
	Message string
}

func (e *FieldError) Error() string { return e.Field + ": " + e.Message }

// Invalid builds a *FieldError for one rejected field.
func Invalid(field, msg string) *FieldError {
	return &FieldError{Field: field, Message: msg}
}
