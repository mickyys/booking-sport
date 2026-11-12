package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// policyCenterRepo returns a center with a given occupancy policy.
type policyCenterRepo struct {
	*mockCenterRepoForRecurring
	authority  string
	generation int64
}

func (p *policyCenterRepo) FindByID(_ context.Context, id primitive.ObjectID) (*domain.SportCenter, error) {
	return &domain.SportCenter{ID: id, Name: "Centro", OccupancyAuthority: p.authority, OccupancyGeneration: p.generation}, nil
}

func policyFixture(t *testing.T, authority string, generation int64) (*BookingUseCase, *mockBookingRepoForCreate, *MemoryCoordinator, *policyCenterRepo, domain.Court) {
	t.Helper()
	uc, repo, coord, court := occupancyCreateFixture(t)
	centers := &policyCenterRepo{mockCenterRepoForRecurring: &mockCenterRepoForRecurring{}, authority: authority, generation: generation}
	uc.centerRepo = centers
	return uc, repo, coord, centers, court
}

func takeSlot(t *testing.T, coord *MemoryCoordinator, b *domain.Booking) {
	t.Helper()
	probe := *b
	probe.SportCenterID = b.SportCenterID
	if err := probe.PrepareForWrite(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := coord.Acquire(context.Background(), AcquireRequest{
		CenterID: probe.SportCenterID, SpaceID: probe.CourtID,
		Start: probe.ScheduledAt, End: probe.EndsAt, TTL: time.Hour,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestShadow_RedisConflictDoesNotBlockAndIsRecorded(t *testing.T) {
	uc, repo, coord, _, court := policyFixture(t, "shadow", 0)
	b := futureBooking(court)
	b.SportCenterID = court.SportCenterID
	takeSlot(t, coord, b) // Redis thinks it is taken, Mongo mock says free

	before := ShadowStats.Snapshot()
	if err := uc.Create(context.Background(), futureBooking(court)); err != nil {
		t.Fatalf("shadow must not block: %v", err)
	}
	after := ShadowStats.Snapshot()

	if len(repo.Created) != 1 {
		t.Fatalf("created = %d, want 1 (Mongo decides)", len(repo.Created))
	}
	if after.RedisConflictMongoFree != before.RedisConflictMongoFree+1 {
		t.Fatal("divergence redis_conflict_mongo_free not recorded")
	}
	if repo.Created[0].OccupancyID != "" {
		t.Fatal("booking must stay unlinked when Redis disagreed")
	}
}

func TestShadow_AgreementIsLinkedAndCounted(t *testing.T) {
	uc, repo, _, _, court := policyFixture(t, "shadow", 0)
	before := ShadowStats.Snapshot()
	if err := uc.Create(context.Background(), futureBooking(court)); err != nil {
		t.Fatal(err)
	}
	after := ShadowStats.Snapshot()
	if after.AgreeFree != before.AgreeFree+1 || after.Divergences() != before.Divergences() {
		t.Fatalf("stats before=%+v after=%+v", before, after)
	}
	if repo.Created[0].OccupancyID == "" {
		t.Fatal("agreeing shadow write should be mirrored in Redis")
	}
}

func TestShadow_RedisOutageNeverBlocks(t *testing.T) {
	uc, repo, coord, _, court := policyFixture(t, "shadow", 0)
	uc.SetOccupancyCoordinator(downCoordinator{coord})

	if err := uc.Create(context.Background(), futureBooking(court)); err != nil {
		t.Fatalf("shadow must tolerate a Redis outage: %v", err)
	}
	if len(repo.Created) != 1 {
		t.Fatal("Mongo write must still happen")
	}
}

func TestShadow_MongoConflictRedisFreeIsRecorded(t *testing.T) {
	uc, repo, _, _, court := policyFixture(t, "shadow", 0)
	repo.FindConfirmedBySlotFn = func(context.Context, primitive.ObjectID, time.Time, int, int) (*domain.Booking, error) {
		return &domain.Booking{Status: domain.BookingStatusConfirmed}, nil
	}
	before := ShadowStats.Snapshot()

	err := uc.Create(context.Background(), futureBooking(court))
	if !isConflict(err) {
		t.Fatalf("Mongo conflict must still reject: %v", err)
	}
	after := ShadowStats.Snapshot()
	if after.MongoConflictRedisFree != before.MongoConflictRedisFree+1 {
		t.Fatalf("divergence mongo_conflict_redis_free not recorded: %+v", after)
	}
}

func TestPerCenterOffOverridesDefault(t *testing.T) {
	uc, repo, _, _, court := policyFixture(t, "off", 0)
	if err := uc.Create(context.Background(), futureBooking(court)); err != nil {
		t.Fatal(err)
	}
	if repo.Created[0].OccupancyID != "" {
		t.Fatal("center in off mode must not use the coordinator")
	}
}

func TestGeneration_StaleWriterIsRejectedUntilItReloads(t *testing.T) {
	uc, repo, coord, centers, court := policyFixture(t, "authority", 0)
	ctx := context.Background()

	// Cutover publishes generation 1 while this instance still caches 0.
	if err := coord.PublishGeneration(ctx, court.SportCenterID, []primitive.ObjectID{court.ID}, 1); err != nil {
		t.Fatal(err)
	}
	err := uc.Create(ctx, futureBooking(court))
	var appErr *domain.AppError
	if !errors.As(err, &appErr) || appErr.StatusCode != 503 {
		t.Fatalf("stale instance must be rejected with 503, got %v", err)
	}
	if len(repo.Created) != 0 {
		t.Fatal("a stale writer must not write")
	}

	// MongoDB now carries generation 1: the retry reloads it and succeeds.
	centers.generation = 1
	if err := uc.Create(ctx, futureBooking(court)); err != nil {
		t.Fatalf("after reloading the generation the write must succeed: %v", err)
	}
}

func TestGeneration_ShadowToleratesStaleGeneration(t *testing.T) {
	uc, repo, coord, _, court := policyFixture(t, "shadow", 0)
	_ = coord.PublishGeneration(context.Background(), court.SportCenterID, []primitive.ObjectID{court.ID}, 5)
	if err := uc.Create(context.Background(), futureBooking(court)); err != nil {
		t.Fatalf("shadow must not fail on stale generation: %v", err)
	}
	if len(repo.Created) != 1 {
		t.Fatal("Mongo write must happen")
	}
}
