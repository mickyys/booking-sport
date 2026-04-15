package app

import (
	"context"
	"fmt"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type SportCenterRepository interface {
	Create(ctx context.Context, center *domain.SportCenter) error
	Update(ctx context.Context, center *domain.SportCenter) error
	UpdateSettings(ctx context.Context, id primitive.ObjectID, slug string, cancellationHours int, retentionPercent int, partialPaymentEnabled bool, partialPaymentPercent int) error
	FindByID(ctx context.Context, id primitive.ObjectID) (*domain.SportCenter, error)
	FindBySlug(ctx context.Context, slug string) (*domain.SportCenter, error)
	FindAll(ctx context.Context) ([]domain.SportCenter, error)
	FindPaged(ctx context.Context, page, limit int, name, city string, date *time.Time, hour *int) ([]domain.SportCenter, int64, error)
	FindByUserID(ctx context.Context, userID string) ([]domain.SportCenter, error)
	GetCities(ctx context.Context) ([]string, error)
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
	FindByMPPreferenceID(ctx context.Context, preferenceID string) (*domain.Booking, error)
	FindByMPPaymentID(ctx context.Context, paymentID string) (*domain.Booking, error)
	FindByBookingCode(ctx context.Context, code string) (*domain.Booking, error)
	UpdateStatus(ctx context.Context, id primitive.ObjectID, status domain.BookingStatus) error
	ConfirmPayment(ctx context.Context, id primitive.ObjectID, status domain.BookingStatus, paidAmount, pendingAmount float64) error
	MarkBalanceAsPaid(ctx context.Context, id primitive.ObjectID) error
	UpdateCancellation(ctx context.Context, id primitive.ObjectID, status domain.BookingStatus, cancelledBy string, reason string) error
	UpdateFintocPaymentIntentID(ctx context.Context, id primitive.ObjectID, paymentIntentID string) error
	UpdateMPPaymentID(ctx context.Context, id primitive.ObjectID, mpPaymentID string) error
	AddRefund(ctx context.Context, paymentIntentID string, refund domain.Refund) error
	AddRefundByBookingID(ctx context.Context, bookingID primitive.ObjectID, refund domain.Refund) error
	FindByCourtAndDate(ctx context.Context, courtID primitive.ObjectID, date time.Time) ([]domain.Booking, error)
	FindBySportCenterAndDate(ctx context.Context, centerID primitive.ObjectID, date time.Time) ([]domain.Booking, error)
	FindByUserID(ctx context.Context, userID string) ([]domain.Booking, error)
	FindByUserIDPaged(ctx context.Context, userID string, page, limit int, isOld bool) ([]domain.BookingSummary, int64, error)
	CountConfirmedByUserID(ctx context.Context, userID string) (int64, error)
	FindByUserIDAndStatusPaged(ctx context.Context, userID string, cancelled domain.BookingStatus, page int, limit int) ([]domain.BookingSummary, int64, error)
	Delete(ctx context.Context, id primitive.ObjectID) error
	DeleteBySeriesID(ctx context.Context, seriesID string) error
	GetDashboardData(ctx context.Context, sportCenterIDs []primitive.ObjectID, page, limit int, dateStr, name string, code string, status string) (*domain.AdminDashboardData, error)
	GetRecurringSeries(ctx context.Context, centerIDs []primitive.ObjectID) ([]domain.RecurringSeries, error)
}

// Mailer envía correos transaccionales (p. ej. confirmación de reserva)
type Mailer interface {
	SendBookingConfirmation(ctx context.Context, booking *domain.Booking) error
	SendBookingCancellation(ctx context.Context, booking *domain.Booking) error
	SendContactEmail(ctx context.Context, to string, name string, email string, phone string, sportCenterName string, message string) error
}
type SportCenterUseCase struct {
	repo        SportCenterRepository
	courtRepo   CourtRepository
	userRepo    UserRepository
	bookingRepo BookingRepository
}
type EnrichedCourtSchedule struct {
	Hour            int                 `json:"hour"`
	Minutes         int                 `json:"minutes"`
	Price           float64             `json:"price"`
	Status          string              `json:"status"`
	PaymentRequired bool                `json:"payment_required"`
	PaymentOptional bool                `json:"payment_optional"`
	BookingID       *primitive.ObjectID `json:"booking_id,omitempty"`
	// Información del cliente cuando la franja está reservada (opcional)
	CustomerName  string `json:"customer_name,omitempty"`
	CustomerEmail string `json:"customer_email,omitempty"`
	CustomerPhone string `json:"customer_phone,omitempty"`
	BookingCode        string  `json:"booking_code,omitempty"`
	PaymentMethod      string  `json:"payment_method,omitempty"`
	IsPartialPayment   bool    `json:"is_partial_payment,omitempty"`
	PaidAmount         float64 `json:"paid_amount,omitempty"`
	PendingAmount      float64 `json:"pending_amount,omitempty"`
	PartialPaymentPaid bool    `json:"partial_payment_paid,omitempty"`
}

type CourtScheduleResponse struct {
	ID       primitive.ObjectID      `json:"id"`
	Name     string                  `json:"name"`
	Schedule []EnrichedCourtSchedule `json:"schedule"`
}

func (uc *SportCenterUseCase) FindByID(ctx context.Context, id primitive.ObjectID) (*domain.SportCenter, error) {
	return uc.repo.FindByID(ctx, id)
}

func (uc *SportCenterUseCase) FindBySlug(ctx context.Context, slug string) (*domain.SportCenter, error) {
	return uc.repo.FindBySlug(ctx, slug)
}

func (uc *SportCenterUseCase) UpdateSettings(ctx context.Context, id primitive.ObjectID, slug string, cancellationHours int, retentionPercent int, partialPaymentEnabled bool, partialPaymentPercent int) error {
	_, err := uc.repo.FindByID(ctx, id)
	if err != nil {
		return err
	}
	return uc.repo.UpdateSettings(ctx, id, slug, cancellationHours, retentionPercent, partialPaymentEnabled, partialPaymentPercent)
}

func (uc *SportCenterUseCase) GetSportCenterSchedules(ctx context.Context, centerID primitive.ObjectID, date time.Time, all bool) ([]CourtScheduleResponse, error) {
	courts, err := uc.courtRepo.FindByCenterID(ctx, centerID)
	if err != nil {
		return nil, err
	}

	loc, _ := time.LoadLocation("America/Santiago")
	// Normalizar la fecha al inicio del día (00:00:00)
	searchDate := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, loc)

	// Buscar TODOS los bookings confirmados para este centro y fecha específica en una sola consulta
	allBookings, _ := uc.bookingRepo.FindBySportCenterAndDate(ctx, centerID, searchDate)

	// Agrupar bookings por CourtID para acceso rápido
	bookingsByCourt := make(map[primitive.ObjectID]map[int]*primitive.ObjectID)
	for _, b := range allBookings {
		if bookingsByCourt[b.CourtID] == nil {
			bookingsByCourt[b.CourtID] = make(map[int]*primitive.ObjectID)
		}
		id := b.ID
		bookingsByCourt[b.CourtID][b.Hour] = &id
	}

	nowInLoc := time.Now().In(loc)

	result := []CourtScheduleResponse{}
	for _, court := range courts {
		bookedHours := bookingsByCourt[court.ID]
		if bookedHours == nil {
			bookedHours = make(map[int]*primitive.ObjectID)
		}

		schedules := []EnrichedCourtSchedule{}
		for _, s := range court.Schedule {
			sch := EnrichedCourtSchedule{
				Hour:            s.Hour,
				Minutes:         s.Minutes,
				Price:           s.Price,
				Status:          s.Status,
				PaymentRequired: s.PaymentRequired,
				PaymentOptional: s.PaymentOptional,
			}

			// Check if slot has already passed
			// Forcing Chile timezone comparison to match the user's local experience
			slotTime := time.Date(date.Year(), date.Month(), date.Day(), s.Hour, s.Minutes, 0, 0, loc)
			if slotTime.Before(nowInLoc) && sch.Status == "available" {
				sch.Status = "passed"
			}

			if bID, exists := bookedHours[s.Hour]; exists {
				sch.Status = "booked"
				sch.BookingID = bID
			}

			if all || (sch.Status == "available") {
				schedules = append(schedules, sch)
			}
		}

		if schedules == nil {
			schedules = []EnrichedCourtSchedule{}
		}

		result = append(result, CourtScheduleResponse{
			ID:       court.ID,
			Name:     court.Name,
			Schedule: schedules,
		})
	}
	return result, nil
}

