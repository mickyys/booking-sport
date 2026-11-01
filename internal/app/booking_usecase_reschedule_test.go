package app

import (
	"context"
	"testing"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// mockBookingRepoForReschedule extiende el mock de holds para registrar los
// bookings persistidos por RescheduleBooking.
type mockBookingRepoForReschedule struct {
	*mockBookingRepoForHold
	Updated []*domain.Booking
}

func (m *mockBookingRepoForReschedule) Update(ctx context.Context, booking *domain.Booking) error {
	m.Updated = append(m.Updated, booking)
	return nil
}

func newRescheduleUseCase(repo *mockBookingRepoForReschedule, recurringRepo RecurringReservationRepository) *BookingUseCase {
	return &BookingUseCase{
		repo:                     repo,
		holdRepo:                 &mockSlotHoldRepo{},
		courtRepo:                &mockCourtRepoForRecurring{},
		recurringReservationRepo: recurringRepo,
	}
}

func TestRescheduleBooking_MovesConfirmedBooking(t *testing.T) {
	loc := domain.GetSantiagoLocation()
	courtID := newObjectID()
	bookingID := newObjectID()
	originalDate := time.Now().In(loc).AddDate(0, 0, 2)

	booking := &domain.Booking{
		ID:              bookingID,
		CourtID:         courtID,
		SportCenterID:   newObjectID(),
		Status:          domain.BookingStatusConfirmed,
		Date:            originalDate,
		Hour:            10,
		Minutes:         0,
		DurationMinutes: 60,
	}

	repo := &mockBookingRepoForReschedule{
		mockBookingRepoForHold: &mockBookingRepoForHold{
			FindByIDFn: func(ctx context.Context, id primitive.ObjectID) (*domain.Booking, error) {
				return booking, nil
			},
		},
	}
	uc := newRescheduleUseCase(repo, &mockRecurringReservationRepo{})

	targetDate := time.Now().In(loc).AddDate(0, 0, 5).Format("2006-01-02")
	updated, err := uc.RescheduleBooking(context.Background(), bookingID, targetDate, 15, 30, nil, "auth0|admin")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if updated.Hour != 15 || updated.Minutes != 30 {
		t.Fatalf("expected 15:30, got %02d:%02d", updated.Hour, updated.Minutes)
	}
	if updated.Date.In(loc).Format("2006-01-02") != targetDate {
		t.Fatalf("expected date %s, got %s", targetDate, updated.Date.In(loc).Format("2006-01-02"))
	}
	if updated.ModifiedBy != "auth0|admin" {
		t.Errorf("expected modified_by auth0|admin, got %q", updated.ModifiedBy)
	}
	if updated.ModifiedAt == nil {
		t.Error("expected ModifiedAt to be set")
	}
	if len(repo.Updated) != 1 {
		t.Fatalf("expected 1 persisted booking, got %d", len(repo.Updated))
	}
}

func TestRescheduleBooking_RejectsPastSlot(t *testing.T) {
	loc := domain.GetSantiagoLocation()
	bookingID := newObjectID()
	booking := &domain.Booking{
		ID:              bookingID,
		CourtID:         newObjectID(),
		SportCenterID:   newObjectID(),
		Status:          domain.BookingStatusConfirmed,
		Date:            time.Now().In(loc),
		Hour:            10,
		DurationMinutes: 60,
	}
	repo := &mockBookingRepoForReschedule{
		mockBookingRepoForHold: &mockBookingRepoForHold{
			FindByIDFn: func(ctx context.Context, id primitive.ObjectID) (*domain.Booking, error) {
				return booking, nil
			},
		},
	}
	uc := newRescheduleUseCase(repo, &mockRecurringReservationRepo{})

	pastDate := time.Now().In(loc).AddDate(0, 0, -1).Format("2006-01-02")
	_, err := uc.RescheduleBooking(context.Background(), bookingID, pastDate, 10, 0, nil, "auth0|admin")
	assertErrorContains(t, err, "pasado")

	if len(repo.Updated) != 0 {
		t.Fatalf("expected no persisted bookings, got %d", len(repo.Updated))
	}
}

func TestRescheduleBooking_RejectsNonConfirmedBooking(t *testing.T) {
	bookingID := newObjectID()
	booking := &domain.Booking{
		ID:     bookingID,
		Status: domain.BookingStatusCancelled,
	}
	repo := &mockBookingRepoForReschedule{
		mockBookingRepoForHold: &mockBookingRepoForHold{
			FindByIDFn: func(ctx context.Context, id primitive.ObjectID) (*domain.Booking, error) {
				return booking, nil
			},
		},
	}
	uc := newRescheduleUseCase(repo, &mockRecurringReservationRepo{})

	_, err := uc.RescheduleBooking(context.Background(), bookingID, "2030-01-01", 10, 0, nil, "auth0|admin")
	assertErrorContains(t, err, "confirmadas")
}

func TestRescheduleBooking_RejectsRecurringConflict(t *testing.T) {
	loc := domain.GetSantiagoLocation()
	bookingID := newObjectID()
	booking := &domain.Booking{
		ID:              bookingID,
		CourtID:         newObjectID(),
		SportCenterID:   newObjectID(),
		Status:          domain.BookingStatusConfirmed,
		Date:            time.Now().In(loc),
		Hour:            10,
		DurationMinutes: 60,
	}
	repo := &mockBookingRepoForReschedule{
		mockBookingRepoForHold: &mockBookingRepoForHold{
			FindByIDFn: func(ctx context.Context, id primitive.ObjectID) (*domain.Booking, error) {
				return booking, nil
			},
		},
	}
	recurringRepo := &mockRecurringReservationRepo{
		FindByCourtHourAndDayFn: func(ctx context.Context, courtID primitive.ObjectID, hour int, minutes int, dayOfWeek int) (*domain.RecurringReservation, error) {
			return &domain.RecurringReservation{}, nil
		},
	}
	uc := newRescheduleUseCase(repo, recurringRepo)

	targetDate := time.Now().In(loc).AddDate(0, 0, 3).Format("2006-01-02")
	_, err := uc.RescheduleBooking(context.Background(), bookingID, targetDate, 11, 0, nil, "auth0|admin")
	assertErrorContains(t, err, "recurrente")

	if len(repo.Updated) != 0 {
		t.Fatalf("expected no persisted bookings, got %d", len(repo.Updated))
	}
}
