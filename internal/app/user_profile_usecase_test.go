package app

import (
	"context"
	"errors"
	"testing"

	"github.com/hamp/booking-sport/internal/domain"
)

type fakeProfileRepo struct {
	profiles map[string]string
}

func (f *fakeProfileRepo) FindByAuth0ID(_ context.Context, id string) (*domain.UserProfile, error) {
	phone, ok := f.profiles[id]
	if !ok {
		return nil, nil
	}
	return &domain.UserProfile{Auth0ID: id, Phone: phone}, nil
}

func (f *fakeProfileRepo) UpsertPhone(_ context.Context, id, phone string) error {
	f.profiles[id] = phone
	return nil
}

func TestNormalizePhone(t *testing.T) {
	cases := map[string]string{
		"+56 9 1234 5678": "+56912345678",
		"912345678":       "912345678",
		"(9) 1234-5678":   "912345678",
	}
	for in, want := range cases {
		got, err := NormalizePhone(in)
		if err != nil || got != want {
			t.Errorf("NormalizePhone(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "abc", "123", "+56 9 12ab 5678"} {
		if _, err := NormalizePhone(bad); !errors.Is(err, ErrInvalidPhone) {
			t.Errorf("NormalizePhone(%q) debería fallar", bad)
		}
	}
}

func TestUserProfilePhoneIsScopedPerUser(t *testing.T) {
	uc := NewUserProfileUseCase(&fakeProfileRepo{profiles: map[string]string{}})
	ctx := context.Background()

	if _, err := uc.SavePhone(ctx, "auth0|a", "+56 9 1111 1111"); err != nil {
		t.Fatal(err)
	}
	if got, _ := uc.GetPhone(ctx, "auth0|a"); got != "+56911111111" {
		t.Errorf("usuario a: %q", got)
	}
	if got, _ := uc.GetPhone(ctx, "auth0|b"); got != "" {
		t.Errorf("usuario b no debería tener teléfono, got %q", got)
	}
}