// GetSportCenterSchedulesWithBookingDetails retorna schedules enriquecidos
// con información del cliente (nombre, email, teléfono y código de reserva)
// para un centro y fecha específica. Solo incluye información de reservas
// confirmadas.
func (uc *SportCenterUseCase) GetSportCenterSchedulesWithBookingDetails(ctx context.Context, centerID primitive.ObjectID, date time.Time, all bool) ([]CourtScheduleResponse, error) {
	courts, err := uc.courtRepo.FindByCenterID(ctx, centerID)
	if err != nil {
		return nil, err
	}

	loc, _ := time.LoadLocation("America/Santiago")
	// Normalizar la fecha al inicio del día (00:00:00)
	searchDate := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, loc)

	// Obtener todas las reservas confirmadas para este centro y fecha
	allBookings, _ := uc.bookingRepo.FindBySportCenterAndDate(ctx, centerID, searchDate)

	// Agrupar bookings por CourtID y hora
	bookingsByCourt := make(map[primitive.ObjectID]map[int]*domain.Booking)
	for _, b := range allBookings {
		if b.Status != domain.BookingStatusConfirmed {
			continue
		}
		if bookingsByCourt[b.CourtID] == nil {
			bookingsByCourt[b.CourtID] = make(map[int]*domain.Booking)
		}
		bookingsByCourt[b.CourtID][b.Hour] = &b
	}

	nowInLoc := time.Now().In(loc)

	result := []CourtScheduleResponse{}
	for _, court := range courts {
		bookedHours := bookingsByCourt[court.ID]
		if bookedHours == nil {
			bookedHours = make(map[int]*domain.Booking)
		}

		schedules := []EnrichedCourtSchedule{}
		for _, s := range court.Schedule {
			sch := EnrichedCourtSchedule{
				Hour:            s.Hour,
				Minutes:         s.Minutes,
				Price:           s.Price,
				Status:          s.Status,
				PaymentRequired: s.PaymentRequired,
				PaymentOptional: s.PaymentOptional,
			}

			// Check if slot has already passed
			slotTime := time.Date(date.Year(), date.Month(), date.Day(), s.Hour, s.Minutes, 0, 0, loc)
			if slotTime.Before(nowInLoc) && sch.Status == "available" {
				sch.Status = "passed"
			}

			if b, exists := bookedHours[s.Hour]; exists && b != nil {
				// Si hay una reserva, mostramos la información sin importar si la hora ya pasó
				sch.Status = "booked"
				if slotTime.Before(nowInLoc) {
					sch.Status = "passed_booked" // Opcional: estado especial para reservas pasadas
				}
				sch.BookingID = &b.ID
				// Preferir GuestDetails si existe
				if b.GuestDetails != nil {
					sch.CustomerName = b.GuestDetails.Name
					sch.CustomerEmail = b.GuestDetails.Email
					sch.CustomerPhone = b.GuestDetails.Phone
				} else {
					sch.CustomerName = b.CustomerName
					sch.CustomerEmail = ""
					sch.CustomerPhone = b.CustomerPhone
				}
				sch.BookingCode = b.BookingCode
				sch.PaymentMethod = b.PaymentMethod
				sch.IsPartialPayment = b.IsPartialPayment
				sch.PaidAmount = b.PaidAmount
				sch.PendingAmount = b.PendingAmount
				sch.PartialPaymentPaid = b.PartialPaymentPaid
				// Diferenciar entre reserva interna y bloqueo
				if b.PaymentMethod == "internal" {
					if b.GuestDetails != nil && b.GuestDetails.Name != "" {
						sch.PaymentMethod = "internal_reservation"
					} else {
						sch.PaymentMethod = "internal_block"
					}
				}
			}

			if all || (sch.Status == "available" || sch.Status == "booked" || sch.Status == "passed_booked" || sch.Status == "closed" || sch.Status == "passed") {
				schedules = append(schedules, sch)
			}
		}

		if schedules == nil {
			schedules = []EnrichedCourtSchedule{}
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
	if center.Slug != "" {
		existing, _ := uc.repo.FindBySlug(ctx, center.Slug)
		if existing != nil {
			return fmt.Errorf("el subdominio '%s' ya está en uso", center.Slug)
		}
	}
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

func (uc *SportCenterUseCase) ListCities(ctx context.Context) ([]string, error) {
	cities, err := uc.repo.GetCities(ctx)
	if err != nil {
		return nil, err
	}
	if cities == nil {
		cities = []string{}
	}
	return cities, nil
}

func (uc *SportCenterUseCase) ListSportCentersPaged(ctx context.Context, page, limit int, name, city string, date *time.Time, hour *int) (*domain.PagedResponse, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 10
	}

	centers, total, err := uc.repo.FindPaged(ctx, page, limit, name, city, date, hour)
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

// FindByUserID devuelve los centros deportivos asociados a un usuario (administrador)
func (uc *SportCenterUseCase) FindByUserID(ctx context.Context, userID string) ([]domain.SportCenter, error) {
	return uc.repo.FindByUserID(ctx, userID)
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
	UpdateScheduleSlot(ctx context.Context, id primitive.ObjectID, slot domain.CourtSchedule) error
	Delete(ctx context.Context, id primitive.ObjectID) error
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

func (uc *CourtUseCase) CreateAdminCourt(ctx context.Context, court *domain.Court, userID string) error {
	center, err := uc.centerRepo.FindByID(ctx, court.SportCenterID)
	if err != nil {
		return err
	}

	isOwner := false
	for _, user := range center.Users {
		if user == userID {
			isOwner = true
			break
		}
	}
	if !isOwner {
		return fmt.Errorf("user is not authorized to create court for this sport center")
	}

	court.CreatedAt = time.Now()
	court.UpdatedAt = time.Now()
	return uc.repo.Create(ctx, court)
}

func (uc *CourtUseCase) UpdateAdminCourt(ctx context.Context, courtID primitive.ObjectID, updatedCourt *domain.Court, userID string) error {
	existing, err := uc.repo.FindByID(ctx, courtID)
	if err != nil {
		return err
	}

	center, err := uc.centerRepo.FindByID(ctx, existing.SportCenterID)
	if err != nil {
		return err
	}

	isOwner := false
	for _, user := range center.Users {
		if user == userID {
			isOwner = true
			break
		}
	}
	if !isOwner {
		return fmt.Errorf("user is not authorized to update court for this sport center")
	}

	existing.Name = updatedCourt.Name
	existing.Description = updatedCourt.Description
	existing.UpdatedAt = time.Now()

	return uc.repo.Update(ctx, existing)
}

func (uc *CourtUseCase) DeleteAdminCourt(ctx context.Context, courtID primitive.ObjectID, userID string) error {
	existing, err := uc.repo.FindByID(ctx, courtID)
	if err != nil {
		return err
	}

	center, err := uc.centerRepo.FindByID(ctx, existing.SportCenterID)
	if err != nil {
		return err
	}

	isOwner := false
	for _, user := range center.Users {
		if user == userID {
			isOwner = true
			break
		}
	}
	if !isOwner {
		return fmt.Errorf("user is not authorized to delete court for this sport center")
	}

	return uc.repo.Delete(ctx, courtID)
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

func (uc *CourtUseCase) ConfigureSchedule(ctx context.Context, courtID primitive.ObjectID, schedule []domain.CourtSchedule, userID string) error {
	court, err := uc.repo.FindByID(ctx, courtID)
	if err != nil {
		return err
	}

	center, err := uc.centerRepo.FindByID(ctx, court.SportCenterID)
	if err != nil {
		return err
	}

	// Verify permissions
	isOwner := false
	for _, u := range center.Users {
		if u == userID {
			isOwner = true
			break
		}
	}
	if !isOwner {
		return fmt.Errorf("user is not authorized to configure schedule for this court")
	}

	court.Schedule = schedule
	court.UpdatedAt = time.Now()
	return uc.repo.Update(ctx, court)
}

func (uc *CourtUseCase) UpdateScheduleSlot(ctx context.Context, courtID primitive.ObjectID, slot domain.CourtSchedule, userID string) error {
	court, err := uc.repo.FindByID(ctx, courtID)
	if err != nil {
		return err
	}

	center, err := uc.centerRepo.FindByID(ctx, court.SportCenterID)
	if err != nil {
		return err
	}

	// Verify permissions
	isOwner := false
	for _, u := range center.Users {
		if u == userID {
			isOwner = true
			break
		}
	}
	if !isOwner {
		return fmt.Errorf("user is not authorized to configure schedule for this court")
	}

	return uc.repo.UpdateScheduleSlot(ctx, courtID, slot)
}

func (uc *CourtUseCase) GetCourtSchedule(ctx context.Context, courtID primitive.ObjectID, date time.Time, all bool) ([]domain.CourtSchedule, error) {
	court, err := uc.repo.FindByID(ctx, courtID)
	if err != nil {
		return nil, err
	}

	loc, _ := time.LoadLocation("America/Santiago")
	searchDate := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, loc)
	bookings, _ := uc.bookingRepo.FindByCourtAndDate(ctx, courtID, searchDate)
	bookedHours := make(map[int]bool)
	for _, b := range bookings {
		if b.Status == domain.BookingStatusConfirmed {
			bookedHours[b.Hour] = true
		}
	}

	nowInLoc := time.Now().In(loc)
	result := []domain.CourtSchedule{}
	for _, s := range court.Schedule {
		sch := s
		if bookedHours[s.Hour] {
			sch.Status = "booked"
		}

		// Check if slot has already passed
		// Forcing Chile timezone comparison to match the user's local experience
		slotTime := time.Date(date.Year(), date.Month(), date.Day(), s.Hour, s.Minutes, 0, 0, loc)
		if slotTime.Before(nowInLoc) && sch.Status == "available" {
			sch.Status = "passed"
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

type CenterCourtsResponse struct {
	SportCenter domain.SportCenter `json:"sport_center"`
	Courts      []domain.Court     `json:"courts"`
}

func (uc *CourtUseCase) GetCourtsByAdminUser(ctx context.Context, userID string) ([]CenterCourtsResponse, error) {
	// Find centers for this user
	centers, err := uc.centerRepo.FindByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}

	result := []CenterCourtsResponse{}
	for _, center := range centers {
		courts, err := uc.repo.FindByCenterID(ctx, center.ID)
		if err != nil {
			return nil, err
		}
		if courts == nil {
			courts = []domain.Court{}
		}
		result = append(result, CenterCourtsResponse{
			SportCenter: center,
			Courts:      courts,
		})
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
		schedules := court.Schedule
		enrichedSchedules := []EnrichedCourtSchedule{}
		for _, s := range schedules {
			enrichedSchedules = append(enrichedSchedules, EnrichedCourtSchedule{
				Hour:            s.Hour,
				Minutes:         s.Minutes,
				Price:           s.Price,
				Status:          s.Status,
				PaymentRequired: s.PaymentRequired,
				PaymentOptional: s.PaymentOptional,
			})
		}

		result = append(result, CourtScheduleResponse{
			ID:       court.ID,
			Name:     court.Name,
			Schedule: enrichedSchedules,
		})
	}
	return result, nil
}
