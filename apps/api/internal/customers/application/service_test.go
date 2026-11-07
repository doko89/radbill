package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"boilerplate/api/internal/customers/domain"
	authdomain "boilerplate/api/internal/jwtauth/domain"
)

type stubRepo struct {
	customers map[string]*domain.Customer
}

func (s *stubRepo) Create(_ context.Context, c *domain.Customer) error {
	s.customers[c.ID] = c
	return nil
}

func (s *stubRepo) Get(_ context.Context, id string) (*domain.Customer, error) {
	c, ok := s.customers[id]
	if !ok {
		return nil, domain.ErrCustomerNotFound
	}
	return c, nil
}

func (s *stubRepo) List(_ context.Context, _, _ int) ([]*domain.Customer, error) { return nil, nil }
func (s *stubRepo) Count(_ context.Context) (int64, error)                       { return 0, nil }
func (s *stubRepo) Update(_ context.Context, c *domain.Customer) error {
	s.customers[c.ID] = c
	return nil
}
func (s *stubRepo) Delete(_ context.Context, id string) error {
	delete(s.customers, id)
	return nil
}

type stubPortal struct {
	createErr error
	resetErr  error
	username  string
}

func (s *stubPortal) CreatePortalAccount(_ context.Context, _, username, _ string, _ time.Time) (string, error) {
	if s.createErr != nil {
		return "", s.createErr
	}
	s.username = username
	return "acc-1", nil
}

func (s *stubPortal) ResetPortalPassword(_ context.Context, _ string, _ string, _ time.Time) (string, error) {
	if s.resetErr != nil {
		return "", s.resetErr
	}
	return s.username, nil
}

type stubHasher struct{}

func (stubHasher) Hash(password string) (string, error) { return "hash:" + password, nil }
func (stubHasher) Verify(_, _ string) bool              { return true }

func newTestService(repo *stubRepo, portal *stubPortal) *Service {
	svc := NewService(repo)
	svc.SetPortalStore(portal, stubHasher{})
	return svc
}

func TestCreatePortalAccount(t *testing.T) {
	ctx := context.Background()
	repo := &stubRepo{customers: map[string]*domain.Customer{
		"c1": {ID: "c1", Name: "C1"},
		"c2": {ID: "c2", Name: "C2", AccountID: "acc-x"},
	}}
	portal := &stubPortal{}
	svc := newTestService(repo, portal)

	dto, err := svc.CreatePortalAccount(ctx, "c1", "user1", "secret123")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if dto.AccountID != "acc-1" || dto.Username != "user1" || dto.Password != "secret123" {
		t.Fatalf("DTO = %+v", dto)
	}

	if _, err := svc.CreatePortalAccount(ctx, "ghost", "u", "secret123"); !errors.Is(err, domain.ErrCustomerNotFound) {
		t.Fatalf("unknown customer err = %v", err)
	}
	if _, err := svc.CreatePortalAccount(ctx, "c1", "", "secret123"); err == nil {
		t.Fatal("expected username required error")
	}
	if _, err := svc.CreatePortalAccount(ctx, "c1", "u", "short"); err == nil {
		t.Fatal("expected short password error")
	}
	if _, err := svc.CreatePortalAccount(ctx, "c2", "u", "secret123"); !errors.Is(err, domain.ErrPortalExists) {
		t.Fatalf("existing portal err = %v", err)
	}

	// Empty password generates a random one (12 chars from the id helper).
	dto, err = svc.CreatePortalAccount(ctx, "c1", "user2", "")
	if err != nil {
		t.Fatalf("Create random: %v", err)
	}
	if len(dto.Password) != 12 {
		t.Fatalf("random password len = %d", len(dto.Password))
	}

	// Username-taken from the store translates to the customers error.
	repo2 := &stubRepo{customers: map[string]*domain.Customer{"c9": {ID: "c9", Name: "C9"}}}
	taken := newTestService(repo2, &stubPortal{createErr: authdomain.ErrUsernameTaken})
	if _, err := taken.CreatePortalAccount(ctx, "c9", "dup", "secret123"); !errors.Is(err, domain.ErrPortalUsernameTaken) {
		t.Fatalf("taken err = %v", err)
	}
}

func TestResetPortalAccount(t *testing.T) {
	ctx := context.Background()
	repo := &stubRepo{customers: map[string]*domain.Customer{
		"c1": {ID: "c1", Name: "C1", AccountID: "acc-1"},
		"c2": {ID: "c2", Name: "C2"},
	}}
	portal := &stubPortal{username: "user1"}
	svc := newTestService(repo, portal)

	dto, err := svc.ResetPortalAccount(ctx, "c1", "baru45678")
	if err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if dto.Username != "user1" || dto.Password != "baru45678" {
		t.Fatalf("DTO = %+v", dto)
	}

	dto, err = svc.ResetPortalAccount(ctx, "c1", "")
	if err != nil {
		t.Fatalf("Reset random: %v", err)
	}
	if len(dto.Password) != 12 {
		t.Fatalf("random password len = %d", len(dto.Password))
	}

	if _, err := svc.ResetPortalAccount(ctx, "c2", "baru45678"); !errors.Is(err, domain.ErrNoPortalAccount) {
		t.Fatalf("no portal err = %v", err)
	}
	if _, err := svc.ResetPortalAccount(ctx, "ghost", "baru45678"); !errors.Is(err, domain.ErrCustomerNotFound) {
		t.Fatalf("unknown customer err = %v", err)
	}
	if _, err := svc.ResetPortalAccount(ctx, "c1", "short"); err == nil {
		t.Fatal("expected short password error")
	}
}
