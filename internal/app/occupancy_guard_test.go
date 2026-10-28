package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
)

func occupancyCreateFixture(t *testing.T) (*BookingUseCase, *mockBookingRepoForCreate, *MemoryCoordinator, domain.Court) {
	t.Helper()
	courtID := newObjectID()
	court := domain.Court{
		ID:            courtID,
		SportCenterID: newObjectID(),
		Name:          "Cancha 1",
		Schedule:      []domain.CourtSchedule{availableSlot(10, 30000)},
	}
	repo := &mockBookingRepoForCreate{mockBookingRepoForHold: &mockBookingRepoForHold{}}
	uc := newCreateBookingUseCase(&court, repo, &mockSlotHoldRepo{})
	coord := NewMemoryCoordinator(24 * time.Hour)
	uc.SetOccupancyCoordinator(coord)
	return uc, repo, coord, court
}

func futureBooking(court domain.Court) *domain.Booking {
	return &domain.Booking{CourtID: court.ID, Hour: 10, Date: time.Now().AddDate(0, 0, 3)}
}

func TestCreate_CoordinatorBlocksSecondBooking(t *testing.T) {
	uc, repo, _, court := occupancyCreateFixture(t)
	ctx := context.Background()

	first := futureBooking(court)
	if err := uc.Create(ctx, first); err != nil {
		t.Fatalf("first: %v", err)
	}
	if first.OccupancyID == "" || first.OccupancyToken == "" {
		t.Fatal("booking must be linked to its occupancy")
	}

	// The Mongo mock reports no conflict: only the coordinator can stop this.
	err := uc.Create(ctx, futureBooking(court))
	var appErr *domain.AppError
	if !errors.As(err, &appErr) || appErr.StatusCode != 409 {
		t.Fatalf("want 409 conflict, got %v", err)
	}
	if len(repo.Created) != 1 {
		t.Fatalf("created = %d, want 1", len(repo.Created))
	}
}

func TestCreate_FailedInsertReleasesOccupancy(t *testing.T) {
	uc, repo, _, court := occupancyCreateFixture(t)
	ctx := context.Background()

	repo.CreateErr = errors.New("mongo down")
	if err := uc.Create(ctx, futureBooking(court)); err == nil {
		t.Fatal("expected insert error")
	}

	repo.CreateErr = nil
	if err := uc.Create(ctx, futureBooking(court)); err != nil {
		t.Fatalf("slot must be free after failed insert: %v", err)
	}
}

func TestCancel_ReleasesOccupancy(t *testing.T) {
	uc, _, coord, court := occupancyCreateFixture(t)
	ctx := context.Background()

	b := futureBooking(court)
	if err := uc.Create(ctx, b); err != nil {
		t.Fatal(err)
	}
	uc.releaseOccupancy(ctx, b)
	if _, err := coord.Inspect(ctx, b.OccupancyID); !errors.Is(err, ErrOccupancyNotFound) {
		t.Fatalf("occupancy should be gone: %v", err)
	}
}

func TestCreate_WithoutCoordinatorKeepsLegacyBehavior(t *testing.T) {
	uc, _, _, court := occupancyCreateFixture(t)
	uc.SetOccupancyCoordinator(nil)
	b := futureBooking(court)
	if err := uc.Create(context.Background(), b); err != nil {
		t.Fatal(err)
	}
	if b.OccupancyID != "" {
		t.Fatal("no occupancy expected when coordinator is disabled")
	}
}

func pendingBooking(t *testing.T, court domain.Court) *domain.Booking {
	t.Helper()
	b := &domain.Booking{
		CourtID: court.ID, SportCenterID: court.SportCenterID,
		Hour: 10, Date: time.Now().AddDate(0, 0, 3), UserID: "user-1",
	}
	if err := b.PrepareForWrite(nil); err != nil {
		t.Fatal(err)
	}
	return b
}

func TestHoldOccupancy_RenewsInsteadOfDuplicating(t *testing.T) {
	uc, _, coord, court := occupancyCreateFixture(t)
	ctx := context.Background()
	b := pendingBooking(t, court)

	if err := uc.holdOccupancy(ctx, b, mpHoldTTL); err != nil {
		t.Fatal(err)
	}
	firstID := b.OccupancyID
	if err := uc.holdOccupancy(ctx, b, mpHoldTTL); err != nil {
		t.Fatalf("retry by same booking must renew: %v", err)
	}
	if b.OccupancyID != firstID {
		t.Fatal("renew must keep the same occupancy")
	}
	if _, err := coord.Inspect(ctx, firstID); err != nil {
		t.Fatal(err)
	}
}

