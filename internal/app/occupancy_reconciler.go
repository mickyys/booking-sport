package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"github.com/hamp/booking-sport/pkg/logger"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

const (
	occupancyReconcileLockName = "occupancy-reconcile"
	// finishedGrace is how long after a booking ends its occupancy is kept.
	finishedGrace = time.Hour
)

// LinkedBooking is the part of a booking the reconciler needs.
type LinkedBooking struct {
	ID          primitive.ObjectID
	Status      domain.BookingStatus
	EndsAt      time.Time
	OccupancyID string
	Token       string
}

// OccupancyReconcileStore reads the bookings linked to an occupancy and
// clears the link once the occupancy no longer exists.
type OccupancyReconcileStore interface {
	FindLinkedBookings(ctx context.Context, limit int64) ([]LinkedBooking, error)
	ClearOccupancyLink(ctx context.Context, bookingID primitive.ObjectID) error
}

// ReconcileReport counts what a run found or repaired, by outcome.
type ReconcileReport map[string]int

func (r ReconcileReport) String() string {
	keys := make([]string, 0, len(r))
	for k := range r {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := ""
	for _, k := range keys {
		out += fmt.Sprintf("%s=%d ", k, r[k])
	}
	return out
}

// OccupancyReconciler aligns the coordinator with MongoDB, which stays the
// source of truth for what a booking is.
type OccupancyReconciler struct {
	Store OccupancyReconcileStore
	Coord OccupancyCoordinator
	Locks JobLockRepository
	// Apply false only reports. Used by the CLI dry run.
	Apply bool
	Limit int64
	Now   func() time.Time
}

// Run performs one reconciliation pass under a distributed lock.
func (r *OccupancyReconciler) Run(ctx context.Context) (ReconcileReport, error) {
	now := time.Now
	if r.Now != nil {
		now = r.Now
	}
	owner := primitive.NewObjectID().Hex()
	if r.Locks != nil {
		ok, err := r.Locks.TryAcquire(ctx, occupancyReconcileLockName, owner, 5*time.Minute, now())
		if err != nil {
			return nil, err
		}
		if !ok {
			return ReconcileReport{"skipped_locked": 1}, nil
		}
		defer func() {
			rctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = r.Locks.Release(rctx, occupancyReconcileLockName, owner)
		}()
	}

	linked, err := r.Store.FindLinkedBookings(ctx, r.Limit)
	if err != nil {
		return nil, err
	}

	report := ReconcileReport{}
	log := logger.FromContext(ctx)
	for _, b := range linked {
		occ, err := r.Coord.Inspect(ctx, b.OccupancyID)
		missing := errors.Is(err, ErrOccupancyNotFound)
		if err != nil && !missing {
			report["inspect_error"]++
			continue
		}
		finished := !b.EndsAt.IsZero() && b.EndsAt.Before(now().Add(-finishedGrace))
		active := b.Status == domain.BookingStatusConfirmed && !finished
		dead := b.Status == domain.BookingStatusCancelled || b.Status == domain.BookingStatusExpired || finished

		switch {
		case active && missing:
			report["confirmed_missing_in_redis"]++
			log.Errorw("occupancy_divergence", "kind", "confirmed_missing_in_redis", "booking_id", b.ID.Hex())
		case active && occ.State == OccupancyCommitting:
			report["committing_to_booked"]++
			if r.Apply {
				if err := r.Coord.Commit(ctx, b.OccupancyID, b.Token, b.ID); err != nil {
					report["repair_failed"]++
				}
			}
		case active && occ.State == OccupancyHeld:
			report["confirmed_but_held"]++
			log.Errorw("occupancy_divergence", "kind", "confirmed_but_held", "booking_id", b.ID.Hex())
		case dead && missing:
			report["link_cleared"]++
			if r.Apply {
				if err := r.Store.ClearOccupancyLink(ctx, b.ID); err != nil {
					report["repair_failed"]++
				}
			}
		case dead:
			report["released_stale"]++
			if r.Apply {
				err := r.Coord.Release(ctx, b.OccupancyID, b.Token)
				if err != nil && !errors.Is(err, ErrOccupancyNotFound) {
					report["repair_failed"]++
					continue
				}
				_ = r.Store.ClearOccupancyLink(ctx, b.ID)
			}
		default:
			report["ok"]++
		}
	}
	return report, nil
}

// Loop runs Run every interval until ctx is cancelled.
func (r *OccupancyReconciler) Loop(ctx context.Context, interval time.Duration) {
	log := logger.FromContext(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if snap := ShadowStats.Snapshot(); snap != (ShadowSnapshot{}) {
				log.Infow("occupancy_shadow_stats",
					"agree_free", snap.AgreeFree, "agree_conflict", snap.AgreeConflict,
					"redis_conflict_mongo_free", snap.RedisConflictMongoFree,
					"mongo_conflict_redis_free", snap.MongoConflictRedisFree,
					"redis_errors", snap.RedisErrors, "stale_generation", snap.StaleGeneration)
			}
			report, err := r.Run(ctx)
			if err != nil {
				log.Errorw("occupancy_reconcile_failed", "error", err)
				continue
			}
			if len(report) > 0 && (len(report) > 1 || report["ok"] == 0) {
				log.Infow("occupancy_reconcile_done", "report", report.String())
			}
		}
	}
}
