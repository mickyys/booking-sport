package app

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/hamp/booking-sport/internal/domain"
)

var ErrInvalidPhone = errors.New("invalid phone number")

var phoneSeparators = strings.NewReplacer(" ", "", ".", "", "-", "", "(", "", ")", "")

var phonePattern = regexp.MustCompile(`^\+?\d{8,15}$`)

type UserProfileRepository interface {
	FindByAuth0ID(ctx context.Context, auth0ID string) (*domain.UserProfile, error)
	UpsertPhone(ctx context.Context, auth0ID, phone string) error
}

type UserProfileUseCase struct {
	repo UserProfileRepository
}

func NewUserProfileUseCase(repo UserProfileRepository) *UserProfileUseCase {
	return &UserProfileUseCase{repo: repo}
}

// NormalizePhone quita separadores y valida el formato. Devuelve ErrInvalidPhone
// si el resultado no es un número telefónico plausible.
func NormalizePhone(phone string) (string, error) {
	normalized := phoneSeparators.Replace(strings.TrimSpace(phone))
	if !phonePattern.MatchString(normalized) {
		return "", ErrInvalidPhone
	}
	return normalized, nil
}

// GetPhone devuelve el teléfono guardado del usuario o "" si no existe.
func (uc *UserProfileUseCase) GetPhone(ctx context.Context, auth0ID string) (string, error) {
	profile, err := uc.repo.FindByAuth0ID(ctx, auth0ID)
	if err != nil || profile == nil {
		return "", err
	}
	return profile.Phone, nil
}

func (uc *UserProfileUseCase) SavePhone(ctx context.Context, auth0ID, phone string) (string, error) {
	normalized, err := NormalizePhone(phone)
	if err != nil {
		return "", err
	}
	if err := uc.repo.UpsertPhone(ctx, auth0ID, normalized); err != nil {
		return "", err
	}
	return normalized, nil
}
