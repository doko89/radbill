package domain

import "time"

// OIDCLink binds a local user to an external identity (issuer + subject).
// Stored in its own table so enabling OIDC never alters the users table.
type OIDCLink struct {
	UserID    string
	Issuer    string
	Sub       string
	CreatedAt time.Time
}
