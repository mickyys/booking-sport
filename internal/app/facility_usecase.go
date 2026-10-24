package app

import (
	"context"

	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type FacilityRepository interface {
	Create(ctx context.Context, facility *domain.Facility) error
	FindByID(ctx context.Context, id primitive.ObjectID) (*domain.Facility, error)
	FindByCenterID(ctx context.Context, centerID primitive.ObjectID) ([]domain.Facility, error)
	Update(ctx context.Context, facility *domain.Facility) error
	Delete(ctx context.Context, id primitive.ObjectID) error
	CreateInquiry(ctx context.Context, inquiry *domain.FacilityInquiry) error
}

type FacilityUseCase struct {
	repo            FacilityRepository
	sportCenterRepo SportCenterRepository
}

func NewFacilityUseCase(repo FacilityRepository, sportCenterRepo SportCenterRepository) *FacilityUseCase {
	return &FacilityUseCase{repo: repo, sportCenterRepo: sportCenterRepo}
}

func (uc *FacilityUseCase) ListByCenterID(ctx context.Context, centerID primitive.ObjectID) ([]domain.Facility, error) {
	return uc.repo.FindByCenterID(ctx, centerID)
}

func (uc *FacilityUseCase) FindByID(ctx context.Context, id primitive.ObjectID) (*domain.Facility, error) {
	return uc.repo.FindByID(ctx, id)
}

func (uc *FacilityUseCase) ListBySlug(ctx context.Context, slug string) ([]domain.Facility, error) {
	center, err := uc.sportCenterRepo.FindBySlug(ctx, slug)
	if err != nil {
		return nil, err
	}
	return uc.repo.FindByCenterID(ctx, center.ID)
}

func (uc *FacilityUseCase) ListByUser(ctx context.Context, userID string) ([]domain.Facility, error) {
	centers, err := uc.sportCenterRepo.FindByUserID(ctx, userID)
	if err != nil || len(centers) == 0 {
		return []domain.Facility{}, nil
	}
	return uc.repo.FindByCenterID(ctx, centers[0].ID)
}

func (uc *FacilityUseCase) Create(ctx context.Context, facility *domain.Facility) error {
	return uc.repo.Create(ctx, facility)
}

func (uc *FacilityUseCase) Update(ctx context.Context, facility *domain.Facility) error {
	return uc.repo.Update(ctx, facility)
}

func (uc *FacilityUseCase) Delete(ctx context.Context, id primitive.ObjectID) error {
	return uc.repo.Delete(ctx, id)
}

func (uc *FacilityUseCase) CreateInquiry(ctx context.Context, inquiry *domain.FacilityInquiry) error {
	if !inquiry.FacilityID.IsZero() {
		facility, err := uc.repo.FindByID(ctx, inquiry.FacilityID)
		if err == nil {
			inquiry.SportCenterID = facility.SportCenterID
			inquiry.FacilityName = facility.Name
			inquiry.Type = facility.Type
		}
	}
	return uc.repo.CreateInquiry(ctx, inquiry)
}
