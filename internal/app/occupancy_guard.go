package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"github.com/hamp/booking-sport/pkg/logger"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// OccupancyMode says who decides whether an interval of a center is free.
type OccupancyMode string

const (
	// OccupancyOff: only MongoDB (previous behavior).
	OccupancyOff OccupancyMode = "off"
	// OccupancyShadow: MongoDB decides. Redis receives a copy of every write
	// and its decision is compared; failures and disagreements are recorded
	// but never block the request.
	OccupancyShadow OccupancyMode = "shadow"
	// OccupancyAuthority: Redis decides. If it cannot answer, the write is
	// rejected (503); it never degrades to MongoDB.
	OccupancyAuthority OccupancyMode = "authority"
)

// ParseOccupancyMode maps a stored value; unknown values are off (safe).
func ParseOccupancyMode(value string) (OccupancyMode, bool) {
	switch OccupancyMode(value) {
	case OccupancyOff, OccupancyShadow, OccupancyAuthority:
		return OccupancyMode(value), true
	}
	return OccupancyOff, false
}

const (
	// occupancyCommitTTL bounds how long an interval is held while a booking
	// is being persisted. After BeginCommit the hold no longer expires by TTL.
	occupancyCommitTTL = 2 * time.Minute
	// MaxBookingDuration bounds a single booking interval.
	MaxBookingDuration = domain.MaxBookingDuration
	// defaultMPHoldTTL is how long an interval stays held while the customer pays.
	defaultMPHoldTTL = 15 * time.Minute
	// occupancyPolicyTTL is how long a center's mode/generation is cached.
	occupancyPolicyTTL = 30 * time.Second
)

// mpHoldTTL es el tiempo que se retiene un horario mientras el cliente paga.
// Se puede acortar con BOOKING_MP_HOLD_TTL (p. ej. "1m") para probar la
// expiración del bloqueo; mínimo 10s. Valores inválidos usan el valor por defecto.
var mpHoldTTL = holdTTLFromEnv(os.Getenv("BOOKING_MP_HOLD_TTL"))

