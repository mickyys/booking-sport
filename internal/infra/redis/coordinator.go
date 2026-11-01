package redis

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/hamp/booking-sport/internal/app"
	goredis "github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Coordinator implements app.OccupancyCoordinator with Lua scripts.
type Coordinator struct {
	client      *goredis.Client
	prefix      string
	maxDuration time.Duration

	acquire, renew, beginCommit, commit, release, importScript *goredis.Script
}

var (
	_ app.OccupancyCoordinator = (*Coordinator)(nil)
	_ app.OccupancyImporter    = (*Coordinator)(nil)
	_ app.GenerationPublisher  = (*Coordinator)(nil)
)

// Config for NewCoordinator.
type Config struct {
	URL         string
	KeyPrefix   string
	MaxDuration time.Duration
}

// NewCoordinator connects using a redis:// or rediss:// URL.
func NewCoordinator(cfg Config) (*Coordinator, error) {
	if cfg.MaxDuration <= 0 {
		cfg.MaxDuration = 24 * time.Hour
	}
	if cfg.KeyPrefix == "" {
		cfg.KeyPrefix = "reservaloya"
	}
	// Los errores de url.Parse/ParseURL incluyen la URL completa (con el token):
	// no se propagan para que nunca terminen en logs.
	u, err := url.Parse(cfg.URL)
	if err != nil {
		return nil, errors.New("invalid REDIS_URL (details omitted to avoid leaking credentials)")
	}
	opts, err := goredis.ParseURL(cfg.URL)
	if err != nil {
		return nil, errors.New("invalid REDIS_URL (details omitted to avoid leaking credentials)")
	}
	opts.Protocol = 2
	opts.PoolSize = 10
	opts.MaxRetries = 2
	opts.DialTimeout = 5 * time.Second
	opts.ReadTimeout = 2 * time.Second
	opts.WriteTimeout = 2 * time.Second
	opts.ConnMaxIdleTime = 5 * time.Minute
	if u.Scheme == "rediss" {
		opts.TLSConfig = &tls.Config{ServerName: u.Hostname(), MinVersion: tls.VersionTLS12}
	}

	// Plain Redis rejects unknown shebang flags; only Upstash needs this one.
	flags := ""
	if strings.HasSuffix(u.Hostname(), "upstash.io") {
		flags = upstashFlags
	}

	return &Coordinator{
		client:       goredis.NewClient(opts),
		prefix:       cfg.KeyPrefix,
		maxDuration:  cfg.MaxDuration,
		acquire:      goredis.NewScript(flags + luaAcquire),
		renew:        goredis.NewScript(flags + luaRenew),
		beginCommit:  goredis.NewScript(flags + luaBeginCommit),
		commit:       goredis.NewScript(flags + luaCommit),
		release:      goredis.NewScript(flags + luaRelease),
		importScript: goredis.NewScript(flags + luaImport),
	}, nil
}

func (c *Coordinator) Close() error { return c.client.Close() }

func (c *Coordinator) keys(centerID, spaceID primitive.ObjectID) []string {
	tag := fmt.Sprintf("%s:occ:{%s:%s}", c.prefix, centerID.Hex(), spaceID.Hex())
	return []string{tag + ":z", tag + ":m", tag + ":g"}
}

func (c *Coordinator) keysFor(id string) ([]string, error) {
	centerID, spaceID, err := app.ParseOccupancyID(id)
	if err != nil {
		return nil, err
	}
	return c.keys(centerID, spaceID), nil
}

func (c *Coordinator) Acquire(ctx context.Context, req app.AcquireRequest) (*app.Occupancy, error) {
	if err := req.Validate(c.maxDuration); err != nil {
		return nil, err
	}
	random, err := randomHex(16)
	if err != nil {
		return nil, err
	}
	token, err := randomHex(32)
	if err != nil {
		return nil, err
	}
	id := app.OccupancyID(req.CenterID, req.SpaceID, random)

	res, err := c.acquire.Run(ctx, c.client, c.keys(req.CenterID, req.SpaceID),
		id, req.Start.UnixMilli(), req.End.UnixMilli(), token, req.OwnerRef,
		req.TTL.Milliseconds(), c.maxDuration.Milliseconds(), req.Generation).Slice()
	if err != nil {
		return nil, err
	}
	if err := codeToError(res); err != nil {
		return nil, err
	}
	return &app.Occupancy{
		ID:        id,
		Token:     token,
		Start:     req.Start.UTC(),
		End:       req.End.UTC(),
		State:     app.OccupancyHeld,
		OwnerRef:  req.OwnerRef,
		ExpiresAt: time.UnixMilli(toInt(res, 1)).UTC(),
		Version:   1,
	}, nil
}

func (c *Coordinator) run(ctx context.Context, s *goredis.Script, id string, args ...any) error {
	keys, err := c.keysFor(id)
	if err != nil {
		return err
	}
	res, err := s.Run(ctx, c.client, keys, append([]any{id}, args...)...).Slice()
	if err != nil {
		return err
	}
	return codeToError(res)
}

