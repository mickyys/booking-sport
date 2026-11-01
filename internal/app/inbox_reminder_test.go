package app

import (
	"context"
	"testing"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type fakeInbox struct{ msgs []*domain.InboxMessage }

func (f *fakeInbox) Insert(_ context.Context, m *domain.InboxMessage) error {
	for _, e := range f.msgs {
		if e.UserID == m.UserID && e.DedupeKey == m.DedupeKey {
			return nil
		}
	}
	f.msgs = append(f.msgs, m)
	return nil
}
func (f *fakeInbox) List(context.Context, string, string, int) ([]domain.InboxMessage, error) {
	return nil, nil
}
func (f *fakeInbox) CountUnread(context.Context, string) (int64, error) { return 0, nil }
func (f *fakeInbox) MarkRead(context.Context, string, string) (bool, error) {
	return false, nil
}
func (f *fakeInbox) MarkAllRead(context.Context, string) error            { return nil }
func (f *fakeInbox) Delete(context.Context, string, string) (bool, error) { return false, nil }

type fakeFinder struct{ bookings []domain.Booking }

func (f fakeFinder) FindConfirmedWithUserBetween(context.Context, time.Time, time.Time) ([]domain.Booking, error) {
	return f.bookings, nil
}

func TestSendBookingReminders(t *testing.T) {
	now := time.Date(2026, 10, 31, 15, 0, 0, 0, time.UTC)
	start := now.Add(90 * time.Minute)
	old := domain.Booking{
		ID: primitive.NewObjectID(), UserID: "u1", SportCenterName: "Andes",
		ScheduledAt: start, CreatedAt: now.Add(-48 * time.Hour),
	}
	fresh := domain.Booking{
		ID: primitive.NewObjectID(), UserID: "u2", SportCenterName: "Andes",
		ScheduledAt: start, CreatedAt: now.Add(-10 * time.Minute),
	}
	inbox := &fakeInbox{}
	uc := &BookingUseCase{inbox: inbox, reminders: fakeFinder{[]domain.Booking{old, fresh}}}

	for i := 0; i < 2; i++ { // idempotente
		sum, err := uc.SendBookingReminders(context.Background(), now, 2*time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		if sum.Scanned != 2 || sum.Skipped != 1 {
			t.Fatalf("summary inesperado: %+v", sum)
		}
	}
	if len(inbox.msgs) != 1 || inbox.msgs[0].UserID != "u1" || inbox.msgs[0].Type != domain.InboxBookingReminder {
		t.Fatalf("mensajes inesperados: %+v", inbox.msgs)
	}
}
