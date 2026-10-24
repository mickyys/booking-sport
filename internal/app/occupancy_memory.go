package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// MemoryCoordinator is an in-process OccupancyCoordinator with the same
// semantics as the Redis scripts. It is meant for unit tests and local runs
// without Redis; it provides no cross-process exclusion.
type MemoryCoordinator struct {
	mu          sync.Mutex
	now         func() time.Time
	maxDuration time.Duration
	// spaces maps "center.space" to its occupancies by ID.
	spaces      map[string]map[string]*Occupancy
	generations map[string]int64
}

func NewMemoryCoordinator(maxDuration time.Duration) *MemoryCoordinator {
	return &MemoryCoordinator{
		now:         time.Now,
		maxDuration: maxDuration,
		spaces:      map[string]map[string]*Occupancy{},
		generations: map[string]int64{},
	}
}

// SetClock replaces the clock; for tests.
func (m *MemoryCoordinator) SetClock(now func() time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.now = now
}

var _ OccupancyCoordinator = (*MemoryCoordinator)(nil)

func spaceKey(centerID, spaceID primitive.ObjectID) string {
	return centerID.Hex() + "." + spaceID.Hex()
}

func (m *MemoryCoordinator) Acquire(_ context.Context, req AcquireRequest) (*Occupancy, error) {
	if err := req.Validate(m.maxDuration); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	now := m.now()
	key := spaceKey(req.CenterID, req.SpaceID)
	if m.generations[key] != req.Generation {
		return nil, ErrOccupancyStaleGeneration
	}
	space := m.spaces[key]
	if space == nil {
		space = map[string]*Occupancy{}
		m.spaces[key] = space
	}

	for id, occ := range space {
		if occ.State == OccupancyHeld && !occ.ExpiresAt.After(now) {
			delete(space, id)
			continue
		}
		if occ.Start.Before(req.End) && occ.End.After(req.Start) {
			return nil, ErrOccupancyConflict
		}
	}

	random, err := randomHex(16)
	if err != nil {
		return nil, err
	}
	token, err := randomHex(32)
	if err != nil {
		return nil, err
	}
	occ := &Occupancy{
		ID:        OccupancyID(req.CenterID, req.SpaceID, random),
		Token:     token,
		Start:     req.Start.UTC(),
		End:       req.End.UTC(),
		State:     OccupancyHeld,
		OwnerRef:  req.OwnerRef,
		ExpiresAt: now.Add(req.TTL),
		Version:   1,
	}
	space[occ.ID] = occ
	copy := *occ
	return &copy, nil
}

// lookup returns the live occupancy; the caller must hold the lock.
func (m *MemoryCoordinator) lookup(id string) (*Occupancy, map[string]*Occupancy, error) {
	centerID, spaceID, err := ParseOccupancyID(id)
	if err != nil {
		return nil, nil, err
	}
	space := m.spaces[spaceKey(centerID, spaceID)]
	occ := space[id]
	if occ == nil {
		return nil, nil, ErrOccupancyNotFound
	}
	if occ.State == OccupancyHeld && !occ.ExpiresAt.After(m.now()) {
		delete(space, id)
		return nil, nil, ErrOccupancyNotFound
	}
	return occ, space, nil
}

func (m *MemoryCoordinator) Renew(_ context.Context, id, token string, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	occ, _, err := m.lookup(id)
	if err != nil {
		return err
	}
	if occ.Token != token {
		return ErrOccupancyToken
	}
	if occ.State != OccupancyHeld {
		return ErrOccupancyState
	}
	occ.ExpiresAt = m.now().Add(ttl)
	occ.Version++
	return nil
}

func (m *MemoryCoordinator) BeginCommit(_ context.Context, id, token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	occ, _, err := m.lookup(id)
	if err != nil {
		return err
	}
	if occ.Token != token {
		return ErrOccupancyToken
	}
	if occ.State != OccupancyHeld {
		return ErrOccupancyState
	}
	occ.State = OccupancyCommitting
	occ.ExpiresAt = time.Time{}
	occ.Version++
	return nil
}

func (m *MemoryCoordinator) Commit(_ context.Context, id, token string, bookingID primitive.ObjectID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	occ, _, err := m.lookup(id)
	if err != nil {
		return err
	}
	if occ.Token != token {
		return ErrOccupancyToken
	}
	if occ.State != OccupancyCommitting {
		return ErrOccupancyState
	}
	occ.State = OccupancyBooked
	occ.BookingID = bookingID
	occ.Version++
	return nil
}

func (m *MemoryCoordinator) Release(_ context.Context, id, token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	occ, space, err := m.lookup(id)
	if err != nil {
		return err
	}
	if occ.Token != token {
		return ErrOccupancyToken
	}
	delete(space, id)
	return nil
}

func (m *MemoryCoordinator) Inspect(_ context.Context, id string) (*Occupancy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	occ, _, err := m.lookup(id)
	if err != nil {
		return nil, err
	}
	copy := *occ
	copy.Token = ""
	return &copy, nil
}

func (m *MemoryCoordinator) Health(context.Context) error { return nil }

func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

var _ OccupancyImporter = (*MemoryCoordinator)(nil)

func (m *MemoryCoordinator) Import(_ context.Context, req ImportRequest) (*Occupancy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := m.now()
	key := spaceKey(req.CenterID, req.SpaceID)
	if m.generations[key] != req.Generation {
		return nil, ErrOccupancyStaleGeneration
	}
	space := m.spaces[key]
	if space == nil {
		space = map[string]*Occupancy{}
		m.spaces[key] = space
	}
	id := BookingOccupancyID(req.CenterID, req.SpaceID, req.BookingID)
	if _, ok := space[id]; ok {
		return nil, ErrOccupancyExists
	}
	for oid, occ := range space {
		if occ.State == OccupancyHeld && !occ.ExpiresAt.After(now) {
			delete(space, oid)
			continue
		}
		if occ.Start.Before(req.End) && occ.End.After(req.Start) {
			return nil, ErrOccupancyConflict
		}
	}
	token, err := randomHex(32)
	if err != nil {
		return nil, err
	}
	occ := &Occupancy{
		ID: id, Token: token, Start: req.Start.UTC(), End: req.End.UTC(),
		State: req.State, OwnerRef: req.OwnerRef, BookingID: req.BookingID, Version: 1,
	}
	if req.State == OccupancyHeld {
		occ.ExpiresAt = req.ExpiresAt
	}
	space[id] = occ
	c := *occ
	return &c, nil
}

var _ GenerationPublisher = (*MemoryCoordinator)(nil)

func (m *MemoryCoordinator) PublishGeneration(_ context.Context, centerID primitive.ObjectID, spaceIDs []primitive.ObjectID, generation int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, spaceID := range spaceIDs {
		m.generations[spaceKey(centerID, spaceID)] = generation
	}
	return nil
}