func TestConfirm_PaymentWithinHold(t *testing.T) {
	uc, _, coord, court := occupancyCreateFixture(t)
	ctx := context.Background()
	b := pendingBooking(t, court)
	if err := uc.holdOccupancy(ctx, b, mpHoldTTL); err != nil {
		t.Fatal(err)
	}

	if err := uc.beginConfirm(ctx, b); err != nil {
		t.Fatal(err)
	}
	// Webhook retry while committing must be tolerated.
	if err := uc.beginConfirm(ctx, b); err != nil {
		t.Fatalf("retry: %v", err)
	}
	uc.finishConfirm(ctx, b)

	occ, err := coord.Inspect(ctx, b.OccupancyID)
	if err != nil || occ.State != OccupancyBooked {
		t.Fatalf("occupancy = %+v, %v", occ, err)
	}
}

func TestConfirm_LatePaymentReacquiresWhenFree(t *testing.T) {
	uc, _, coord, court := occupancyCreateFixture(t)
	ctx := context.Background()
	now := time.Now()
	coord.SetClock(func() time.Time { return now })

	b := pendingBooking(t, court)
	if err := uc.holdOccupancy(ctx, b, mpHoldTTL); err != nil {
		t.Fatal(err)
	}
	oldID := b.OccupancyID

	now = now.Add(20 * time.Minute)
	if err := uc.beginConfirm(ctx, b); err != nil {
		t.Fatalf("free slot must be re-acquired: %v", err)
	}
	if b.OccupancyID == oldID {
		t.Fatal("expected a new occupancy after expiry")
	}
}

func TestConfirm_LatePaymentConflictWhenTaken(t *testing.T) {
	uc, _, coord, court := occupancyCreateFixture(t)
	ctx := context.Background()
	now := time.Now()
	coord.SetClock(func() time.Time { return now })

	late := pendingBooking(t, court)
	if err := uc.holdOccupancy(ctx, late, mpHoldTTL); err != nil {
		t.Fatal(err)
	}

	now = now.Add(20 * time.Minute)
	other := pendingBooking(t, court)
	other.UserID = "user-2"
	if err := uc.holdOccupancy(ctx, other, mpHoldTTL); err != nil {
		t.Fatalf("other user takes the expired slot: %v", err)
	}

	if err := uc.beginConfirm(ctx, late); !errors.Is(err, ErrLatePaymentConflict) {
		t.Fatalf("want ErrLatePaymentConflict, got %v", err)
	}
}

