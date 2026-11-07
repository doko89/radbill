package application

import (
	"context"

	authdomain "boilerplate/api/internal/jwtauth/domain"
	"boilerplate/api/internal/shared/apperr"
)

// ChangePassword verifies the current password, sets a new one, and revokes
// every refresh token of the account (G2, ADITIF).
func (s *Service) ChangePassword(ctx context.Context, accountID, currentPassword, newPassword string) error {
	if currentPassword == "" {
		return apperr.Invalid("current_password", "required")
	}
	if len(newPassword) < MinPasswordLen {
		return apperr.Invalid("new_password", "minimum 8 characters")
	}
	now := s.now().UTC()
	acct, err := s.accounts.GetByID(ctx, accountID)
	if err != nil {
		return err
	}
	if acct.Disabled() {
		return authdomain.ErrAccountDisabled
	}
	if !s.hasher.Verify(currentPassword, acct.PasswordHash) {
		return authdomain.ErrInvalidCredentials
	}
	hash, err := s.hasher.Hash(newPassword)
	if err != nil {
		return err
	}
	if err := s.accounts.UpdatePassword(ctx, acct.ID, hash, now); err != nil {
		return err
	}
	return s.refresh.RevokeAllForAccount(ctx, acct.ID, now)
}