func (c *Coordinator) Renew(ctx context.Context, id, token string, ttl time.Duration) error {
	return c.run(ctx, c.renew, id, token, ttl.Milliseconds())
}

func (c *Coordinator) BeginCommit(ctx context.Context, id, token string) error {
	return c.run(ctx, c.beginCommit, id, token)
}

func (c *Coordinator) Commit(ctx context.Context, id, token string, bookingID primitive.ObjectID) error {
	return c.run(ctx, c.commit, id, token, bookingID.Hex())
}

func (c *Coordinator) Release(ctx context.Context, id, token string) error {
	return c.run(ctx, c.release, id, token)
}

func (c *Coordinator) Inspect(ctx context.Context, id string) (*app.Occupancy, error) {
	keys, err := c.keysFor(id)
	if err != nil {
		return nil, err
	}
	raw, err := c.client.HGet(ctx, keys[1], id).Result()
	if errors.Is(err, goredis.Nil) {
		return nil, app.ErrOccupancyNotFound
	}
	if err != nil {
		return nil, err
	}
	f := strings.Split(raw, "|")
	if len(f) != 8 {
		return nil, fmt.Errorf("corrupt occupancy metadata for %s", id)
	}
	startMs, _ := strconv.ParseInt(f[0], 10, 64)
	endMs, _ := strconv.ParseInt(f[1], 10, 64)
	expMs, _ := strconv.ParseInt(f[5], 10, 64)
	version, _ := strconv.Atoi(f[7])
	state := app.OccupancyState(f[2])

	now, err := c.client.Time(ctx).Result()
	if err != nil {
		return nil, err
	}
	if state == app.OccupancyHeld && expMs > 0 && expMs <= now.UnixMilli() {
		return nil, app.ErrOccupancyNotFound
	}
	occ := &app.Occupancy{
		ID: id, Start: time.UnixMilli(startMs).UTC(), End: time.UnixMilli(endMs).UTC(),
		State: state, OwnerRef: f[4], Version: version,
	}
	if expMs > 0 {
		occ.ExpiresAt = time.UnixMilli(expMs).UTC()
	}
	if f[6] != "" {
		occ.BookingID, _ = primitive.ObjectIDFromHex(f[6])
	}
	return occ, nil
}

func (c *Coordinator) Health(ctx context.Context) error {
	return c.client.Ping(ctx).Err()
}

func codeToError(res []any) error {
	switch toInt(res, 0) {
	case 0:
		return nil
	case 1:
		return app.ErrOccupancyExists
	case -1:
		return app.ErrOccupancyConflict
	case -2:
		return app.ErrOccupancyToken
	case -3:
		return app.ErrOccupancyState
	case -4:
		return app.ErrOccupancyNotFound
	case -9:
		return app.ErrOccupancyStaleGeneration
	default:
		return fmt.Errorf("unexpected coordinator code %v", res)
	}
}

func toInt(res []any, i int) int64 {
	if i >= len(res) {
		return 0
	}
	v, _ := res[i].(int64)
	return v
}

func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// Import loads an existing booking. See app.OccupancyImporter.
func (c *Coordinator) Import(ctx context.Context, req app.ImportRequest) (*app.Occupancy, error) {
	if req.State != app.OccupancyBooked && req.State != app.OccupancyHeld {
		return nil, fmt.Errorf("import state must be booked or held, got %q", req.State)
	}
	if !req.End.After(req.Start) {
		return nil, errors.New("end must be after start")
	}
	token, err := randomHex(32)
	if err != nil {
		return nil, err
	}
	id := app.BookingOccupancyID(req.CenterID, req.SpaceID, req.BookingID)
	var expiresMs int64
	if req.State == app.OccupancyHeld {
		expiresMs = req.ExpiresAt.UnixMilli()
	}
	res, err := c.importScript.Run(ctx, c.client, c.keys(req.CenterID, req.SpaceID),
		id, req.Start.UnixMilli(), req.End.UnixMilli(), string(req.State), token,
		req.OwnerRef, expiresMs, req.BookingID.Hex(), c.maxDuration.Milliseconds(), req.Generation).Slice()
	if err != nil {
		return nil, err
	}
	if err := codeToError(res); err != nil {
		return nil, err
	}
	return &app.Occupancy{
		ID: id, Token: token, Start: req.Start.UTC(), End: req.End.UTC(), State: req.State,
		OwnerRef: req.OwnerRef, BookingID: req.BookingID, Version: 1,
	}, nil
}

// PublishGeneration stores the generation of each space of a center.
func (c *Coordinator) PublishGeneration(ctx context.Context, centerID primitive.ObjectID, spaceIDs []primitive.ObjectID, generation int64) error {
	for _, spaceID := range spaceIDs {
		key := c.keys(centerID, spaceID)[2]
		if err := c.client.Set(ctx, key, strconv.FormatInt(generation, 10), 0).Err(); err != nil {
			return fmt.Errorf("publish generation for space %s: %w", spaceID.Hex(), err)
		}
	}
	return nil
}
