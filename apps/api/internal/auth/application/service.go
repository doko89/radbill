// Package application hosts auth use cases.
package application

import (
	"context"
	"errors"
	"strings"

	"boilerplate/api/internal/auth/domain"
	"boilerplate/api/internal/shared/apperr"
)

// ErrInvalidCredentials is returned for unknown users and wrong passwords alike.
var ErrInvalidCredentials = errors.New("invalid username or password")

// Hasher abstracts password hashing so use cases stay crypto-agnostic.
type Hasher interface {
	Hash(password string) (string, error)
	Verify(hash, password string) bool
}

// UserDTO is the public shape of an authenticated user.
type UserDTO struct {
	ID        string `json:"id"`
	Username  string `json:"username"`
	AvatarURL string `json:"avatarUrl"`
}

// LoginInput carries raw login fields.
type LoginInput struct {
	Username string
	Password string
}

// Service implements auth use cases.
type Service struct {
	users  domain.Repository
	hasher Hasher
}

// NewService builds a Service.
func NewService(users domain.Repository, hasher Hasher) *Service {
	return &Service{users: users, hasher: hasher}
}

// Login verifies credentials and returns the authenticated user.
func (s *Service) Login(ctx context.Context, in LoginInput) (UserDTO, error) {
	username := strings.TrimSpace(in.Username)
	if username == "" {
		return UserDTO{}, apperr.Invalid("username", "required")
	}
	if in.Password == "" {
		return UserDTO{}, apperr.Invalid("password", "required")
	}
	u, err := s.users.FindByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			return UserDTO{}, ErrInvalidCredentials
		}
		return UserDTO{}, err
	}
	if !s.hasher.Verify(u.PasswordHash, in.Password) {
		return UserDTO{}, ErrInvalidCredentials
	}
	return toUserDTO(u), nil
}

// Me returns the public profile of an authenticated user id.
func (s *Service) Me(ctx context.Context, userID string) (UserDTO, error) {
	u, err := s.users.FindByID(ctx, userID)
	if err != nil {
		return UserDTO{}, err
	}
	return toUserDTO(u), nil
}

func toUserDTO(u *domain.User) UserDTO {
	return UserDTO{ID: u.ID, Username: u.Username, AvatarURL: u.AvatarURL}
}
