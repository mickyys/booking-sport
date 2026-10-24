package app

import (
	"context"
	"testing"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type fakeReconcileStore struct {
	linked  []LinkedBooking
	cleared []primitive.ObjectID
}

func (f *fakeReconcileStore) FindLinkedBookings(context.Context, int64) ([]LinkedBooking, error) {
	return f.linked, nil
}
func (f *fakeReconcileStore) ClearOccupancyLink(_ context.Context, id primitive.ObjectID) error {
	f.cleared = append(f.cleared, id)
	return nil
}

func TestReconciler(t *testing.T) {
	ctx := context.Background()
	coord := NewMemoryCoordinator(24 * time.Hour)
	center, space := primitive.NewObjectID(), primitive.NewObjectID()
	base := time.Now().Add(48 * time.Hour).Truncate(time.Hour)
	acquire := func(offset int) *Occupancy {
		start := base.Add(time.Duration(offset) * time.Hour)
		occ, err := coord.Acquire(ctx, AcquireRequest{CenterID: center, SpaceID: space, Start: start, End: start.Add(time.Hour), TTL: time.Hour})
		if err != nil {
			t.Fatal(err)
		}
		return occ
	}

	committing := acquire(0)
	_ = coord.BeginCommit(ctx, committing.ID, committing.Token)
	cancelled := acquire(2)
	healthy := acquire(4)
	_ = coord.BeginCommit(ctx, healthy.ID, healthy.Token)
	_ = coord.Commit(ctx, healthy.ID, healthy.Token, primitive.NewObjectID())

	store := &fakeReconcileStore{linked: []LinkedBooking{
		{ID: primitive.NewObjectID(), Status: domain.BookingStatusConfirmed, EndsAt: base.Add(time.Hour), OccupancyID: committing.ID, Token: committing.Token},
		{ID: primitive.NewObjectID(), Status: domain.BookingStatusCancelled, EndsAt: base.Add(3 * time.Hour), OccupancyID: cancelled.ID, Token: cancelled.Token},
		{ID: primitive.NewObjectID(), Status: domain.BookingStatusConfirmed, EndsAt: base.Add(5 * time.Hour), OccupancyID: healthy.ID, Token: healthy.Token},
		{ID: primitive.NewObjectID(), Status: domain.BookingStatusConfirmed, EndsAt: base.Add(9 * time.Hour), OccupancyID: OccupancyID(center, space, "gone")},
	}}

	dry := &OccupancyReconciler{Store: store, Coord: coord}
	report, err := dry.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if report["committing_to_booked"] != 1 || report["released_stale"] != 1 || report["ok"] != 1 || report["confirmed_missing_in_redis"] != 1 {
		t.Fatalf("dry report: %v", report)
	}
	if occ, _ := coord.Inspect(ctx, cancelled.ID); occ == nil {
		t.Fatal("dry run must not release anything")
	}

	apply := &OccupancyReconciler{Store: store, Coord: coord, Apply: true}
	if _, err := apply.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if occ, _ := coord.Inspect(ctx, committing.ID); occ == nil || occ.State != OccupancyBooked {
		t.Fatalf("committing must become booked: %+v", occ)
	}
	if _, err := coord.Inspect(ctx, cancelled.ID); err == nil {
		t.Fatal("cancelled occupancy must be released")
	}
	if len(store.cleared) != 1 || store.cleared[0] != store.linked[1].ID {
		t.Fatalf("link of released booking must be cleared: %v", store.cleared)
	}
}

type denyLocks struct{}

func (denyLocks) TryAcquire(context.Context, string, string, time.Duration, time.Time) (bool, error) {
	return false, nil
}
func (denyLocks) Release(context.Context, string, string) error { return nil }

func TestReconciler_SkipsWhenLocked(t *testing.T) {
	r := &OccupancyReconciler{Store: &fakeReconcileStore{}, Coord: NewMemoryCoordinator(time.Hour), Locks: denyLocks{}}
	report, err := r.Run(context.Background())
	if err != nil || report["skipped_locked"] != 1 {
		t.Fatalf("report=%v err=%v", report, err)
	}
}
