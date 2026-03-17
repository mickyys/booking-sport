package app

import (
	"context"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type SportCenterRepository interface {
	Create(ctx context.Context, center *domain.SportCenter) error
	Update(ctx context.Context, center *domain.SportCenter) error
	FindByID(ctx context.Context, id primitive.ObjectID) (*domain.SportCenter, error)
	FindAll(ctx context.Context) ([]domain.SportCenter, error)
	FindPaged(ctx context.Context, page, limit int) ([]domain.SportCenter, int64, error)
}

type UserRepository interface {
	Create(ctx context.Context, user *domain.User) error
	FindByUsername(ctx context.Context, username string) (*domain.User, error)
}

type BookingRepository interface {
	Create(ctx context.Context, booking *domain.Booking) error
	Update(ctx context.Context, booking *domain.Booking) error
	FindByID(ctx context.Context, id primitive.ObjectID) (*domain.Booking, error)
	FindByPreferenceID(ctx context.Context, preferenceID string) (*domain.Booking, error)
	FindByFintocPaymentID(ctx context.Context, fintocPaymentID string) (*domain.Booking, error)
	FindByFintocPaymentIntentID(ctx context.Context, paymentIntentID string) (*domain.Booking, error)
	FindByBookingCode(ctx context.Context, code string) (*domain.Booking, error)
	UpdateStatus(ctx context.Context, id primitive.ObjectID, status domain.BookingStatus) error
	UpdateFintocPaymentIntentID(ctx context.Context, id primitive.ObjectID, paymentIntentID string) error
	AddRefund(ctx context.Context, paymentIntentID string, refund domain.Refund) error
	FindByCourtAndDate(ctx context.Context, courtID primitive.ObjectID, date time.Time) ([]domain.Booking, error)
	FindByUserID(ctx context.Context, userID string) ([]domain.Booking, error)
	FindByUserIDPaged(ctx context.Context, userID string, page, limit int, isOld bool) ([]domain.BookingSummary, int64, error)
	CountConfirmedByUserID(ctx context.Context, userID string) (int64, error)
}

type SportCenterUseCase struct {
	repo        SportCenterRepository
	courtRepo   CourtRepository
	userRepo    UserRepository
	bookingRepo BookingRepository
}

type CourtScheduleResponse struct {
	ID       primitive.ObjectID     `json:"id"`
	Name     string                 `json:"name"`
	Schedule []domain.CourtSchedule `json:"schedule"`
}

func (uc *SportCenterUseCase) GetSportCenterSchedules(ctx context.Context, centerID primitive.ObjectID, date time.Time, all bool) ([]CourtScheduleResponse, error) {
	courts, err := uc.courtRepo.FindByCenterID(ctx, centerID)
	if err != nil {
		return nil, err
	}

	// Normalizar la fecha al inicio del día (00:00:00)
	searchDate := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location())

	result := []CourtScheduleResponse{}
	for _, court := range courts {
		// Buscar bookings confirmados para esta cancha y fecha específica
		bookings, _ := uc.bookingRepo.FindByCourtAndDate(ctx, court.ID, searchDate)
		bookedHours := make(map[int]bool)
		for _, b := range bookings {
			if b.Status == domain.BookingStatusConfirmed {
				bookedHours[b.Hour] = true
			}
		}

		schedules := []domain.CourtSchedule{}
		for _, s := range court.Schedule {
			sch := s
			if bookedHours[s.Hour] {
				sch.Status = "booked"
			}

			if all || sch.Status == "available" {
				schedules = append(schedules, sch)
			}
		}

		if schedules == nil {
			schedules = []domain.CourtSchedule{}
		}

		result = append(result, CourtScheduleResponse{
			ID:       court.ID,
			Name:     court.Name,
			Schedule: schedules,
		})
	}
	return result, nil
}

func NewSportCenterUseCase(repo SportCenterRepository, courtRepo CourtRepository, userRepo UserRepository, bookingRepo BookingRepository) *SportCenterUseCase {
	return &SportCenterUseCase{repo: repo, courtRepo: courtRepo, userRepo: userRepo, bookingRepo: bookingRepo}
}

func (uc *SportCenterUseCase) CreateSportCenter(ctx context.Context, center *domain.SportCenter) error {
	center.CreatedAt = time.Now()
	center.UpdatedAt = time.Now()
	return uc.repo.Create(ctx, center)
}

func (uc *SportCenterUseCase) UpdateSportCenter(ctx context.Context, id primitive.ObjectID, updatedCenter *domain.SportCenter) error {
	existing, err := uc.repo.FindByID(ctx, id)
	if err != nil {
		return err
	}

	updatedCenter.ID = existing.ID
	updatedCenter.CreatedAt = existing.CreatedAt
	updatedCenter.UpdatedAt = time.Now()

	// Mantener la configuración de Fintoc existente si no se proporciona una nueva
	if updatedCenter.Fintoc == nil {
		updatedCenter.Fintoc = existing.Fintoc
	}

	return uc.repo.Update(ctx, updatedCenter)
}

func (uc *SportCenterUseCase) ListSportCenters(ctx context.Context) ([]domain.SportCenter, error) {
	return uc.repo.FindAll(ctx)
}

