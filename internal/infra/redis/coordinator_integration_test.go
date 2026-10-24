//go:build integration

package redis

import (
	"context"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hamp/booking-sport/internal/app"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func newTestCoordinator(t *testing.T) *Coordinator {
	t.Helper()
	url := os.Getenv("REDIS_URL")
	if url == "" {
		url = "redis://localhost:6379"
	}
	c, err := NewCoordinator(Config{URL: url, KeyPrefix: "test:" + primitive.NewObjectID().Hex()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	if err := c.Health(context.Background()); err != nil {
		t.Skipf("redis not available: %v", err)
	}
	return c
}

type slot struct{ center, space primitive.ObjectID }

func newSlot() slot { return slot{primitive.NewObjectID(), primitive.NewObjectID()} }

func (s slot) req(start time.Time, d time.Duration, ttl time.Duration) app.AcquireRequest {
	return app.AcquireRequest{CenterID: s.center, SpaceID: s.space, Start: start, End: start.Add(d), OwnerRef: "o", TTL: ttl}
}

var t0 = time.Date(2026, 7, 16, 2, 30, 0, 0, time.UTC)

func TestOverlapRules(t *testing.T) {
	c, ctx, s := newTestCoordinator(t), context.Background(), newSlot()

	if _, err := c.Acquire(ctx, s.req(t0, 60*time.Minute, time.Minute)); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name     string
		start    time.Time
		d        time.Duration
		conflict bool
	}{
		{"same", t0, 60 * time.Minute, true},
		{"partial overlap 30m shift", t0.Add(30 * time.Minute), 60 * time.Minute, true},
		{"inside", t0.Add(15 * time.Minute), 15 * time.Minute, true},
		{"contains", t0.Add(-30 * time.Minute), 3 * time.Hour, true},
		{"adjacent after", t0.Add(60 * time.Minute), 60 * time.Minute, false},
		{"adjacent before", t0.Add(-60 * time.Minute), 60 * time.Minute, false},
	}
	for _, tc := range cases {
		_, err := c.Acquire(ctx, s.req(tc.start, tc.d, time.Minute))
		if tc.conflict && !errors.Is(err, app.ErrOccupancyConflict) {
			t.Errorf("%s: want conflict, got %v", tc.name, err)
		}
		if !tc.conflict && err != nil {
			t.Errorf("%s: want ok, got %v", tc.name, err)
		}
	}
}

func TestOtherSpaceIsIndependent(t *testing.T) {
	c, ctx := newTestCoordinator(t), context.Background()
	a, b := newSlot(), newSlot()
	if _, err := c.Acquire(ctx, a.req(t0, time.Hour, time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Acquire(ctx, b.req(t0, time.Hour, time.Minute)); err != nil {
		t.Fatalf("other space must be free: %v", err)
	}
}

func TestHoldExpires(t *testing.T) {
	c, ctx, s := newTestCoordinator(t), context.Background(), newSlot()
	occ, err := c.Acquire(ctx, s.req(t0, time.Hour, 300*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(450 * time.Millisecond)
	if _, err := c.Acquire(ctx, s.req(t0, time.Hour, time.Minute)); err != nil {
		t.Fatalf("expired hold must free the slot: %v", err)
	}
	if err := c.Renew(ctx, occ.ID, occ.Token, time.Minute); !errors.Is(err, app.ErrOccupancyNotFound) {
		t.Fatalf("renew expired: %v", err)
	}
}

func TestLifecycle(t *testing.T) {
	c, ctx, s := newTestCoordinator(t), context.Background(), newSlot()
	occ, err := c.Acquire(ctx, s.req(t0, time.Hour, time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Renew(ctx, occ.ID, "bad", time.Minute); !errors.Is(err, app.ErrOccupancyToken) {
		t.Fatalf("bad token: %v", err)
	}
	if err := c.Commit(ctx, occ.ID, occ.Token, primitive.NewObjectID()); !errors.Is(err, app.ErrOccupancyState) {
		t.Fatalf("commit before begin: %v", err)
	}
	if err := c.BeginCommit(ctx, occ.ID, occ.Token); err != nil {
		t.Fatal(err)
	}
	if err := c.Renew(ctx, occ.ID, occ.Token, time.Minute); !errors.Is(err, app.ErrOccupancyState) {
		t.Fatalf("renew while committing: %v", err)
	}
	bookingID := primitive.NewObjectID()
	if err := c.Commit(ctx, occ.ID, occ.Token, bookingID); err != nil {
		t.Fatal(err)
	}
	got, err := c.Inspect(ctx, occ.ID)
	if err != nil || got.State != app.OccupancyBooked || got.BookingID != bookingID {
		t.Fatalf("inspect: %+v %v", got, err)
	}
	if _, err := c.Acquire(ctx, s.req(t0, time.Hour, time.Minute)); !errors.Is(err, app.ErrOccupancyConflict) {
		t.Fatalf("booked must block: %v", err)
	}
	if err := c.Release(ctx, occ.ID, occ.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Acquire(ctx, s.req(t0, time.Hour, time.Minute)); err != nil {
		t.Fatalf("released must free: %v", err)
	}
}

func TestCommittingDoesNotExpire(t *testing.T) {
	c, ctx, s := newTestCoordinator(t), context.Background(), newSlot()
	occ, _ := c.Acquire(ctx, s.req(t0, time.Hour, 200*time.Millisecond))
	if err := c.BeginCommit(ctx, occ.ID, occ.Token); err != nil {
		t.Fatal(err)
	}
	time.Sleep(350 * time.Millisecond)
	if _, err := c.Acquire(ctx, s.req(t0, time.Hour, time.Minute)); !errors.Is(err, app.ErrOccupancyConflict) {
		t.Fatalf("committing must keep the slot: %v", err)
	}
}

func TestConcurrentAcquireHasSingleWinner(t *testing.T) {
	c, ctx, s := newTestCoordinator(t), context.Background(), newSlot()
	var wins, conflicts int32
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// Overlapping but different starts so it is not just a key collision.
			start := t0.Add(time.Duration(i%4) * 15 * time.Minute)
			_, err := c.Acquire(ctx, s.req(start, time.Hour, time.Minute))
			switch {
			case err == nil:
				atomic.AddInt32(&wins, 1)
			case errors.Is(err, app.ErrOccupancyConflict):
				atomic.AddInt32(&conflicts, 1)
			default:
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	if wins != 1 || conflicts != 63 {
		t.Fatalf("wins=%d conflicts=%d, want 1/63", wins, conflicts)
	}
}

func TestImport(t *testing.T) {
	c, ctx, s := newTestCoordinator(t), context.Background(), newSlot()
	req := app.ImportRequest{
		CenterID: s.center, SpaceID: s.space, BookingID: primitive.NewObjectID(),
		Start: t0, End: t0.Add(time.Hour), State: app.OccupancyBooked, OwnerRef: "o",
	}
	occ, err := c.Import(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Import(ctx, req); !errors.Is(err, app.ErrOccupancyExists) {
		t.Fatalf("re-import must be idempotent: %v", err)
	}
	other := req
	other.BookingID = primitive.NewObjectID()
	other.Start = t0.Add(30 * time.Minute)
	other.End = other.Start.Add(time.Hour)
	if _, err := c.Import(ctx, other); !errors.Is(err, app.ErrOccupancyConflict) {
		t.Fatalf("overlapping import: %v", err)
	}
	// The returned token is usable.
	if err := c.Release(ctx, occ.ID, occ.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Acquire(ctx, s.req(t0, time.Hour, time.Minute)); err != nil {
		t.Fatalf("released import must free the slot: %v", err)
	}
}

func TestGenerationGuard(t *testing.T) {
	c, ctx, s := newTestCoordinator(t), context.Background(), newSlot()

	if err := c.PublishGeneration(ctx, s.center, []primitive.ObjectID{s.space}, 3); err != nil {
		t.Fatal(err)
	}
	stale := s.req(t0, time.Hour, time.Minute) // Generation 0
	if _, err := c.Acquire(ctx, stale); !errors.Is(err, app.ErrOccupancyStaleGeneration) {
		t.Fatalf("stale acquire: %v", err)
	}
	if _, err := c.Import(ctx, app.ImportRequest{
		CenterID: s.center, SpaceID: s.space, BookingID: primitive.NewObjectID(),
		Start: t0, End: t0.Add(time.Hour), State: app.OccupancyBooked,
	}); !errors.Is(err, app.ErrOccupancyStaleGeneration) {
		t.Fatalf("stale import: %v", err)
	}

	current := stale
	current.Generation = 3
	occ, err := c.Acquire(ctx, current)
	if err != nil {
		t.Fatalf("current generation must work: %v", err)
	}
	// Operations on an existing occupancy keep working across the cutover.
	if err := c.BeginCommit(ctx, occ.ID, occ.Token); err != nil {
		t.Fatal(err)
	}
}
