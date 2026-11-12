//go:build integration

package redis

import (
	"context"
	"testing"

	"github.com/hamp/booking-sport/internal/app"
)

func TestVerificationLimiter(t *testing.T) {
	c, ctx := newTestCoordinator(t), context.Background()

	got, err := c.AllowVerificationRequest(ctx, "1.1.1.1", "12345678-5")
	if err != nil || got != app.VerificationLimitOK {
		t.Fatalf("first = %v, %v", got, err)
	}
	if got, _ := c.AllowVerificationRequest(ctx, "1.1.1.1", "12345678-5"); got != app.VerificationLimitCooldown {
		t.Fatalf("second = %v, want cooldown", got)
	}

	// Distintos RUT desde la misma IP agotan el límite por IP (10).
	var last app.VerificationLimit
	for i := 0; i < 12; i++ {
		last, _ = c.AllowVerificationRequest(ctx, "2.2.2.2", "doc-"+string(rune('a'+i)))
	}
	if last != app.VerificationLimitIP {
		t.Fatalf("after 12 = %v, want IP limit", last)
	}
}