func bookedAt(t *testing.T, uc *BookingUseCase, court domain.Court, hour int, user string) *domain.Booking {
	t.Helper()
	b := &domain.Booking{
		CourtID: court.ID, SportCenterID: court.SportCenterID, UserID: user,
		Hour: hour, Date: time.Now().AddDate(0, 0, 5), Status: domain.BookingStatusConfirmed,
	}
	if err := uc.persistWithOccupancy(context.Background(), b, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	return b
}

func movedTo(orig *domain.Booking, hour int) *domain.Booking {
	m := *orig
	m.Hour = hour
	m.ScheduledAt = time.Time{}
	m.EndsAt = time.Time{}
	m.ScheduleSchemaVersion = 0
	return &m
}

func TestMoveOccupancy_ShiftOverlappingItself(t *testing.T) {
	uc, _, coord, court := occupancyCreateFixture(t)
	ctx := context.Background()
	orig := bookedAt(t, uc, court, 10, "u1")
	moved := movedTo(orig, 10)
	moved.Minutes = 30 // 10:30-11:30 overlaps own 10:00-11:00

	if err := uc.moveOccupancy(ctx, orig, moved, func() error { return nil }); err != nil {
		t.Fatalf("self-overlapping move: %v", err)
	}
	if moved.OccupancyID == orig.OccupancyID {
		t.Fatal("expected a new occupancy")
	}
	if _, err := coord.Inspect(ctx, orig.OccupancyID); !errors.Is(err, ErrOccupancyNotFound) {
		t.Fatalf("old occupancy should be gone: %v", err)
	}
}

func TestMoveOccupancy_ConflictKeepsOriginal(t *testing.T) {
	uc, _, coord, court := occupancyCreateFixture(t)
	ctx := context.Background()
	orig := bookedAt(t, uc, court, 10, "u1")
	bookedAt(t, uc, court, 14, "u2")

	err := uc.moveOccupancy(ctx, orig, movedTo(orig, 14), func() error { return nil })
	if !isConflict(err) {
		t.Fatalf("want conflict, got %v", err)
	}
	if occ, err := coord.Inspect(ctx, orig.OccupancyID); err != nil || occ.State != OccupancyBooked {
		t.Fatalf("original must remain booked: %+v %v", occ, err)
	}
}

func TestMoveOccupancy_PersistFailureRestoresOriginal(t *testing.T) {
	uc, _, coord, court := occupancyCreateFixture(t)
	ctx := context.Background()
	orig := bookedAt(t, uc, court, 10, "u1")
	moved := movedTo(orig, 10)
	moved.Minutes = 30

	err := uc.moveOccupancy(ctx, orig, moved, func() error { return errors.New("mongo down") })
	if err == nil {
		t.Fatal("expected persist error")
	}
	// The new interval must be free again and the old one blocked again.
	other := &domain.Booking{CourtID: court.ID, SportCenterID: court.SportCenterID, Hour: 10, Date: orig.Date}
	if err := uc.acquireOccupancy(ctx, other, time.Minute); !isConflict(err) {
		t.Fatalf("original interval must be blocked after restore: %v", err)
	}
	_ = coord
}

// downCoordinator simulates an unreachable Redis.
type downCoordinator struct{ *MemoryCoordinator }

var errRedisDown = errors.New("dial tcp: connection refused")

func (downCoordinator) Acquire(context.Context, AcquireRequest) (*Occupancy, error) {
	return nil, errRedisDown
}
func (downCoordinator) BeginCommit(context.Context, string, string) error { return errRedisDown }
func (downCoordinator) Release(context.Context, string, string) error     { return errRedisDown }
func (downCoordinator) Renew(context.Context, string, string, time.Duration) error {
	return errRedisDown
}
func (downCoordinator) Inspect(context.Context, string) (*Occupancy, error) { return nil, errRedisDown }

func TestRedisOutage_RejectsWritesWithoutTouchingMongo(t *testing.T) {
	uc, repo, _, court := occupancyCreateFixture(t)
	uc.SetOccupancyCoordinator(downCoordinator{NewMemoryCoordinator(time.Hour)})

	err := uc.Create(context.Background(), futureBooking(court))
	var appErr *domain.AppError
	if !errors.As(err, &appErr) || appErr.StatusCode != 503 {
		t.Fatalf("want 503, got %v", err)
	}
	if len(repo.Created) != 0 {
		t.Fatal("no booking may be written while the coordinator is down")
	}
}

func TestRedisOutage_PaymentConfirmationFailsSoWebhookIsRetried(t *testing.T) {
	uc, _, coord, court := occupancyCreateFixture(t)
	b := pendingBooking(t, court)
	if err := uc.holdOccupancy(context.Background(), b, mpHoldTTL); err != nil {
		t.Fatal(err)
	}
	uc.SetOccupancyCoordinator(downCoordinator{coord})

	if err := uc.beginConfirm(context.Background(), b); err == nil {
		t.Fatal("confirmation must fail while Redis is down so MercadoPago retries the webhook")
	}
}

func TestRedisOutage_CancelStillSucceedsAndLeavesCleanupToReconciler(t *testing.T) {
	uc, _, coord, court := occupancyCreateFixture(t)
	b := futureBooking(court)
	if err := uc.Create(context.Background(), b); err != nil {
		t.Fatal(err)
	}
	uc.SetOccupancyCoordinator(downCoordinator{coord})

	uc.releaseOccupancy(context.Background(), b) // must not panic nor block
	if _, err := coord.Inspect(context.Background(), b.OccupancyID); err != nil {
		t.Fatalf("occupancy remains for the reconciler to clean: %v", err)
	}
}
