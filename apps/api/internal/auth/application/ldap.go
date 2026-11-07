package application

import (
	"context"
	"errors"
	"strings"
	"time"

	"boilerplate/api/internal/auth/domain"
	"boilerplate/api/internal/shared/apperr"
	"boilerplate/api/internal/shared/id"
)

// ErrLoginConflict is returned when an external login (LDAP) hits a username
// owned by a local password account. Linking is deliberately refused: the
// user must keep using password login.
var ErrLoginConflict = errors.New("this username uses password login")

// LDAPIdentity is the directory identity verified by bind.
type LDAPIdentity struct {
	Username string
	Email    string
	Name     string
}

// LDAPAuthenticator verifies directory credentials. Implemented in
// infrastructure (go-ldap); nil means LDAP is disabled.
type LDAPAuthenticator interface {
	Authenticate(ctx context.Context, username, password string) (LDAPIdentity, error)
}

// LDAPLogin verifies directory credentials, then finds-or-creates the local
// user. LDAP usernames are stored lowercased (directory names are
// case-insensitive). LDAP users carry an empty password hash, so password
// login always rejects them — the two methods can never take each other over.
func (s *Service) LDAPLogin(ctx context.Context, a LDAPAuthenticator, username, password string) (UserDTO, error) {
	name := strings.ToLower(strings.TrimSpace(username))
	if name == "" {
		return UserDTO{}, apperr.Invalid("username", "required")
	}
	if password == "" {
		return UserDTO{}, apperr.Invalid("password", "required")
	}
	ident, err := a.Authenticate(ctx, name, password)
	if err != nil {
		return UserDTO{}, err
	}
	u, err := s.users.FindByUsername(ctx, ident.Username)
	if err == nil {
		if u.PasswordHash != "" {
			return UserDTO{}, ErrLoginConflict
		}
		return toUserDTO(u), nil
	}
	if !errors.Is(err, domain.ErrUserNotFound) {
		return UserDTO{}, err
	}
	user := &domain.User{ID: id.New(), Username: ident.Username, CreatedAt: time.Now().UTC()}
	if err := s.users.Create(ctx, user); err != nil {
		return UserDTO{}, err
	}
	return toUserDTO(user), nil
}
