// Command reconcile-occupancy keeps the occupancy coordinator (Redis) aligned
// with MongoDB.
//
//	MODE=import     loads active bookings that are not yet in Redis. Required
//	                before enabling BOOKING_REDIS_AUTHORITY for a center.
//	MODE=import REBUILD=true
//	                rebuilds Redis after a total loss: also imports bookings
//	                that already carry an occupancy link and rewrites the link.
//	                Only use it when the Redis namespace is empty; otherwise
//	                bookings conflict with their own previous occupancy.
//	MODE=reconcile  repairs differences: commits `committing` occupancies of
//	                confirmed bookings and releases those of cancelled,
//	                expired or finished bookings.
//
// Both modes are DRY_RUN by default; APPLY=true writes. In import mode the
// dry run detects overlaps among the bookings with an in-memory coordinator,
// so conflicts are visible before touching Redis.
//
// Env: MONGO_URI, MONGO_DB, REDIS_URL, REDIS_KEY_PREFIX, CENTER_ID, LIMIT.
package main

import (
	"context"
	"errors"
	"log"
	"os"
	"sort"
	"strconv"
	"time"

	"github.com/hamp/booking-sport/internal/app"
	occmongo "github.com/hamp/booking-sport/internal/infra/mongo"
	occredis "github.com/hamp/booking-sport/internal/infra/redis"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type bookingDoc struct {
	ID                    primitive.ObjectID `bson:"_id"`
	CourtID               primitive.ObjectID `bson:"court_id"`
	SportCenterID         primitive.ObjectID `bson:"sport_center_id"`
	Status                string             `bson:"status"`
	UserID                string             `bson:"user_id"`
	GuestDeviceID         string             `bson:"guest_device_id"`
	ScheduledAt           time.Time          `bson:"scheduled_at"`
	EndsAt                time.Time          `bson:"ends_at"`
	ScheduleSchemaVersion int                `bson:"schedule_schema_version"`
	LockExpiresAt         *time.Time         `bson:"lock_expires_at"`
	OccupancyID           string             `bson:"occupancy_id"`
	OccupancyToken        string             `bson:"occupancy_token"`
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	mode := env("MODE", "")
	apply := env("APPLY", "false") == "true"
	limit := int64(envInt("LIMIT", 0))

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(env("MONGO_URI", "mongodb://localhost:27017")))
	if err != nil {
		log.Fatalf("connect MongoDB: %v", err)
	}
	defer client.Disconnect(context.Background())
	db := client.Database(env("MONGO_DB", "sport_booking"))
	bookings := db.Collection("bookings")

	filter := bson.M{}
	if centerID := os.Getenv("CENTER_ID"); centerID != "" {
		oid, err := primitive.ObjectIDFromHex(centerID)
		if err != nil {
			log.Fatalf("invalid CENTER_ID: %v", err)
		}
		filter["sport_center_id"] = oid
	}

	switch mode {
	case "import":
		runImport(ctx, bookings, filter, apply, limit)
	case "reconcile":
		runReconcile(ctx, db, apply, limit)
	default:
		log.Fatal("MODE must be import or reconcile")
	}
}

func coordinator() *occredis.Coordinator {
	c, err := occredis.NewCoordinator(occredis.Config{
		URL:         env("REDIS_URL", ""),
		KeyPrefix:   env("REDIS_KEY_PREFIX", "reservaloya"),
		MaxDuration: 24 * time.Hour,
	})
	if err != nil {
		log.Fatalf("redis config: %v", err)
	}
	if err := c.Health(context.Background()); err != nil {
		log.Fatalf("redis unavailable: %v", err)
	}
	return c
}