func holdTTLFromEnv(raw string) time.Duration {
	if raw == "" {
		return defaultMPHoldTTL
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d < 10*time.Second {
		return defaultMPHoldTTL
	}
	return d
}

// ErrLatePaymentConflict means a payment was approved after the hold expired
// and the interval has since been taken by someone else.
var ErrLatePaymentConflict = errors.New("late payment: interval no longer available")

// ShadowStats counts shadow-mode comparisons. Divergences must be zero (or
// explained) before a center is cut over to authority.
var ShadowStats = &shadowStats{}

type shadowStats struct {
	AgreeFree, AgreeConflict     atomic.Int64
	RedisConflictMongoFree       atomic.Int64
	MongoConflictRedisFree       atomic.Int64
	RedisErrors, StaleGeneration atomic.Int64
}

// ShadowSnapshot is a point-in-time copy of ShadowStats.
type ShadowSnapshot struct {
	AgreeFree, AgreeConflict     int64
	RedisConflictMongoFree       int64
	MongoConflictRedisFree       int64
	RedisErrors, StaleGeneration int64
}

func (s *shadowStats) Snapshot() ShadowSnapshot {
	return ShadowSnapshot{
		AgreeFree: s.AgreeFree.Load(), AgreeConflict: s.AgreeConflict.Load(),
		RedisConflictMongoFree: s.RedisConflictMongoFree.Load(),
		MongoConflictRedisFree: s.MongoConflictRedisFree.Load(),
		RedisErrors:            s.RedisErrors.Load(), StaleGeneration: s.StaleGeneration.Load(),
	}
}

// Divergences are disagreements, not infrastructure errors.
func (s ShadowSnapshot) Divergences() int64 {
	return s.RedisConflictMongoFree + s.MongoConflictRedisFree
}

type occupancyPolicy struct {
	mode       OccupancyMode
	generation int64
	expires    time.Time
}

// SetOccupancyCoordinator enables the coordinator. Centers without an explicit
// occupancy_authority use the default mode, which is authority unless changed
// with SetOccupancyDefaultMode. With a nil coordinator the use case keeps its
// previous (MongoDB-only) behavior.
func (uc *BookingUseCase) SetOccupancyCoordinator(c OccupancyCoordinator) {
	uc.occupancy = c
	if c != nil && uc.occupancyDefault == "" {
		uc.occupancyDefault = OccupancyAuthority
	}
}

// SetOccupancyDefaultMode sets the mode of centers with no explicit setting.
func (uc *BookingUseCase) SetOccupancyDefaultMode(mode OccupancyMode) {
	uc.occupancyDefault = mode
}

// policyFor resolves the mode and generation of a center (cached briefly).
func (uc *BookingUseCase) policyFor(ctx context.Context, centerID primitive.ObjectID) occupancyPolicy {
	if uc.occupancy == nil {
		return occupancyPolicy{mode: OccupancyOff}
	}
	def := uc.occupancyDefault
	if def == "" {
		def = OccupancyOff
	}

	uc.policyMu.Lock()
	cached, ok := uc.policies[centerID]
	uc.policyMu.Unlock()
	if ok && time.Now().Before(cached.expires) {
		return cached
	}

	var center *domain.SportCenter
	if uc.centerRepo != nil && !centerID.IsZero() {
		center, _ = uc.centerRepo.FindByID(ctx, centerID)
	}
	if center == nil {
		return occupancyPolicy{mode: def}
	}
	pol := occupancyPolicy{mode: def, generation: center.OccupancyGeneration, expires: time.Now().Add(occupancyPolicyTTL)}
	if mode, ok := ParseOccupancyMode(center.OccupancyAuthority); ok && center.OccupancyAuthority != "" {
		pol.mode = mode
	}

	uc.policyMu.Lock()
	if uc.policies == nil {
		uc.policies = map[primitive.ObjectID]occupancyPolicy{}
	}
	uc.policies[centerID] = pol
	uc.policyMu.Unlock()
	return pol
}

func (uc *BookingUseCase) invalidatePolicy(centerID primitive.ObjectID) {
	uc.policyMu.Lock()
	delete(uc.policies, centerID)
	uc.policyMu.Unlock()
}

func (uc *BookingUseCase) occupancyModeFor(ctx context.Context, centerID primitive.ObjectID) OccupancyMode {
	return uc.policyFor(ctx, centerID).mode
}

// acquireOccupancy reserves the booking interval and links it to the booking.
//
// off: no-op. shadow: records the comparison and never fails the request (the
// booking is simply left unlinked). authority: conflicts are 409 and any
// coordinator failure is 503.
func (uc *BookingUseCase) acquireOccupancy(ctx context.Context, booking *domain.Booking, ttl time.Duration) error {
	pol := uc.policyFor(ctx, booking.SportCenterID)
	if pol.mode == OccupancyOff {
		return nil
	}
	if err := booking.PrepareForWrite(nil); err != nil {
		return err
	}
	req := AcquireRequest{
		CenterID: booking.SportCenterID, SpaceID: booking.CourtID,
		Start: booking.ScheduledAt, End: booking.EndsAt,
		OwnerRef: occupancyOwnerRef(booking), TTL: ttl, Generation: pol.generation,
	}
	if err := req.Validate(MaxBookingDuration); err != nil {
		return domain.NewBadRequestError(err.Error())
	}

	occ, err := uc.occupancy.Acquire(ctx, req)
	log := logger.FromContext(ctx)
	switch {
	case err == nil:
		if pol.mode == OccupancyShadow {
			ShadowStats.AgreeFree.Add(1)
		}
		booking.OccupancyID, booking.OccupancyToken = occ.ID, occ.Token
		return nil

	case errors.Is(err, ErrOccupancyConflict):
		if pol.mode == OccupancyShadow {
			ShadowStats.RedisConflictMongoFree.Add(1)
			log.Errorw("shadow_divergence", "kind", "redis_conflict_mongo_free",
				"booking_id", booking.ID.Hex(), "court_id", booking.CourtID.Hex(),
				"start", req.Start.UTC().Format(time.RFC3339), "end", req.End.UTC().Format(time.RFC3339))
			return nil
		}
		return domain.NewConflictError("el horario ya no está disponible")

	case errors.Is(err, ErrOccupancyStaleGeneration):
		uc.invalidatePolicy(booking.SportCenterID)
		ShadowStats.StaleGeneration.Add(1)
		log.Errorw("occupancy_stale_generation", "center_id", booking.SportCenterID.Hex(), "generation", pol.generation)
		if pol.mode == OccupancyShadow {
			return nil
		}
		return &domain.AppError{Message: "el servicio de reservas se está actualizando, intenta nuevamente", StatusCode: 503}

	default:
		ShadowStats.RedisErrors.Add(1)
		log.Errorw("occupancy_coordinator_unavailable", "error", err)
		if pol.mode == OccupancyShadow {
			return nil
		}
		// Redis is the exclusion authority: if it cannot answer, the write is
		// rejected. Degrading to MongoDB would allow double bookings.
		return &domain.AppError{Message: "el servicio de reservas no está disponible, intenta nuevamente en unos minutos", StatusCode: 503}
	}
}

// shadowCompareMongoConflict is called when MongoDB rejected an interval. In
// shadow mode it asks Redis too, to detect cases Redis would have allowed.
func (uc *BookingUseCase) shadowCompareMongoConflict(ctx context.Context, booking *domain.Booking) {
	if uc.occupancyModeFor(ctx, booking.SportCenterID) != OccupancyShadow {
		return
	}
	probe := *booking
	probe.ID = primitive.NilObjectID
	probe.OccupancyID, probe.OccupancyToken = "", ""
	pol := uc.policyFor(ctx, booking.SportCenterID)
	if err := probe.PrepareForWrite(nil); err != nil {
		return
	}
	occ, err := uc.occupancy.Acquire(ctx, AcquireRequest{
		CenterID: probe.SportCenterID, SpaceID: probe.CourtID,
		Start: probe.ScheduledAt, End: probe.EndsAt, TTL: 5 * time.Second, Generation: pol.generation,
	})
	switch {
	case errors.Is(err, ErrOccupancyConflict):
		ShadowStats.AgreeConflict.Add(1)
	case err == nil:
		ShadowStats.MongoConflictRedisFree.Add(1)
		logger.FromContext(ctx).Errorw("shadow_divergence", "kind", "mongo_conflict_redis_free",
			"court_id", booking.CourtID.Hex(),
			"start", probe.ScheduledAt.UTC().Format(time.RFC3339), "end", probe.EndsAt.UTC().Format(time.RFC3339))
		_ = uc.occupancy.Release(ctx, occ.ID, occ.Token)
	default:
		ShadowStats.RedisErrors.Add(1)
	}
}

// releaseOccupancy frees the interval of a booking. Missing occupancies are
// ignored: the goal state (interval free) is already reached.
func (uc *BookingUseCase) releaseOccupancy(ctx context.Context, booking *domain.Booking) {
	if uc.occupancy == nil || booking.OccupancyID == "" {
		return
	}
	err := uc.occupancy.Release(ctx, booking.OccupancyID, booking.OccupancyToken)
	if err != nil && !errors.Is(err, ErrOccupancyNotFound) {
		logger.FromContext(ctx).Errorw("occupancy_release_failed",
			"occupancy_id", booking.OccupancyID, "booking_id", booking.ID.Hex(), "error", err)
	}
}

// persistWithOccupancy runs create (which inserts the confirmed booking) under
// the coordinator: acquire → committing → create → booked. On any failure the
// interval is released, so a failed insert never leaves the slot blocked.
func (uc *BookingUseCase) persistWithOccupancy(ctx context.Context, booking *domain.Booking, create func() error) error {
	if uc.occupancyModeFor(ctx, booking.SportCenterID) == OccupancyOff {
		return create()
	}
	if err := uc.acquireOccupancy(ctx, booking, occupancyCommitTTL); err != nil {
		return err
	}
	if booking.OccupancyID == "" { // shadow without a usable Redis answer
		return create()
	}
	return uc.commitOccupancy(ctx, booking, create)
}

// commitOccupancy completes a flow whose interval is already acquired.
func (uc *BookingUseCase) commitOccupancy(ctx context.Context, booking *domain.Booking, create func() error) error {
	shadow := uc.occupancyModeFor(ctx, booking.SportCenterID) == OccupancyShadow
	if err := uc.occupancy.BeginCommit(ctx, booking.OccupancyID, booking.OccupancyToken); err != nil {
		if !shadow {
			uc.releaseOccupancy(ctx, booking)
			return fmt.Errorf("error iniciando la confirmación: %w", err)
		}
		ShadowStats.RedisErrors.Add(1)
		uc.releaseOccupancy(ctx, booking)
		booking.OccupancyID, booking.OccupancyToken = "", ""
		return create()
	}
	if err := create(); err != nil {
		uc.releaseOccupancy(ctx, booking)
		return err
	}
	if err := uc.occupancy.Commit(ctx, booking.OccupancyID, booking.OccupancyToken, booking.ID); err != nil {
		// The booking exists in MongoDB and the interval is still blocked
		// (committing never expires). Reconciliation repairs the state.
		logger.FromContext(ctx).Errorw("occupancy_commit_failed",
			"occupancy_id", booking.OccupancyID, "booking_id", booking.ID.Hex(), "error", err)
	}
	return nil
}

// occupancyOwnerRef is an opaque reference to the requester. Raw user ids and
// device ids are never written to Redis.
func occupancyOwnerRef(b *domain.Booking) string {
	return OccupancyOwnerRef(b.UserID, b.GuestDeviceID)
}

// OccupancyOwnerRef hashes the requester identity stored in the coordinator.
func OccupancyOwnerRef(userID, guestDeviceID string) string {
	raw := userID
	if raw == "" {
		raw = guestDeviceID
	}
	if raw == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:8])
}

