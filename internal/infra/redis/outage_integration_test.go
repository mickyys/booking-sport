//go:build integration

package redis

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/hamp/booking-sport/internal/app"
)

// TestOutageAndRecovery stops the local Redis container mid-test.
// Run with REDIS_CONTAINER=booking_sport_redis (see `make test-redis-outage`).
func TestOutageAndRecovery(t *testing.T) {
	container := os.Getenv("REDIS_CONTAINER")
	if container == "" {
		t.Skip("REDIS_CONTAINER not set")
	}
	c, ctx, s := newTestCoordinator(t), context.Background(), newSlot()

	occ, err := c.Acquire(ctx, s.req(t0, time.Hour, time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.BeginCommit(ctx, occ.ID, occ.Token); err != nil {
		t.Fatal(err)
	}
	if err := c.Commit(ctx, occ.ID, occ.Token, newSlot().center); err != nil {
		t.Fatal(err)
	}

	if out, err := exec.Command("docker", "stop", container).CombinedOutput(); err != nil {
		t.Fatalf("docker stop: %v %s", err, out)
	}
	t.Cleanup(func() { exec.Command("docker", "start", container).Run() })

	// Writes fail fast (bounded by the client timeouts), they do not hang.
	start := time.Now()
	if _, err := c.Acquire(ctx, s.req(t0.Add(3*time.Hour), time.Hour, time.Minute)); err == nil {
		t.Fatal("acquire must fail while Redis is down")
	}
	if elapsed := time.Since(start); elapsed > 15*time.Second {
		t.Fatalf("acquire took %s during outage", elapsed)
	}
	if err := c.Health(ctx); err == nil {
		t.Fatal("health must report the outage")
	}

	if out, err := exec.Command("docker", "start", container).CombinedOutput(); err != nil {
		t.Fatalf("docker start: %v %s", err, out)
	}
	deadline := time.Now().Add(20 * time.Second)
	for c.Health(ctx) != nil {
		if time.Now().After(deadline) {
			t.Fatal("coordinator did not recover")
		}
		time.Sleep(300 * time.Millisecond)
	}

	// Data survived (container restart keeps the dataset only if persisted;
	// either way the scripts must work again after the client reconnects).
	if _, err := c.Acquire(ctx, s.req(t0.Add(5*time.Hour), time.Hour, time.Minute)); err != nil {
		t.Fatalf("acquire after recovery: %v", err)
	}
	if got, err := c.Inspect(ctx, occ.ID); err == nil {
		if got.State != app.OccupancyBooked {
			t.Fatalf("state after restart = %s", got.State)
		}
		t.Log("occupancy survived the restart")
	} else {
		t.Logf("occupancy lost on restart (%v): rebuild with reconcile-occupancy import", err)
	}
}