func runImport(ctx context.Context, bookings *mongo.Collection, base bson.M, apply bool, limit int64) {
	now := time.Now()
	rebuild := env("REBUILD", "false") == "true"
	filter := bson.M{
		"status":  bson.M{"$in": []string{"confirmed", "pending"}},
		"ends_at": bson.M{"$gt": now},
	}
	if !rebuild {
		filter["occupancy_id"] = bson.M{"$exists": false}
	}
	for k, v := range base {
		filter[k] = v
	}

	legacy, err := bookings.CountDocuments(ctx, mergeFilter(base, bson.M{
		"status":                  bson.M{"$in": []string{"confirmed", "pending"}},
		"schedule_schema_version": bson.M{"$exists": false},
	}))
	if err != nil {
		log.Fatalf("count legacy: %v", err)
	}

	opts := options.Find().SetSort(bson.D{{Key: "court_id", Value: 1}, {Key: "scheduled_at", Value: 1}})
	if limit > 0 {
		opts.SetLimit(limit)
	}
	cursor, err := bookings.Find(ctx, filter, opts)
	if err != nil {
		log.Fatalf("find bookings: %v", err)
	}
	var docs []bookingDoc
	if err := cursor.All(ctx, &docs); err != nil {
		log.Fatalf("decode bookings: %v", err)
	}

	var importer app.OccupancyImporter = app.NewMemoryCoordinator(24 * time.Hour)
	mode := "DRY_RUN"
	if apply {
		importer = coordinator()
		mode = "APPLY"
	}
	log.Printf("[RECONCILE] import mode=%s rebuild=%v candidates=%d legacy_without_v2=%d", mode, rebuild, len(docs), legacy)

	counts := map[string]int{}
	for _, d := range docs {
		req := app.ImportRequest{
			CenterID: d.SportCenterID, SpaceID: d.CourtID, BookingID: d.ID,
			Start: d.ScheduledAt, End: d.EndsAt, State: app.OccupancyBooked,
			OwnerRef: app.OccupancyOwnerRef(d.UserID, d.GuestDeviceID),
		}
		if d.Status == "pending" {
			if d.LockExpiresAt == nil || !d.LockExpiresAt.After(now) {
				counts["pending_expired"]++
				continue
			}
			req.State, req.ExpiresAt = app.OccupancyHeld, *d.LockExpiresAt
		}

		occ, err := importer.Import(ctx, req)
		switch {
		case errors.Is(err, app.ErrOccupancyConflict):
			counts["conflict"]++
			log.Printf("[RECONCILE] CONFLICT booking=%s court=%s %s-%s", d.ID.Hex(), d.CourtID.Hex(),
				d.ScheduledAt.UTC().Format(time.RFC3339), d.EndsAt.UTC().Format(time.RFC3339))
			continue
		case errors.Is(err, app.ErrOccupancyExists):
			counts["already_in_redis"]++
			continue
		case err != nil:
			log.Fatalf("import %s: %v", d.ID.Hex(), err)
		}

		if apply {
			linkFilter := bson.M{"_id": d.ID}
			if !rebuild {
				linkFilter["occupancy_id"] = bson.M{"$exists": false}
			}
			res, err := bookings.UpdateOne(ctx, linkFilter,
				bson.M{"$set": bson.M{"occupancy_id": occ.ID, "occupancy_token": occ.Token}})
			if err != nil || res.ModifiedCount == 0 {
				counts["link_failed"]++
				log.Printf("[RECONCILE] link failed booking=%s err=%v", d.ID.Hex(), err)
				continue
			}
		}
		counts["imported"]++
	}
	report(counts)
	if !apply {
		log.Printf("[RECONCILE] DRY_RUN: nothing written. Re-run with APPLY=true.")
	}
}

func runReconcile(ctx context.Context, db *mongo.Database, apply bool, limit int64) {
	c := coordinator()
	defer c.Close()
	mode := "DRY_RUN"
	if apply {
		mode = "APPLY"
	}
	log.Printf("[RECONCILE] reconcile mode=%s", mode)

	reconciler := &app.OccupancyReconciler{
		Store: occmongo.NewOccupancyReconcileStore(db),
		Coord: c,
		Locks: occmongo.NewJobLockRepository(db),
		Apply: apply,
		Limit: limit,
	}
	counts, err := reconciler.Run(ctx)
	if err != nil {
		log.Fatalf("reconcile: %v", err)
	}
	report(counts)
	if !apply {
		log.Printf("[RECONCILE] DRY_RUN: nothing written. Re-run with APPLY=true.")
	}
}

func report(counts map[string]int) {
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		log.Printf("[RECONCILE]   %-28s %d", k, counts[k])
	}
}

func mergeFilter(a, b bson.M) bson.M {
	out := bson.M{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if n, err := strconv.Atoi(os.Getenv(key)); err == nil && n >= 0 {
		return n
	}
	return fallback
}
