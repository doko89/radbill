package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"boilerplate/api/internal/auth/domain"
	"boilerplate/api/internal/shared/id"
)

// OIDCClaims is the verified identity returned by the OIDC provider.
type OIDCClaims struct {
	Subject string
	Email   string
	Name    string
	Picture string
}

// OIDCProvider exchanges authorization codes for verified claims.
// Implemented in infrastructure (go-oidc); nil means OIDC is disabled.
type OIDCProvider interface {
	// AuthURL builds the provider authorization URL for the state/nonce pair.
	AuthURL(state, nonce string) string
	// Exchange trades code for claims, verifying signature, audience and nonce.
	Exchange(ctx context.Context, code, nonce string) (OIDCClaims, error)
	// Issuer returns the provider issuer URL (link scope).
	Issuer() string
}

// maxUsernameAttempts bounds the unique-username search for OIDC signups.
const maxUsernameAttempts = 10

// OIDCLogin finds the user linked to the OIDC identity, creating both the
// user and the link on first login. OIDC users have no password: password
// login always rejects them (empty hash never verifies).
func (s *Service) OIDCLogin(ctx context.Context, issuer string, claims OIDCClaims) (UserDTO, error) {
	if claims.Subject == "" {
		return UserDTO{}, errors.New("oidc: empty subject")
	}
	u, err := s.users.FindUserByOIDC(ctx, issuer, claims.Subject)
	if err == nil {
		// Refresh remote avatars (e.g. Google picture changes), but never
		// overwrite a locally uploaded /uploads/... avatar.
		if claims.Picture != "" && u.AvatarURL != claims.Picture &&
			(u.AvatarURL == "" || strings.HasPrefix(u.AvatarURL, "http")) {
			if err := s.users.SetAvatarURL(ctx, u.ID, claims.Picture); err != nil {
				return UserDTO{}, err
			}
			u.AvatarURL = claims.Picture
		}
		return toUserDTO(u), nil
	}
	if !errors.Is(err, domain.ErrOIDCNotFound) {
		return UserDTO{}, err
	}

	username, err := s.uniqueUsername(ctx, baseUsername(claims))
	if err != nil {
		return UserDTO{}, err
	}
	now := time.Now().UTC()
	user := &domain.User{ID: id.New(), Username: username, CreatedAt: now, AvatarURL: claims.Picture}
	if err := s.users.Create(ctx, user); err != nil {
		return UserDTO{}, err
	}
	link := &domain.OIDCLink{UserID: user.ID, Issuer: issuer, Sub: claims.Subject, CreatedAt: now}
	if err := s.users.SaveOIDCLink(ctx, link); err != nil {
		return UserDTO{}, err
	}
	return toUserDTO(user), nil
}

func baseUsername(claims OIDCClaims) string {
	if at := strings.Index(claims.Email, "@"); at > 0 {
		return strings.ToLower(claims.Email[:at])
	}
	if claims.Name != "" {
		return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(claims.Name), " ", "."))
	}
	return "user"
}

func (s *Service) uniqueUsername(ctx context.Context, base string) (string, error) {
	base = strings.TrimSpace(base)
	if base == "" {
		base = "user"
	}
	if len(base) > 32 {
		base = base[:32]
	}
	for attempt := range maxUsernameAttempts {
		candidate := base
		if attempt > 0 {
			candidate = fmt.Sprintf("%s-%d", base, attempt+1)
		}
		_, err := s.users.FindByUsername(ctx, candidate)
		if errors.Is(err, domain.ErrUserNotFound) {
			return candidate, nil
		}
		if err != nil {
			return "", err
		}
	}
	return "", fmt.Errorf("oidc: no free username for base %q", base)
}