// holdOccupancy keeps the interval of a pending (unpaid) booking held for ttl:
// it renews the existing occupancy or, if it expired or never existed,
// acquires a new one.
func (uc *BookingUseCase) holdOccupancy(ctx context.Context, booking *domain.Booking, ttl time.Duration) error {
	mode := uc.occupancyModeFor(ctx, booking.SportCenterID)
	if mode == OccupancyOff {
		return nil
	}
	if booking.OccupancyID != "" {
		err := uc.occupancy.Renew(ctx, booking.OccupancyID, booking.OccupancyToken, ttl)
		if err == nil {
			return nil
		}
		if !errors.Is(err, ErrOccupancyNotFound) && !errors.Is(err, ErrOccupancyToken) {
			if mode == OccupancyShadow {
				ShadowStats.RedisErrors.Add(1)
				return nil
			}
			return &domain.AppError{Message: "el servicio de reservas no está disponible, intenta nuevamente en unos minutos", StatusCode: 503}
		}
		booking.OccupancyID, booking.OccupancyToken = "", ""
	}
	return uc.acquireOccupancy(ctx, booking, ttl)
}

// beginConfirm moves the occupancy of a paid booking to committing before the
// booking is persisted as confirmed. If the hold expired it re-acquires the
// interval; if somebody else owns it now it returns ErrLatePaymentConflict so
// the caller can compensate (refund). In shadow mode it never fails.
func (uc *BookingUseCase) beginConfirm(ctx context.Context, booking *domain.Booking) error {
	mode := uc.occupancyModeFor(ctx, booking.SportCenterID)
	if mode == OccupancyOff {
		return nil
	}
	err := uc.beginConfirmAuthority(ctx, booking)
	if err != nil && mode == OccupancyShadow {
		if errors.Is(err, ErrLatePaymentConflict) {
			ShadowStats.RedisConflictMongoFree.Add(1)
			logger.FromContext(ctx).Errorw("shadow_divergence", "kind", "late_payment_redis_conflict",
				"booking_id", booking.ID.Hex())
		} else {
			ShadowStats.RedisErrors.Add(1)
		}
		return nil
	}
	return err
}

