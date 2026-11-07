package application

import (
	"context"
	"errors"
	"testing"

	"boilerplate/api/internal/auth/domain"
	"boilerplate/api/internal/shared/apperr"
)

var (
	_ domain.Repository = (*stubRepo)(nil)
	_ LDAPAuthenticator = (*stubLDAP)(nil)
)

type stubRepo struct {
	users map[string]*domain.User
	links map[string]*domain.OIDCLink
}

func newStubRepo() *stubRepo {
	return &stubRepo{users: map[string]*domain.User{}, links: map[string]*domain.OIDCLink{}}
}

func (s *stubRepo) FindByUsername(_ context.Context, username string) (*domain.User, error) {
	u, ok := s.users[username]
	if !ok {
		return nil, domain.ErrUserNotFound
	}
	return u, nil
}

func (s *stubRepo) FindByID(_ context.Context, id string) (*domain.User, error) {
	for _, u := range s.users {
		if u.ID == id {
			return u, nil
		}
	}
	return nil, domain.ErrUserNotFound
}

func (s *stubRepo) Create(_ context.Context, u *domain.User) error {
	if _, ok := s.users[u.Username]; ok {
		return domain.ErrUsernameTaken
	}
	s.users[u.Username] = u
	return nil
}

func (s *stubRepo) Count(_ context.Context) (int64, error) { return int64(len(s.users)), nil }

func (s *stubRepo) FindUserByOIDC(_ context.Context, issuer, sub string) (*domain.User, error) {
	l, ok := s.links[issuer+"\x00"+sub]
	if !ok {
		return nil, domain.ErrOIDCNotFound
	}
	return s.FindByID(context.Background(), l.UserID)
}

func (s *stubRepo) SaveOIDCLink(_ context.Context, link *domain.OIDCLink) error {
	s.links[link.Issuer+"\x00"+link.Sub] = link
	return nil
}

func (s *stubRepo) SetAvatarURL(_ context.Context, userID, url string) error {
	for _, u := range s.users {
		if u.ID == userID {
			u.AvatarURL = url
			return nil
		}
	}
	return domain.ErrUserNotFound
}

type stubLDAP struct {
	creds map[string]string
	calls int
}

func (s *stubLDAP) Authenticate(_ context.Context, username, password string) (LDAPIdentity, error) {
	s.calls++
	if pw, ok := s.creds[username]; ok && pw == password {
		return LDAPIdentity{Username: username}, nil
	}
	return LDAPIdentity{}, ErrInvalidCredentials
}

func TestLDAPLoginCreatesUser(t *testing.T) {
	repo := newStubRepo()
	ldap := &stubLDAP{creds: map[string]string{"joko": "secret"}}
	svc := NewService(repo, nil)

	dto, err := svc.LDAPLogin(context.Background(), ldap, "  Joko ", "secret")
	if err != nil {
		t.Fatalf("LDAPLogin: %v", err)
	}
	if dto.Username != "joko" {
		t.Fatalf("expected lowercased joko, got %q", dto.Username)
	}
	stored, err := repo.FindByUsername(context.Background(), "joko")
	if err != nil {
		t.Fatalf("user not persisted: %v", err)
	}
	if stored.PasswordHash != "" {
		t.Fatal("LDAP user must carry an empty password hash")
	}
}

func TestLDAPLoginExistingExternal(t *testing.T) {
	repo := newStubRepo()
	ldap := &stubLDAP{creds: map[string]string{"sri": "pw"}}
	svc := NewService(repo, nil)

	first, err := svc.LDAPLogin(context.Background(), ldap, "sri", "pw")
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.LDAPLogin(context.Background(), ldap, "sri", "pw")
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("expected same user, got %q then %q", first.ID, second.ID)
	}
	if n, _ := repo.Count(context.Background()); n != 1 {
		t.Fatalf("expected 1 user, got %d", n)
	}
}

func TestLDAPLoginConflictWithPasswordUser(t *testing.T) {
	repo := newStubRepo()
	repo.users["admin"] = &domain.User{ID: "u1", Username: "admin", PasswordHash: "bcrypt-hash"}
	ldap := &stubLDAP{creds: map[string]string{"admin": "dir-pass"}}
	svc := NewService(repo, nil)

	_, err := svc.LDAPLogin(context.Background(), ldap, "admin", "dir-pass")
	if !errors.Is(err, ErrLoginConflict) {
		t.Fatalf("expected ErrLoginConflict, got %v", err)
	}
}

func TestLDAPLoginBadCredentials(t *testing.T) {
	repo := newStubRepo()
	ldap := &stubLDAP{creds: map[string]string{"joko": "secret"}}
	svc := NewService(repo, nil)

	_, err := svc.LDAPLogin(context.Background(), ldap, "joko", "wrong")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
	if n, _ := repo.Count(context.Background()); n != 0 {
		t.Fatal("failed login must not create a user")
	}
}

func TestLDAPLoginEmptyInput(t *testing.T) {
	repo := newStubRepo()
	ldap := &stubLDAP{creds: map[string]string{"joko": "secret"}}
	svc := NewService(repo, nil)

	if _, err := svc.LDAPLogin(context.Background(), ldap, "joko", ""); err == nil {
		t.Fatal("expected error for empty password")
	} else if _, ok := err.(*apperr.FieldError); !ok {
		t.Fatalf("expected *FieldError, got %T", err)
	}
	if _, err := svc.LDAPLogin(context.Background(), ldap, "  ", "secret"); err == nil {
		t.Fatal("expected error for blank username")
	}
	if ldap.calls != 0 {
		t.Fatalf("directory must not be contacted on invalid input, got %d calls", ldap.calls)
	}
}
