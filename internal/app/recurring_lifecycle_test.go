package app

import (
	"context"
	"testing"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func TestCancelRecurringReservationCancelsFutureBookings(t *testing.T) {
	centerID := newObjectID()
	recurringID := newObjectID()
	finished := false

	cancelledRecurringID := ""
	cancelledBy := ""
	cancelledReason := ""

	bookingRepo := &mockBookingRepoForHold{
		CancelFutureByRecurringIDFn: func(ctx context.Context, recurringID string, from time.Time, by, reason string) (int64, error) {
			cancelledRecurringID = recurringID
			cancelledBy = by
			cancelledReason = reason
			return 3, nil
		},
	}
	recurringRepo := &mockRecurringReservationRepo{
		FindByIDFn: func(ctx context.Context, id primitive.ObjectID) (*domain.RecurringReservation, error) {
			return &domain.RecurringReservation{
				ID:            id,
				SportCenterID: centerID,
				Status:        domain.RecurringReservationStatusActive,
			}, nil
		},
		FinishFn: func(ctx context.Context, id primitive.ObjectID, by, reason string, finishedAt time.Time) error {
			finished = true
			return nil
		},
	}
	centerRepo := &mockCenterRepoForRecurring{
		FindByUserIDFn: func(ctx context.Context, userID string) ([]domain.SportCenter, error) {
			return []domain.SportCenter{{ID: centerID}}, nil
		},
	}

	uc := &BookingUseCase{
		repo:                     bookingRepo,
		recurringReservationRepo: recurringRepo,
		centerRepo:               centerRepo,
	}

	err := uc.CancelRecurringReservation(context.Background(), recurringID, "auth0|admin", "Fin de temporada")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cancelledRecurringID != recurringID.Hex() {
		t.Fatalf("expected future bookings of %s cancelled, got %q", recurringID.Hex(), cancelledRecurringID)
	}
	if cancelledBy != "auth0|admin" || cancelledReason != "Fin de temporada" {
		t.Fatalf("unexpected cancellation metadata: by=%q reason=%q", cancelledBy, cancelledReason)
	}
	if !finished {
		t.Fatal("expected recurring reservation to be finished")
	}
}

func TestDeleteBookingMarksRecurringDateCancelled(t *testing.T) {
	recurringID := newObjectID()
	bookingID := newObjectID()
	date := domain.SantiagoCivilDate(time.Now().AddDate(0, 0, 7))

	markedDate := ""
	recurringRepo := &mockRecurringReservationRepo{
		AddCancelledDateFn: func(ctx context.Context, id primitive.ObjectID, value string) error {
			if id != recurringID {
				t.Fatalf("unexpected recurring id: %s", id.Hex())
			}
			markedDate = value
			return nil
		},
	}
	bookingRepo := &mockBookingRepoForHold{
		FindByIDFn: func(ctx context.Context, id primitive.ObjectID) (*domain.Booking, error) {
			return &domain.Booking{
				ID:          bookingID,
				RecurringID: recurringID.Hex(),
				Date:        date,
				Status:      domain.BookingStatusConfirmed,
			}, nil
		},
	}

	uc := &BookingUseCase{
		repo:                     bookingRepo,
		recurringReservationRepo: recurringRepo,
	}

	if err := uc.DeleteBooking(context.Background(), bookingID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if markedDate != date.Format("2006-01-02") {
		t.Fatalf("expected cancelled date %s, got %q", date.Format("2006-01-02"), markedDate)
	}
}
