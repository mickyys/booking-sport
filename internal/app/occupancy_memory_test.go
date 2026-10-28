package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

func TestMemoryCoordinatorExclusionAndExpiry(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryCoordinator(24 * time.Hour)
	now := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	m.SetClock(func() time.Time { return now })

	center, space := primitive.NewObjectID(), primitive.NewObjectID()
	start := now.Add(time.Hour)
	req := func(s time.Time, d time.Duration) AcquireRequest {
		return AcquireRequest{CenterID: center, SpaceID: space, Start: s, End: s.Add(d), TTL: time.Minute}
	}

	occ, err := m.Acquire(ctx, req(start, time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Acquire(ctx, req(start.Add(30*time.Minute), time.Hour)); !errors.Is(err, ErrOccupancyConflict) {
		t.Fatalf("overlap: %v", err)
	}
	if _, err := m.Acquire(ctx, req(start.Add(time.Hour), time.Hour)); err != nil {
		t.Fatalf("adjacent: %v", err)
	}

	now = now.Add(2 * time.Minute)
	if _, err := m.Acquire(ctx, req(start, time.Hour)); err != nil {
		t.Fatalf("expired hold must free the slot: %v", err)
	}
	if err := m.Renew(ctx, occ.ID, occ.Token, time.Minute); !errors.Is(err, ErrOccupancyNotFound) {
		t.Fatalf("renew expired: %v", err)
	}
}
