package domain

import "context"

// Repository persists users. Implemented in infrastructure.
type Repository interface {
	FindByUsername(ctx context.Context, username string) (*User, error)
	FindByID(ctx context.Context, id string) (*User, error)
	Create(ctx context.Context, u *User) error
	Count(ctx context.Context) (int64, error)
	FindUserByOIDC(ctx context.Context, issuer, sub string) (*User, error)
	SaveOIDCLink(ctx context.Context, link *OIDCLink) error
	SetAvatarURL(ctx context.Context, userID, url string) error
}
