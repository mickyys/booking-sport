package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// OccupancyState is the lifecycle of an interval in the coordinator.
type OccupancyState string

const (
	// OccupancyHeld is a temporary reservation that expires with its TTL.
	OccupancyHeld OccupancyState = "held"
	// OccupancyCommitting means payment/persistence is in flight. It never
	// expires by TTL: only an explicit Commit/Release (or reconciliation)
	// resolves it.
	OccupancyCommitting OccupancyState = "committing"
	// OccupancyBooked is a confirmed interval backed by a booking.
	OccupancyBooked OccupancyState = "booked"
)

var (
	ErrOccupancyConflict = errors.New("occupancy conflict: interval already taken")
	ErrOccupancyToken    = errors.New("occupancy token mismatch")
	ErrOccupancyState    = errors.New("occupancy in invalid state for this operation")
	ErrOccupancyNotFound = errors.New("occupancy not found or expired")
	ErrOccupancyExists   = errors.New("occupancy already exists")
	// ErrOccupancyStaleGeneration means the writer holds an outdated
	// occupancy generation (a cutover happened); it must reload and retry.
	ErrOccupancyStaleGeneration = errors.New("stale occupancy generation")
)

// AcquireRequest asks the coordinator for exclusive use of [Start, End) in a
// space.
type AcquireRequest struct {
	CenterID primitive.ObjectID
	SpaceID  primitive.ObjectID
	Start    time.Time
	End      time.Time
	// OwnerRef is an opaque hash of the user/session. Never a raw identifier.
	OwnerRef string
	TTL      time.Duration
	// Generation is the occupancy generation the writer believes is current.
	Generation int64
}

// Occupancy is the coordinator's view of an interval.
type Occupancy struct {
	// ID encodes the space so later calls can find the keys: centerHex.spaceHex.rand
	ID        string
	Token     string
	Start     time.Time
	End       time.Time
	State     OccupancyState
	OwnerRef  string
	ExpiresAt time.Time
	BookingID primitive.ObjectID
	Version   int
}

// OccupancyCoordinator is the single authority that decides whether an
// interval of a space is free.
type OccupancyCoordinator interface {
	Acquire(ctx context.Context, req AcquireRequest) (*Occupancy, error)
	Renew(ctx context.Context, id, token string, ttl time.Duration) error
	BeginCommit(ctx context.Context, id, token string) error
	Commit(ctx context.Context, id, token string, bookingID primitive.ObjectID) error
	Release(ctx context.Context, id, token string) error
	Inspect(ctx context.Context, id string) (*Occupancy, error)
	Health(ctx context.Context) error
}

// OccupancyID builds an ID that embeds the space it belongs to.
func OccupancyID(centerID, spaceID primitive.ObjectID, random string) string {
	return centerID.Hex() + "." + spaceID.Hex() + "." + random
}

// ParseOccupancyID returns the center and space encoded in an occupancy ID.
func ParseOccupancyID(id string) (centerID, spaceID primitive.ObjectID, err error) {
	parts := strings.Split(id, ".")
	if len(parts) != 3 {
		return centerID, spaceID, fmt.Errorf("invalid occupancy id %q", id)
	}
	if centerID, err = primitive.ObjectIDFromHex(parts[0]); err != nil {
		return centerID, spaceID, fmt.Errorf("invalid occupancy id %q: %w", id, err)
	}
	if spaceID, err = primitive.ObjectIDFromHex(parts[1]); err != nil {
		return centerID, spaceID, fmt.Errorf("invalid occupancy id %q: %w", id, err)
	}
	return centerID, spaceID, nil
}

// Validate checks the request before it reaches the coordinator.
func (r AcquireRequest) Validate(maxDuration time.Duration) error {
	if r.CenterID.IsZero() || r.SpaceID.IsZero() {
		return errors.New("center and space are required")
	}
	if !r.End.After(r.Start) {
		return errors.New("end must be after start")
	}
	if maxDuration > 0 && r.End.Sub(r.Start) > maxDuration {
		return fmt.Errorf("interval exceeds max duration %s", maxDuration)
	}
	if r.TTL <= 0 {
		return errors.New("ttl must be positive")
	}
	return nil
}

// ImportRequest loads an already-persisted booking into the coordinator. The
// ID is deterministic (see BookingOccupancyID) so imports are idempotent.
type ImportRequest struct {
	CenterID  primitive.ObjectID
	SpaceID   primitive.ObjectID
	BookingID primitive.ObjectID
	Start     time.Time
	End       time.Time
	State     OccupancyState // booked or held
	OwnerRef  string
	// ExpiresAt is required for held; ignored for booked.
	ExpiresAt  time.Time
	Generation int64
}

// OccupancyImporter is implemented by coordinators that can import existing
// bookings (backfill before enabling the coordinator for a center).
type OccupancyImporter interface {
	// Import returns the occupancy with its fresh token. ErrOccupancyExists
	// means the booking was already imported; ErrOccupancyConflict means the
	// interval overlaps another occupancy.
	Import(ctx context.Context, req ImportRequest) (*Occupancy, error)
}

// BookingOccupancyID is the deterministic occupancy ID of an imported booking.
func BookingOccupancyID(centerID, spaceID, bookingID primitive.ObjectID) string {
	return OccupancyID(centerID, spaceID, bookingID.Hex())
}

// GenerationPublisher publishes the occupancy generation of a center's spaces
// to the coordinator (cutover). After publishing N, writers with another
// generation are rejected with ErrOccupancyStaleGeneration.
type GenerationPublisher interface {
	PublishGeneration(ctx context.Context, centerID primitive.ObjectID, spaceIDs []primitive.ObjectID, generation int64) error
}
