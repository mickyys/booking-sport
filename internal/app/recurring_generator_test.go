package app

import (
	"context"
	"testing"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func newRecurringGeneratorUseCase(bookingRepo *mockBookingRepoForHold, court *domain.Court) *BookingUseCase {
	return &BookingUseCase{
		repo:       bookingRepo,
		holdRepo:   &mockSlotHoldRepo{},
		courtRepo:  &mockCourtRepoForRecurring{FindByIDFn: func(ctx context.Context, id primitive.ObjectID) (*domain.Court, error) { return court, nil }},
		centerRepo: &mockCenterRepoForRecurring{},
	}
}

func TestFirstRecurringOccurrence(t *testing.T) {
	loc := domain.GetSantiagoLocation()
	base := time.Date(2026, 11, 2, 12, 0, 0, 0, loc) // lunes

	got, ok := firstRecurringOccurrence(base, int(time.Monday))
	if !ok {
		t.Fatal("expected occurrence")
	}
	if got.Format("2006-01-02") != "2026-11-02" {
		t.Fatalf("expected 2026-11-02, got %s", got.Format("2006-01-02"))
	}

	got, ok = firstRecurringOccurrence(base, int(time.Thursday))
	if !ok {
		t.Fatal("expected occurrence")
	}
	if got.Format("2006-01-02") != "2026-11-05" {
		t.Fatalf("expected 2026-11-05, got %s", got.Format("2006-01-02"))
	}
}

func TestGenerateForRecurringCreatesFourFutureOccurrences(t *testing.T) {
	loc := domain.GetSantiagoLocation()
	now := time.Date(2026, 11, 2, 6, 0, 0, 0, loc) // lunes 06:00
	courtID := newObjectID()
	court := &domain.Court{
		ID:            courtID,
		SportCenterID: newObjectID(),
		Name:          "Cancha 1",
		Schedule:      []domain.CourtSchedule{{Hour: 10, Minutes: 0, Price: 30000, Status: "available"}},
	}

	var created []*domain.Booking
	bookingRepo := &mockBookingRepoForHold{
		CreateRecurringBookingFn: func(ctx context.Context, booking *domain.Booking) error {
			created = append(created, booking)
			return nil
		},
	}
	uc := newRecurringGeneratorUseCase(bookingRepo, court)

	reservation := &domain.RecurringReservation{
		ID:              newObjectID(),
		CourtID:         courtID,
		CustomerName:    "Cliente Semanal",
		CustomerPhone:   "+56911111111",
		Hour:            10,
		Minutes:         0,
		DurationMinutes: 60,
		Price:           28000,
		DayOfWeek:       int(time.Monday),
		StartDate:       time.Date(2026, 11, 2, 12, 0, 0, 0, loc),
	}

	result := uc.generateForRecurring(context.Background(), reservation, now)
	if result.Created != 4 {
		t.Fatalf("expected 4 created, got %d (issues: %+v)", result.Created, result.Issues)
	}
	if result.Failed != 0 || result.Skipped != 0 {
		t.Fatalf("expected no failures/skips, got failed=%d skipped=%d", result.Failed, result.Skipped)
	}

	wantDates := []string{"2026-11-02", "2026-11-09", "2026-11-16", "2026-11-23"}
	if len(created) != len(wantDates) {
		t.Fatalf("expected %d bookings, got %d", len(wantDates), len(created))
	}
	for i, booking := range created {
		if got := booking.Date.Format("2006-01-02"); got != wantDates[i] {
			t.Errorf("booking %d: expected date %s, got %s", i, wantDates[i], got)
		}
		if booking.RecurringID != reservation.ID.Hex() {
			t.Errorf("booking %d: expected recurring_id %s, got %s", i, reservation.ID.Hex(), booking.RecurringID)
		}
		if booking.SeriesID != "" {
			t.Errorf("booking %d: generated bookings must not set series_id", i)
		}
		if booking.PaymentMethod != "internal" {
			t.Errorf("booking %d: expected internal payment method, got %s", i, booking.PaymentMethod)
		}
		if booking.Status != domain.BookingStatusConfirmed {
			t.Errorf("booking %d: expected confirmed status, got %s", i, booking.Status)
		}
		if booking.Price != reservation.Price || booking.FinalPrice != reservation.Price {
			t.Errorf("booking %d: expected series price %.0f, got %.0f/%.0f", i, reservation.Price, booking.Price, booking.FinalPrice)
		}
		if booking.CustomerName != reservation.CustomerName {
			t.Errorf("booking %d: expected customer %s, got %s", i, reservation.CustomerName, booking.CustomerName)
		}
	}
}

func TestGenerateForRecurringSkipsCancelledDate(t *testing.T) {
	loc := domain.GetSantiagoLocation()
	now := time.Date(2026, 11, 2, 6, 0, 0, 0, loc)
	courtID := newObjectID()
	court := &domain.Court{
		ID:            courtID,
		SportCenterID: newObjectID(),
		Name:          "Cancha 1",
		Schedule:      []domain.CourtSchedule{{Hour: 10, Minutes: 0, Price: 30000, Status: "available"}},
	}

	bookingRepo := &mockBookingRepoForHold{
		CreateRecurringBookingFn: func(ctx context.Context, booking *domain.Booking) error { return nil },
	}
	uc := newRecurringGeneratorUseCase(bookingRepo, court)

	reservation := &domain.RecurringReservation{
		ID:             newObjectID(),
		CourtID:        courtID,
		Hour:           10,
		DayOfWeek:      int(time.Monday),
		StartDate:      time.Date(2026, 11, 2, 12, 0, 0, 0, loc),
		CancelledDates: []string{"2026-11-09"},
	}

	result := uc.generateForRecurring(context.Background(), reservation, now)
	if result.Created != 3 || result.Skipped != 1 {
		t.Fatalf("expected 3 created and 1 skipped, got created=%d skipped=%d (issues: %+v)", result.Created, result.Skipped, result.Issues)
	}
	if len(result.Issues) != 1 || result.Issues[0].Date != "2026-11-09" {
		t.Fatalf("expected issue for cancelled date, got %+v", result.Issues)
	}
}

func TestGenerateForRecurringSkipsConflictAndContinues(t *testing.T) {
	loc := domain.GetSantiagoLocation()
	now := time.Date(2026, 11, 2, 6, 0, 0, 0, loc)
	courtID := newObjectID()
	court := &domain.Court{
		ID:            courtID,
		SportCenterID: newObjectID(),
		Name:          "Cancha 1",
		Schedule:      []domain.CourtSchedule{{Hour: 10, Minutes: 0, Price: 30000, Status: "available"}},
	}

	var created []*domain.Booking
	bookingRepo := &mockBookingRepoForHold{
		FindConfirmedBySlotFn: func(ctx context.Context, cID primitive.ObjectID, date time.Time, hour int, minutes int) (*domain.Booking, error) {
			if domain.SantiagoCivilDate(date).Format("2006-01-02") == "2026-11-09" {
				return &domain.Booking{ID: newObjectID()}, nil
			}
			return nil, nil
		},
		CreateRecurringBookingFn: func(ctx context.Context, booking *domain.Booking) error {
			created = append(created, booking)
			return nil
		},
	}
	uc := newRecurringGeneratorUseCase(bookingRepo, court)

	reservation := &domain.RecurringReservation{
		ID:        newObjectID(),
		CourtID:   courtID,
		Hour:      10,
		DayOfWeek: int(time.Monday),
		StartDate: time.Date(2026, 11, 2, 12, 0, 0, 0, loc),
	}

	result := uc.generateForRecurring(context.Background(), reservation, now)
	if result.Created != 3 || result.Skipped != 1 {
		t.Fatalf("expected 3 created and 1 skipped, got created=%d skipped=%d", result.Created, result.Skipped)
	}
	for _, booking := range created {
		if booking.Date.Format("2006-01-02") == "2026-11-09" {
			t.Fatal("conflicting date must not be created")
		}
	}
}

func TestGenerateForRecurringIsIdempotent(t *testing.T) {
	loc := domain.GetSantiagoLocation()
	now := time.Date(2026, 11, 2, 6, 0, 0, 0, loc)
	courtID := newObjectID()
	court := &domain.Court{
		ID:            courtID,
		SportCenterID: newObjectID(),
		Name:          "Cancha 1",
		Schedule:      []domain.CourtSchedule{{Hour: 10, Minutes: 0, Price: 30000, Status: "available"}},
	}

	createCalls := 0
	bookingRepo := &mockBookingRepoForHold{
		FindByRecurringIDAndDateFn: func(ctx context.Context, recurringID string, date time.Time) (*domain.Booking, error) {
			return &domain.Booking{ID: newObjectID()}, nil
		},
		CreateRecurringBookingFn: func(ctx context.Context, booking *domain.Booking) error {
			createCalls++
			return nil
		},
	}
	uc := newRecurringGeneratorUseCase(bookingRepo, court)

	reservation := &domain.RecurringReservation{
		ID:        newObjectID(),
		CourtID:   courtID,
		Hour:      10,
		DayOfWeek: int(time.Monday),
		StartDate: time.Date(2026, 11, 2, 12, 0, 0, 0, loc),
	}

	result := uc.generateForRecurring(context.Background(), reservation, now)
	if result.Created != 0 || result.AlreadyExists != 4 {
		t.Fatalf("expected 0 created and 4 existing, got created=%d existing=%d", result.Created, result.AlreadyExists)
	}
	if createCalls != 0 {
		t.Fatalf("expected no create calls, got %d", createCalls)
	}
}

func TestGenerateForRecurringSkipsClosedLegacySlot(t *testing.T) {
	loc := domain.GetSantiagoLocation()
	now := time.Date(2026, 11, 2, 6, 0, 0, 0, loc)
	courtID := newObjectID()
	court := &domain.Court{
		ID:            courtID,
		SportCenterID: newObjectID(),
		Name:          "Cancha 1",
		Schedule:      []domain.CourtSchedule{{Hour: 10, Minutes: 0, Price: 30000, Status: "closed"}},
	}

	bookingRepo := &mockBookingRepoForHold{}
	uc := newRecurringGeneratorUseCase(bookingRepo, court)

	reservation := &domain.RecurringReservation{
		ID:        newObjectID(),
		CourtID:   courtID,
		Hour:      10,
		DayOfWeek: int(time.Monday),
		StartDate: time.Date(2026, 11, 2, 12, 0, 0, 0, loc),
	}

	result := uc.generateForRecurring(context.Background(), reservation, now)
	if result.Created != 0 || result.Skipped != 4 {
		t.Fatalf("expected 0 created and 4 skipped, got created=%d skipped=%d", result.Created, result.Skipped)
	}
}