func (uc *BookingUseCase) beginConfirmAuthority(ctx context.Context, booking *domain.Booking) error {
	if booking.OccupancyID != "" {
		err := uc.occupancy.BeginCommit(ctx, booking.OccupancyID, booking.OccupancyToken)
		if err == nil {
			return nil
		}
		if errors.Is(err, ErrOccupancyState) {
			// A previous attempt already moved it to committing (retry).
			if cur, ierr := uc.occupancy.Inspect(ctx, booking.OccupancyID); ierr == nil && cur.State != OccupancyHeld {
				return nil
			}
		}
		if !errors.Is(err, ErrOccupancyNotFound) {
			return fmt.Errorf("error confirmando el horario: %w", err)
		}
		booking.OccupancyID, booking.OccupancyToken = "", ""
	}

	if err := uc.acquireOccupancy(ctx, booking, occupancyCommitTTL); err != nil {
		if isConflict(err) {
			return ErrLatePaymentConflict
		}
		return err
	}
	if booking.OccupancyID == "" {
		return nil
	}
	if err := uc.occupancy.BeginCommit(ctx, booking.OccupancyID, booking.OccupancyToken); err != nil {
		uc.releaseOccupancy(ctx, booking)
		return fmt.Errorf("error confirmando el horario: %w", err)
	}
	return nil
}

