package app

import (
	"context"
	"testing"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// ---------- Mock RecurringReservationRepository ----------

type mockRecurringReservationRepo struct {
	CreateFn                     func(ctx context.Context, reservation *domain.RecurringReservation) error
	FindByIDFn                   func(ctx context.Context, id primitive.ObjectID) (*domain.RecurringReservation, error)
	FindByCourtHourAndDayFn      func(ctx context.Context, courtID primitive.ObjectID, hour int, dayOfWeek int) (*domain.RecurringReservation, error)
	FindByCourtAndHourFn         func(ctx context.Context, courtID primitive.ObjectID, hour int) (*domain.RecurringReservation, error)
	FindActiveByCourtAndHourFn   func(ctx context.Context, courtID primitive.ObjectID, hour int) (*domain.RecurringReservation, error)
	FindByCenterIDFn             func(ctx context.Context, centerID primitive.ObjectID) ([]domain.RecurringReservation, error)
	FindAdminByCenterIDFn        func(ctx context.Context, centerID primitive.ObjectID) ([]domain.RecurringReservation, error)
	FindByCenterIDAndDayOfWeekFn func(ctx context.Context, centerID primitive.ObjectID, dayOfWeek int) ([]domain.RecurringReservation, error)
	FindByCourtIDFn              func(ctx context.Context, courtID primitive.ObjectID) ([]domain.RecurringReservation, error)
	UpdateFn                     func(ctx context.Context, reservation *domain.RecurringReservation) error
	CancelFn                     func(ctx context.Context, id primitive.ObjectID, cancelledBy string, reason string) error
	FinishFn                     func(ctx context.Context, id primitive.ObjectID, finishedBy string, reason string, finishedAt time.Time) error
	DeleteFn                     func(ctx context.Context, id primitive.ObjectID) error
	AddCancelledDateFn           func(ctx context.Context, id primitive.ObjectID, date string) error

	CreateCalls int
}

func (m *mockRecurringReservationRepo) Create(ctx context.Context, reservation *domain.RecurringReservation) error {
	m.CreateCalls++
	if m.CreateFn != nil {
		return m.CreateFn(ctx, reservation)
	}
	return nil
}

func (m *mockRecurringReservationRepo) FindByID(ctx context.Context, id primitive.ObjectID) (*domain.RecurringReservation, error) {
	if m.FindByIDFn != nil {
		return m.FindByIDFn(ctx, id)
	}
	return nil, nil
}

func (m *mockRecurringReservationRepo) FindByCourtHourAndDay(ctx context.Context, courtID primitive.ObjectID, hour int, dayOfWeek int) (*domain.RecurringReservation, error) {
	if m.FindByCourtHourAndDayFn != nil {
		return m.FindByCourtHourAndDayFn(ctx, courtID, hour, dayOfWeek)
	}
	return nil, nil
}

func (m *mockRecurringReservationRepo) FindByCourtAndHour(ctx context.Context, courtID primitive.ObjectID, hour int) (*domain.RecurringReservation, error) {
	if m.FindByCourtAndHourFn != nil {
		return m.FindByCourtAndHourFn(ctx, courtID, hour)
	}
	return nil, nil
}

func (m *mockRecurringReservationRepo) FindActiveByCourtAndHour(ctx context.Context, courtID primitive.ObjectID, hour int) (*domain.RecurringReservation, error) {
	if m.FindActiveByCourtAndHourFn != nil {
		return m.FindActiveByCourtAndHourFn(ctx, courtID, hour)
	}
	return nil, nil
}

func (m *mockRecurringReservationRepo) FindByCenterID(ctx context.Context, centerID primitive.ObjectID) ([]domain.RecurringReservation, error) {
	if m.FindByCenterIDFn != nil {
		return m.FindByCenterIDFn(ctx, centerID)
	}
	return nil, nil
}

func (m *mockRecurringReservationRepo) FindAdminByCenterID(ctx context.Context, centerID primitive.ObjectID) ([]domain.RecurringReservation, error) {
	if m.FindAdminByCenterIDFn != nil {
		return m.FindAdminByCenterIDFn(ctx, centerID)
	}
	return nil, nil
}

func (m *mockRecurringReservationRepo) FindByCenterIDAndDayOfWeek(ctx context.Context, centerID primitive.ObjectID, dayOfWeek int) ([]domain.RecurringReservation, error) {
	if m.FindByCenterIDAndDayOfWeekFn != nil {
		return m.FindByCenterIDAndDayOfWeekFn(ctx, centerID, dayOfWeek)
	}
	return nil, nil
}

func (m *mockRecurringReservationRepo) FindByCourtID(ctx context.Context, courtID primitive.ObjectID) ([]domain.RecurringReservation, error) {
	if m.FindByCourtIDFn != nil {
		return m.FindByCourtIDFn(ctx, courtID)
	}
	return nil, nil
}

func (m *mockRecurringReservationRepo) Update(ctx context.Context, reservation *domain.RecurringReservation) error {
	if m.UpdateFn != nil {
		return m.UpdateFn(ctx, reservation)
	}
	return nil
}

func (m *mockRecurringReservationRepo) Cancel(ctx context.Context, id primitive.ObjectID, cancelledBy string, reason string) error {
	if m.CancelFn != nil {
		return m.CancelFn(ctx, id, cancelledBy, reason)
	}
	return nil
}

func (m *mockRecurringReservationRepo) Finish(ctx context.Context, id primitive.ObjectID, finishedBy string, reason string, finishedAt time.Time) error {
	if m.FinishFn != nil {
		return m.FinishFn(ctx, id, finishedBy, reason, finishedAt)
	}
	return nil
}

func (m *mockRecurringReservationRepo) Delete(ctx context.Context, id primitive.ObjectID) error {
	if m.DeleteFn != nil {
		return m.DeleteFn(ctx, id)
	}
	return nil
}

func (m *mockRecurringReservationRepo) AddCancelledDate(ctx context.Context, id primitive.ObjectID, date string) error {
	if m.AddCancelledDateFn != nil {
		return m.AddCancelledDateFn(ctx, id, date)
	}
	return nil
}

var _ RecurringReservationRepository = (*mockRecurringReservationRepo)(nil)

// ---------- Mock CourtRepository (stub) ----------

type mockCourtRepoForRecurring struct {
	FindByIDFn func(ctx context.Context, id primitive.ObjectID) (*domain.Court, error)
}

func (m *mockCourtRepoForRecurring) FindByID(ctx context.Context, id primitive.ObjectID) (*domain.Court, error) {
	if m.FindByIDFn != nil {
		return m.FindByIDFn(ctx, id)
	}
	return &domain.Court{
		ID:            id,
		SportCenterID: primitive.NewObjectID(),
		Schedule: []domain.CourtSchedule{
			{Hour: 10, Minutes: 0, Price: 30000},
		},
	}, nil
}

func (m *mockCourtRepoForRecurring) FindByCenterID(ctx context.Context, centerID primitive.ObjectID) ([]domain.Court, error) {
	return nil, nil
}

func (m *mockCourtRepoForRecurring) FindBySlug(ctx context.Context, slug string) (*domain.Court, error) {
	return nil, nil
}

func (m *mockCourtRepoForRecurring) FindAllPaged(ctx context.Context, page, limit int) ([]domain.Court, int64, error) {
	return nil, 0, nil
}

func (m *mockCourtRepoForRecurring) Create(ctx context.Context, court *domain.Court) error {
	return nil
}

func (m *mockCourtRepoForRecurring) Update(ctx context.Context, court *domain.Court) error {
	return nil
}

func (m *mockCourtRepoForRecurring) UpdateScheduleSlot(ctx context.Context, id primitive.ObjectID, slot domain.CourtSchedule) error {
	return nil
}

func (m *mockCourtRepoForRecurring) Delete(ctx context.Context, id primitive.ObjectID) error {
	return nil
}

func (m *mockCourtRepoForRecurring) SyncPartialPaymentSlots(ctx context.Context, centerID primitive.ObjectID, partialPaymentEnabled bool) (int64, error) {
	return 0, nil
}

var _ CourtRepository = (*mockCourtRepoForRecurring)(nil)

// ---------- Mock SportCenterRepository (stub) ----------

type mockCenterRepoForRecurring struct {
	FindByUserIDFn func(ctx context.Context, userID string) ([]domain.SportCenter, error)
}

func (m *mockCenterRepoForRecurring) FindByID(ctx context.Context, id primitive.ObjectID) (*domain.SportCenter, error) {
	return &domain.SportCenter{
		ID:   id,
		Name: "Centro Deportivo Test",
	}, nil
}

func (m *mockCenterRepoForRecurring) FindBySlug(ctx context.Context, slug string) (*domain.SportCenter, error) {
	return nil, nil
}

func (m *mockCenterRepoForRecurring) FindByUserID(ctx context.Context, userID string) ([]domain.SportCenter, error) {
	if m.FindByUserIDFn != nil {
		return m.FindByUserIDFn(ctx, userID)
	}
	return nil, nil
}

func (m *mockCenterRepoForRecurring) FindAll(ctx context.Context) ([]domain.SportCenter, error) {
	return nil, nil
}

func (m *mockCenterRepoForRecurring) FindPaged(ctx context.Context, page, limit int, name, city string, date *time.Time, hour *int) ([]domain.SportCenter, int64, error) {
	return nil, 0, nil
}

func (m *mockCenterRepoForRecurring) Update(ctx context.Context, center *domain.SportCenter) error {
	return nil
}

func (m *mockCenterRepoForRecurring) UpdateSettings(ctx context.Context, id primitive.ObjectID, slug *string, cancellationHours *int, retentionPercent *int, partialPaymentEnabled *bool, partialPaymentPercent *int, imageURL *string) error {
	return nil
}

func (m *mockCenterRepoForRecurring) Create(ctx context.Context, center *domain.SportCenter) error {
	return nil
}

func (m *mockCenterRepoForRecurring) GetCities(ctx context.Context) ([]string, error) {
	return nil, nil
}

var _ SportCenterRepository = (*mockCenterRepoForRecurring)(nil)

func TestCancelRecurringReservationFinishesActiveReservation(t *testing.T) {
	centerID := primitive.NewObjectID()
	reservationID := primitive.NewObjectID()
	var finishedBy string
	var finishReason string
	repo := &mockRecurringReservationRepo{
		FindByIDFn: func(context.Context, primitive.ObjectID) (*domain.RecurringReservation, error) {
			return &domain.RecurringReservation{
				ID:            reservationID,
				SportCenterID: centerID,
				Status:        domain.RecurringReservationStatusActive,
			}, nil
		},
		FinishFn: func(_ context.Context, _ primitive.ObjectID, userID, reason string, _ time.Time) error {
			finishedBy = userID
			finishReason = reason
			return nil
		},
	}
	uc := &BookingUseCase{
		recurringReservationRepo: repo,
		centerRepo: &mockCenterRepoForRecurring{FindByUserIDFn: func(context.Context, string) ([]domain.SportCenter, error) {
			return []domain.SportCenter{{ID: centerID}}, nil
		}},
	}

	err := uc.CancelRecurringReservation(context.Background(), reservationID, "auth0|admin", "Cierre solicitado")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if finishedBy != "auth0|admin" || finishReason != "Cierre solicitado" {
		t.Fatalf("unexpected finish metadata: by=%q reason=%q", finishedBy, finishReason)
	}
}

func TestCancelRecurringReservationIsIdempotentWhenFinished(t *testing.T) {
	finishCalled := false
	uc := &BookingUseCase{
		recurringReservationRepo: &mockRecurringReservationRepo{
			FindByIDFn: func(context.Context, primitive.ObjectID) (*domain.RecurringReservation, error) {
				return &domain.RecurringReservation{Status: domain.RecurringReservationStatusFinished}, nil
			},
			FinishFn: func(context.Context, primitive.ObjectID, string, string, time.Time) error {
				finishCalled = true
				return nil
			},
		},
	}

	if err := uc.CancelRecurringReservation(context.Background(), primitive.NewObjectID(), "admin", ""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if finishCalled {
		t.Fatal("finished reservation must not be updated again")
	}
}

func TestCancelRecurringReservationRejectsOtherCenter(t *testing.T) {
	uc := &BookingUseCase{
		recurringReservationRepo: &mockRecurringReservationRepo{
			FindByIDFn: func(context.Context, primitive.ObjectID) (*domain.RecurringReservation, error) {
				return &domain.RecurringReservation{
					SportCenterID: primitive.NewObjectID(),
					Status:        domain.RecurringReservationStatusActive,
				}, nil
			},
		},
		centerRepo: &mockCenterRepoForRecurring{FindByUserIDFn: func(context.Context, string) ([]domain.SportCenter, error) {
			return []domain.SportCenter{{ID: primitive.NewObjectID()}}, nil
		}},
	}

	if err := uc.CancelRecurringReservation(context.Background(), primitive.NewObjectID(), "admin", ""); err == nil {
		t.Fatal("expected authorization error")
	}
}

func TestDeleteSeriesFinishesOnlyInAdminCenters(t *testing.T) {
	centerID := primitive.NewObjectID()
	var receivedSeriesID string
	var receivedCenterIDs []primitive.ObjectID
	var receivedUserID string
	bookingRepo := &mockBookingRepoForHold{
		FinishSeriesFn: func(_ context.Context, seriesID string, centerIDs []primitive.ObjectID, userID, _ string, _ time.Time) (int64, error) {
			receivedSeriesID = seriesID
			receivedCenterIDs = centerIDs
			receivedUserID = userID
			return 2, nil
		},
	}
	uc := &BookingUseCase{
		repo: bookingRepo,
		centerRepo: &mockCenterRepoForRecurring{FindByUserIDFn: func(context.Context, string) ([]domain.SportCenter, error) {
			return []domain.SportCenter{{ID: centerID}}, nil
		}},
	}

	if err := uc.DeleteSeries(context.Background(), "SERIE-123", "auth0|admin", "Fin anticipado"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receivedSeriesID != "SERIE-123" || receivedUserID != "auth0|admin" {
		t.Fatalf("unexpected finish request: series=%q user=%q", receivedSeriesID, receivedUserID)
	}
	if len(receivedCenterIDs) != 1 || receivedCenterIDs[0] != centerID {
		t.Fatalf("unexpected authorized centers: %v", receivedCenterIDs)
	}
}

// ---------- Tests ----------

func TestCreateRecurringReservation_Conflict_ActiveWithCancelledDates(t *testing.T) {
	courtID := newObjectID()
	hour := 10
	dayOfWeek := 2 // martes

	existing := &domain.RecurringReservation{
		ID:             newObjectID(),
		CourtID:        courtID,
		Hour:           hour,
		DayOfWeek:      dayOfWeek,
		DayOfWeekName:  "martes",
		Status:         domain.RecurringReservationStatusActive,
		CancelledDates: []string{"2026-07-14"},
	}

	recurringRepo := &mockRecurringReservationRepo{
		FindByCourtHourAndDayFn: func(ctx context.Context, cID primitive.ObjectID, h int, d int) (*domain.RecurringReservation, error) {
			return existing, nil
		},
	}
	courtRepo := &mockCourtRepoForRecurring{}
	centerRepo := &mockCenterRepoForRecurring{}

	uc := &BookingUseCase{
		recurringReservationRepo: recurringRepo,
		courtRepo:                courtRepo,
		centerRepo:               centerRepo,
		repo: &mockBookingRepoForHold{
			FindConfirmedBookingsAfterFn: func(ctx context.Context, courtID primitive.ObjectID, hour int, since time.Time) ([]domain.Booking, error) {
				return []domain.Booking{
					{ID: newObjectID(), CourtID: courtID, Hour: hour, Date: time.Date(2026, 7, 21, 0, 0, 0, 0, time.UTC), Status: domain.BookingStatusConfirmed},
				}, nil
			},
		},
	}

	date := time.Date(2026, 7, 21, 0, 0, 0, 0, time.UTC) // martes
	reservation := &domain.RecurringReservation{
		CourtID:       courtID,
		Hour:          hour,
		CustomerName:  "Test User",
		CustomerPhone: "123456789",
		Price:         30000,
	}

	err := uc.CreateRecurringReservation(context.Background(), reservation, date)
	if err == nil {
		t.Fatal("expected error for conflicting active recurring reservation, got nil")
	}
	expectedMsg := "ya existe una reserva recurrente semanal para esta cancha, hora y día"
	if err.Error() != expectedMsg {
		t.Errorf("expected error %q, got %q", expectedMsg, err.Error())
	}
	if recurringRepo.CreateCalls != 0 {
		t.Errorf("expected 0 Create calls, got %d", recurringRepo.CreateCalls)
	}
}

func TestCreateRecurringReservation_Conflict_ActiveWithoutCancelledDates(t *testing.T) {
	courtID := newObjectID()
	hour := 10
	dayOfWeek := 2

	existing := &domain.RecurringReservation{
		ID:            newObjectID(),
		CourtID:       courtID,
		Hour:          hour,
		DayOfWeek:     dayOfWeek,
		DayOfWeekName: "martes",
		Status:        domain.RecurringReservationStatusActive,
	}

	recurringRepo := &mockRecurringReservationRepo{
		FindByCourtHourAndDayFn: func(ctx context.Context, cID primitive.ObjectID, h int, d int) (*domain.RecurringReservation, error) {
			return existing, nil
		},
	}
	courtRepo := &mockCourtRepoForRecurring{}
	centerRepo := &mockCenterRepoForRecurring{}

	uc := &BookingUseCase{
		recurringReservationRepo: recurringRepo,
		courtRepo:                courtRepo,
		centerRepo:               centerRepo,
		repo: &mockBookingRepoForHold{
			FindConfirmedBookingsAfterFn: func(ctx context.Context, courtID primitive.ObjectID, hour int, since time.Time) ([]domain.Booking, error) {
				return []domain.Booking{
					{ID: newObjectID(), CourtID: courtID, Hour: hour, Date: time.Date(2026, 7, 21, 0, 0, 0, 0, time.UTC), Status: domain.BookingStatusConfirmed},
				}, nil
			},
		},
	}

	date := time.Date(2026, 7, 21, 0, 0, 0, 0, time.UTC)
	reservation := &domain.RecurringReservation{
		CourtID:       courtID,
		Hour:          hour,
		CustomerName:  "Test User",
		CustomerPhone: "123456789",
		Price:         30000,
	}

	err := uc.CreateRecurringReservation(context.Background(), reservation, date)
	if err == nil {
		t.Fatal("expected error for conflicting active recurring reservation, got nil")
	}
}

func TestCreateRecurringReservation_NoConflict_CancelledRecurring(t *testing.T) {
	courtID := newObjectID()
	hour := 10

	recurringRepo := &mockRecurringReservationRepo{
		FindByCourtHourAndDayFn: func(ctx context.Context, cID primitive.ObjectID, h int, d int) (*domain.RecurringReservation, error) {
			// Simula que la recurrencia cancelada no se encuentra porque filtra por status=active
			return nil, nil
		},
		CreateFn: func(ctx context.Context, r *domain.RecurringReservation) error {
			return nil
		},
	}
	courtRepo := &mockCourtRepoForRecurring{}
	centerRepo := &mockCenterRepoForRecurring{}

	uc := &BookingUseCase{
		recurringReservationRepo: recurringRepo,
		courtRepo:                courtRepo,
		centerRepo:               centerRepo,
	}

	date := time.Date(2026, 7, 21, 0, 0, 0, 0, time.UTC)
	reservation := &domain.RecurringReservation{
		CourtID:       courtID,
		Hour:          hour,
		CustomerName:  "Test User",
		CustomerPhone: "123456789",
		Price:         30000,
	}

	err := uc.CreateRecurringReservation(context.Background(), reservation, date)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if recurringRepo.CreateCalls != 1 {
		t.Errorf("expected 1 Create call, got %d", recurringRepo.CreateCalls)
	}
}

func TestCreateRecurringReservation_NoConflict_NoExisting(t *testing.T) {
	courtID := newObjectID()
	hour := 10

	recurringRepo := &mockRecurringReservationRepo{
		FindByCourtHourAndDayFn: func(ctx context.Context, cID primitive.ObjectID, h int, d int) (*domain.RecurringReservation, error) {
			return nil, nil
		},
		CreateFn: func(ctx context.Context, r *domain.RecurringReservation) error {
			return nil
		},
	}
	courtRepo := &mockCourtRepoForRecurring{}
	centerRepo := &mockCenterRepoForRecurring{}

	uc := &BookingUseCase{
		recurringReservationRepo: recurringRepo,
		courtRepo:                courtRepo,
		centerRepo:               centerRepo,
	}

	date := time.Date(2026, 7, 21, 0, 0, 0, 0, time.UTC)
	reservation := &domain.RecurringReservation{
		CourtID:       courtID,
		Hour:          hour,
		CustomerName:  "Test User",
		CustomerPhone: "123456789",
		Price:         30000,
	}

	err := uc.CreateRecurringReservation(context.Background(), reservation, date)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if recurringRepo.CreateCalls != 1 {
		t.Errorf("expected 1 Create call, got %d", recurringRepo.CreateCalls)
	}
}

func TestCreateRecurringReservation_NoConflict_DifferentDay(t *testing.T) {
	courtID := newObjectID()
	hour := 10

	recurringRepo := &mockRecurringReservationRepo{
		FindByCourtHourAndDayFn: func(ctx context.Context, cID primitive.ObjectID, h int, d int) (*domain.RecurringReservation, error) {
			return nil, nil
		},
		CreateFn: func(ctx context.Context, r *domain.RecurringReservation) error {
			return nil
		},
	}
	courtRepo := &mockCourtRepoForRecurring{}
	centerRepo := &mockCenterRepoForRecurring{}

	uc := &BookingUseCase{
		recurringReservationRepo: recurringRepo,
		courtRepo:                courtRepo,
		centerRepo:               centerRepo,
	}

	// Miércoles (dayOfWeek=3) mientras que la existente es martes (dayOfWeek=2)
	date := time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC)
	reservation := &domain.RecurringReservation{
		CourtID:       courtID,
		Hour:          hour,
		CustomerName:  "Test User",
		CustomerPhone: "123456789",
		Price:         30000,
	}

	err := uc.CreateRecurringReservation(context.Background(), reservation, date)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if recurringRepo.CreateCalls != 1 {
		t.Errorf("expected 1 Create call, got %d", recurringRepo.CreateCalls)
	}
}

func TestCreateRecurringReservation_NoConflict_DifferentHour(t *testing.T) {
	courtID := newObjectID()

	recurringRepo := &mockRecurringReservationRepo{
		FindByCourtHourAndDayFn: func(ctx context.Context, cID primitive.ObjectID, h int, d int) (*domain.RecurringReservation, error) {
			return nil, nil
		},
		CreateFn: func(ctx context.Context, r *domain.RecurringReservation) error {
			return nil
		},
	}
	courtRepo := &mockCourtRepoForRecurring{}
	centerRepo := &mockCenterRepoForRecurring{}

	uc := &BookingUseCase{
		recurringReservationRepo: recurringRepo,
		courtRepo:                courtRepo,
		centerRepo:               centerRepo,
	}

	// 11:00, mientras que la existente es a las 10:00
	date := time.Date(2026, 7, 21, 0, 0, 0, 0, time.UTC)
	reservation := &domain.RecurringReservation{
		CourtID:       courtID,
		Hour:          11,
		CustomerName:  "Test User",
		CustomerPhone: "123456789",
		Price:         30000,
	}

	err := uc.CreateRecurringReservation(context.Background(), reservation, date)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if recurringRepo.CreateCalls != 1 {
		t.Errorf("expected 1 Create call, got %d", recurringRepo.CreateCalls)
	}
}
