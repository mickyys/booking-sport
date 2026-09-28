package app

import (
	"context"
	"fmt"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type SpaceRepository interface {
	Create(ctx context.Context, space *domain.Space) error
	FindByID(ctx context.Context, id primitive.ObjectID) (*domain.Space, error)
	FindByCenterID(ctx context.Context, centerID primitive.ObjectID) ([]domain.Space, error)
	FindByCenterAndCategory(ctx context.Context, centerID primitive.ObjectID, category domain.SpaceCategory) ([]domain.Space, error)
	Update(ctx context.Context, space *domain.Space) error
	Delete(ctx context.Context, id primitive.ObjectID) error
	UpdateScheduleSlot(ctx context.Context, id primitive.ObjectID, slot domain.CourtSchedule) error
	SyncPartialPaymentSlots(ctx context.Context, centerID primitive.ObjectID, partialPaymentEnabled bool) (int64, error)
}

type SpaceUseCase struct {
	repo            SpaceRepository
	sportCenterRepo SportCenterRepository
}

func NewSpaceUseCase(repo SpaceRepository, sportCenterRepo SportCenterRepository) *SpaceUseCase {
	return &SpaceUseCase{repo: repo, sportCenterRepo: sportCenterRepo}
}

func (uc *SpaceUseCase) ListByUser(ctx context.Context, userID string) ([]domain.Space, error) {
	centers, err := uc.sportCenterRepo.FindByUserID(ctx, userID)
	if err != nil || len(centers) == 0 {
		return []domain.Space{}, nil
	}
	return uc.repo.FindByCenterID(ctx, centers[0].ID)
}

func (uc *SpaceUseCase) ListFacilitiesByUser(ctx context.Context, userID string) ([]domain.Space, error) {
	centers, err := uc.sportCenterRepo.FindByUserID(ctx, userID)
	if err != nil || len(centers) == 0 {
		return []domain.Space{}, nil
	}
	return uc.repo.FindByCenterAndCategory(ctx, centers[0].ID, domain.SpaceCategoryQuincho)
}

func (uc *SpaceUseCase) Create(ctx context.Context, space *domain.Space, userID string) error {
	if !space.Category.IsValid() {
		return fmt.Errorf("invalid category %q", space.Category)
	}
	// Para facilities, `type` se deriva de la categoría para compatibilidad pública.
	if space.Category.IsFacility() {
		space.Type = domain.FacilityType(space.Category)
	}
	if space.Name == "" {
		return fmt.Errorf("name is required")
	}

	center, err := uc.sportCenterRepo.FindByID(ctx, space.SportCenterID)
	if err != nil {
		return err
	}
	if !centerOwnsUser(center, userID) {
		return fmt.Errorf("user is not authorized to create space for this sport center")
	}

	space.Enabled = true
	space.CreatedAt = time.Now()
	space.UpdatedAt = time.Now()
	return uc.repo.Create(ctx, space)
}

func (uc *SpaceUseCase) Update(ctx context.Context, id primitive.ObjectID, updated *domain.Space, userID string) error {
	if !updated.Category.IsValid() {
		return fmt.Errorf("invalid category %q", updated.Category)
	}

	existing, err := uc.repo.FindByID(ctx, id)
	if err != nil {
		return err
	}

	center, err := uc.sportCenterRepo.FindByID(ctx, existing.SportCenterID)
	if err != nil {
		return err
	}
	if !centerOwnsUser(center, userID) {
		return fmt.Errorf("user is not authorized to update space for this sport center")
	}

	existing.Category = updated.Category
	if updated.Category.IsFacility() {
		existing.Type = domain.FacilityType(updated.Category)
	} else {
		existing.Type = ""
	}

	existing.Name = updated.Name
	existing.Description = updated.Description
	existing.ImageURL = updated.ImageURL
	existing.YPosition = updated.YPosition
	existing.Capacity = updated.Capacity
	existing.Area = updated.Area
	existing.Features = updated.Features
	existing.PriceAllDay = updated.PriceAllDay
	existing.PriceHalfDay = updated.PriceHalfDay
	existing.OpenHour = updated.OpenHour
	existing.CloseHour = updated.CloseHour
	existing.PricePerDay = updated.PricePerDay
	existing.PricePerMonth = updated.PricePerMonth
	existing.MinMonths = updated.MinMonths
	existing.MaxMonths = updated.MaxMonths
	existing.AvailableMonths = updated.AvailableMonths
	existing.DepositMonths = updated.DepositMonths

	// Limpia campos que no aplican a la nueva categoría para evitar datos
	// huérfanos de la categoría anterior.
	switch updated.Category {
	case domain.SpaceCategoryCancha:
		existing.Capacity = 0
		existing.Area = ""
		existing.Features = nil
		existing.PriceAllDay = 0
		existing.PriceHalfDay = 0
		existing.OpenHour = 0
		existing.CloseHour = 0
		existing.PricePerDay = 0
		existing.PricePerMonth = 0
		existing.MinMonths = 0
		existing.MaxMonths = 0
		existing.AvailableMonths = nil
		existing.DepositMonths = 0
	case domain.SpaceCategoryQuincho:
		existing.PricePerDay = 0
		existing.PricePerMonth = 0
		existing.MinMonths = 0
		existing.MaxMonths = 0
		existing.AvailableMonths = nil
		existing.DepositMonths = 0
	default: // sede / piscina
		existing.PriceAllDay = 0
		existing.PriceHalfDay = 0
		existing.OpenHour = 0
		existing.CloseHour = 0
	}

	existing.UpdatedAt = time.Now()

	return uc.repo.Update(ctx, existing)
}

func (uc *SpaceUseCase) Delete(ctx context.Context, id primitive.ObjectID, userID string) error {
	existing, err := uc.repo.FindByID(ctx, id)
	if err != nil {
		return err
	}

	center, err := uc.sportCenterRepo.FindByID(ctx, existing.SportCenterID)
	if err != nil {
		return err
	}
	if !centerOwnsUser(center, userID) {
		return fmt.Errorf("user is not authorized to delete space for this sport center")
	}

	return uc.repo.Delete(ctx, id)
}

func centerOwnsUser(center *domain.SportCenter, userID string) bool {
	for _, u := range center.Users {
		if u == userID {
			return true
		}
	}
	return false
}