// finishConfirm marks the occupancy as booked after the booking was persisted.
func (uc *BookingUseCase) finishConfirm(ctx context.Context, booking *domain.Booking) {
	if uc.occupancy == nil || booking.OccupancyID == "" {
		return
	}
	if err := uc.occupancy.Commit(ctx, booking.OccupancyID, booking.OccupancyToken, booking.ID); err != nil &&
		!errors.Is(err, ErrOccupancyState) {
		logger.FromContext(ctx).Errorw("occupancy_commit_failed",
			"occupancy_id", booking.OccupancyID, "booking_id", booking.ID.Hex(), "error", err)
	}
}

// moveOccupancy moves the occupancy of a booking while it is rescheduled.
// orig is the booking as stored; moved carries the new interval. persist
// writes the moved booking.
//
// authority: the new interval is acquired first. If it only conflicts with the
// booking's own old interval (a shift or resize), the old one is released and
// the acquisition retried; if that still fails, the old interval is restored.
// shadow: MongoDB writes first and Redis is updated best effort.
func (uc *BookingUseCase) moveOccupancy(ctx context.Context, orig, moved *domain.Booking, persist func() error) error {
	mode := uc.occupancyModeFor(ctx, moved.SportCenterID)
	switch mode {
	case OccupancyOff:
		return persist()
	case OccupancyShadow:
		if err := persist(); err != nil {
			return err
		}
		uc.releaseOccupancy(ctx, orig)
		moved.OccupancyID, moved.OccupancyToken = "", ""
		if err := uc.acquireOccupancy(ctx, moved, occupancyCommitTTL); err == nil && moved.OccupancyID != "" {
			if err := uc.occupancy.BeginCommit(ctx, moved.OccupancyID, moved.OccupancyToken); err == nil {
				uc.finishConfirm(ctx, moved)
			}
		}
		return nil
	}

	oldID, oldToken := orig.OccupancyID, orig.OccupancyToken
	moved.OccupancyID, moved.OccupancyToken = "", ""

	swapped := false
	err := uc.acquireOccupancy(ctx, moved, occupancyCommitTTL)
	if err != nil && oldID != "" && bookingsOverlap(orig, moved) && isConflict(err) {
		uc.releaseOccupancy(ctx, orig)
		swapped = true
		err = uc.acquireOccupancy(ctx, moved, occupancyCommitTTL)
	}
	if err != nil {
		if swapped {
			uc.restoreOccupancy(ctx, orig)
		}
		return err
	}

	if err := uc.occupancy.BeginCommit(ctx, moved.OccupancyID, moved.OccupancyToken); err != nil {
		uc.releaseOccupancy(ctx, moved)
		if swapped {
			uc.restoreOccupancy(ctx, orig)
		}
		return fmt.Errorf("error iniciando la confirmación: %w", err)
	}
	if err := persist(); err != nil {
		uc.releaseOccupancy(ctx, moved)
		if swapped {
			uc.restoreOccupancy(ctx, orig)
		}
		return err
	}
	uc.finishConfirm(ctx, moved)
	if !swapped && oldID != "" {
		_ = uc.occupancy.Release(ctx, oldID, oldToken)
	}
	return nil
}

// restoreOccupancy re-acquires the original interval after a failed move and
// links the stored booking to it.
func (uc *BookingUseCase) restoreOccupancy(ctx context.Context, orig *domain.Booking) {
	restored := *orig
	restored.OccupancyID, restored.OccupancyToken = "", ""
	if err := uc.acquireOccupancy(ctx, &restored, occupancyCommitTTL); err != nil || restored.OccupancyID == "" {
		logger.FromContext(ctx).Errorw("occupancy_restore_failed", "booking_id", orig.ID.Hex(), "error", err)
		return
	}
	if err := uc.occupancy.BeginCommit(ctx, restored.OccupancyID, restored.OccupancyToken); err == nil {
		uc.finishConfirm(ctx, &restored)
	}
	if err := uc.repo.Update(ctx, &restored); err != nil {
		logger.FromContext(ctx).Errorw("occupancy_restore_link_failed", "booking_id", orig.ID.Hex(), "error", err)
	}
}

func bookingsOverlap(a, b *domain.Booking) bool {
	return a.CourtID == b.CourtID && a.StartInstant().Before(b.EndInstant()) && b.StartInstant().Before(a.EndInstant())
}

func isConflict(err error) bool {
	var appErr *domain.AppError
	return errors.As(err, &appErr) && appErr.StatusCode == 409
}