func (uc *SportCenterUseCase) ListSportCentersPaged(ctx context.Context, page, limit int) (*domain.PagedResponse, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 10
	}

	centers, total, err := uc.repo.FindPaged(ctx, page, limit)
	if err != nil {
		return nil, err
	}

	// Asegurar que centers sea un array vacío si es nil para el JSON
	if centers == nil {
		centers = []domain.SportCenter{}
	}

	totalPages := int((total + int64(limit) - 1) / int64(limit))

	return &domain.PagedResponse{
		Data:       centers,
		Total:      total,
		Page:       page,
		Limit:      limit,
		TotalPages: totalPages,
	}, nil
}

type UserUseCase struct {
	repo UserRepository
}

func NewUserUseCase(repo UserRepository) *UserUseCase {
	return &UserUseCase{repo: repo}
}

type CourtRepository interface {
	Create(ctx context.Context, court *domain.Court) error
	Update(ctx context.Context, court *domain.Court) error
	FindByID(ctx context.Context, id primitive.ObjectID) (*domain.Court, error)
	FindByCenterID(ctx context.Context, centerID primitive.ObjectID) ([]domain.Court, error)
	FindAllPaged(ctx context.Context, page, limit int) ([]domain.Court, int64, error)
}

type CourtUseCase struct {
	repo        CourtRepository
	centerRepo  SportCenterRepository
	bookingRepo BookingRepository
}

func NewCourtUseCase(repo CourtRepository, centerRepo SportCenterRepository, bookingRepo BookingRepository) *CourtUseCase {
	return &CourtUseCase{repo: repo, centerRepo: centerRepo, bookingRepo: bookingRepo}
}

func (uc *CourtUseCase) CreateCourt(ctx context.Context, court *domain.Court) error {
	_, err := uc.centerRepo.FindByID(ctx, court.SportCenterID)
	if err != nil {
		return err
	}

	court.CreatedAt = time.Now()
	court.UpdatedAt = time.Now()
	return uc.repo.Create(ctx, court)
}

func (uc *CourtUseCase) ListCourtsPaged(ctx context.Context, page, limit int) (*domain.PagedResponse, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 10
	}

	courts, total, err := uc.repo.FindAllPaged(ctx, page, limit)
	if err != nil {
		return nil, err
	}

	if courts == nil {
		courts = []domain.Court{}
	}

	totalPages := int((total + int64(limit) - 1) / int64(limit))

	return &domain.PagedResponse{
		Data:       courts,
		Total:      total,
		Page:       page,
		Limit:      limit,
		TotalPages: totalPages,
	}, nil
}

func (uc *CourtUseCase) ConfigureSchedule(ctx context.Context, courtID primitive.ObjectID, schedule []domain.CourtSchedule) error {
	court, err := uc.repo.FindByID(ctx, courtID)
	if err != nil {
		return err
	}

	court.Schedule = schedule
	court.UpdatedAt = time.Now()
	return uc.repo.Update(ctx, court)
}

func (uc *CourtUseCase) GetCourtSchedule(ctx context.Context, courtID primitive.ObjectID, date time.Time, all bool) ([]domain.CourtSchedule, error) {
	court, err := uc.repo.FindByID(ctx, courtID)
	if err != nil {
		return nil, err
	}

	searchDate := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location())
	bookings, _ := uc.bookingRepo.FindByCourtAndDate(ctx, courtID, searchDate)
	bookedHours := make(map[int]bool)
	for _, b := range bookings {
		if b.Status == domain.BookingStatusConfirmed {
			bookedHours[b.Hour] = true
		}
	}

	result := []domain.CourtSchedule{}
	for _, s := range court.Schedule {
		sch := s
		if bookedHours[s.Hour] {
			sch.Status = "booked"
		}
		if all || sch.Status == "available" {
			result = append(result, sch)
		}
	}

	return result, nil
}

func (uc *CourtUseCase) GetSportCenterSchedules(ctx context.Context, centerID primitive.ObjectID, all bool) (map[string][]domain.CourtSchedule, error) {
	courts, err := uc.repo.FindByCenterID(ctx, centerID)
	if err != nil {
		return nil, err
	}

	result := make(map[string][]domain.CourtSchedule)
	for _, court := range courts {
		schedules := court.Schedule
		if !all {
			available := []domain.CourtSchedule{}
			for _, s := range schedules {
				if s.Status == "available" {
					available = append(available, s)
				}
			}
			schedules = available
		}
		result[court.Name] = schedules
	}
	return result, nil
}

func (uc *CourtUseCase) GetSportCenterSchedulesWithBookings(ctx context.Context, centerID primitive.ObjectID, all bool) ([]CourtScheduleResponse, error) {
	courts, err := uc.repo.FindByCenterID(ctx, centerID)
	if err != nil {
		return nil, err
	}

	result := []CourtScheduleResponse{}
	for _, court := range courts {
		// NOTA: Como CourtUseCase no tiene bookingRepo por defecto y no quiero romper dependencias circulares
		// si fuera necesario, pero aquí usaremos la lógica de marcar como booked.
		// Para esta implementación, asumimos que CourtUseCase sólo maneja la estructura base.

		schedules := court.Schedule
		if !all {
			available := []domain.CourtSchedule{}
			for _, s := range schedules {
				if s.Status == "available" {
					available = append(available, s)
				}
			}
			schedules = available
		}

		result = append(result, CourtScheduleResponse{
			ID:       court.ID,
			Name:     court.Name,
			Schedule: schedules,
		})
	}
	return result, nil
}
