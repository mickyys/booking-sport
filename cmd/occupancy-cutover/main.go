// Command occupancy-cutover changes who decides the availability of a center:
//
//	ACTION=status     shows mode, generation and how many active bookings are
//	                  linked to the coordinator.
//	ACTION=shadow     MongoDB decides; Redis receives a copy and decisions are
//	                  compared (see occupancy_shadow_stats / shadow_divergence).
//	ACTION=authority  Redis decides. Refuses unless every active booking has
//	                  been imported (MODE=import of cmd/reconcile-occupancy).
//	                  Publishes generation N+1 to Redis and MongoDB.
//	ACTION=off        rollback: MongoDB decides again. Also bumps the
//	                  generation so instances with a stale view cannot acquire.
//
// Required: CENTER_ID. Dry run by default; APPLY=true writes.
// Env: MONGO_URI, MONGO_DB, REDIS_URL, REDIS_KEY_PREFIX, FORCE (skip the
// "all imported" check for authority).
//
// Cutover runbook (docs/runbooks-occupancy-redis.md §8): pause writes, drain,
// import, verify, ACTION=authority, reopen.
package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/hamp/booking-sport/internal/app"
	occredis "github.com/hamp/booking-sport/internal/infra/redis"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	action := os.Getenv("ACTION")
	apply := os.Getenv("APPLY") == "true"
	centerID, err := primitive.ObjectIDFromHex(os.Getenv("CENTER_ID"))
	if err != nil {
		log.Fatalf("CENTER_ID is required: %v", err)
	}

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(env("MONGO_URI", "mongodb://localhost:27017")))
	if err != nil {
		log.Fatalf("connect MongoDB: %v", err)
	}
	defer client.Disconnect(context.Background())
	db := client.Database(env("MONGO_DB", "sport_booking"))

	var center struct {
		Name       string `bson:"name"`
		Authority  string `bson:"occupancy_authority"`
		Generation int64  `bson:"occupancy_generation"`
	}
	if err := db.Collection("sport_centers").FindOne(ctx, bson.M{"_id": centerID}).Decode(&center); err != nil {
		log.Fatalf("center not found: %v", err)
	}
	if center.Authority == "" {
		center.Authority = "off (default)"
	}

	now := time.Now()
	// Activas = confirmadas, o pendientes con hold vigente (las pendientes
	// vencidas no ocupan horario y el import las omite).
	active := bson.M{
		"sport_center_id": centerID,
		"ends_at":         bson.M{"$gt": now},
		"$or": []bson.M{
			{"status": "confirmed"},
			{"status": "pending", "lock_expires_at": bson.M{"$gt": now}},
		},
	}
	activeCount, err := db.Collection("bookings").CountDocuments(ctx, active)
	must(err)
	unlinkedFilter := bson.M{"occupancy_id": bson.M{"$exists": false}}
	for k, v := range active {
		unlinkedFilter[k] = v
	}
	unlinked, err := db.Collection("bookings").CountDocuments(ctx, unlinkedFilter)
	must(err)
	legacy, err := db.Collection("bookings").CountDocuments(ctx, bson.M{
		"sport_center_id": centerID, "status": bson.M{"$in": []string{"confirmed", "pending"}},
		"schedule_schema_version": bson.M{"$exists": false},
	})
	must(err)

	log.Printf("[CUTOVER] center=%s (%s) mode=%s generation=%d active=%d unlinked=%d legacy_without_v2=%d",
		center.Name, centerID.Hex(), center.Authority, center.Generation, activeCount, unlinked, legacy)

	var newMode string
	bump := false
	switch action {
	case "status":
		return
	case "shadow":
		newMode = "shadow"
	case "authority":
		if (unlinked > 0 || legacy > 0) && os.Getenv("FORCE") != "true" {
			log.Fatalf("[CUTOVER] refused: %d active bookings are not imported and %d lack v2 fields. "+
				"Run cmd/migrate-booking-schedule-fields and cmd/reconcile-occupancy MODE=import first.", unlinked, legacy)
		}
		newMode, bump = "authority", true
	case "off":
		newMode, bump = "off", true
	default:
		log.Fatal("ACTION must be status, shadow, authority or off")
	}

	gen := center.Generation
	if bump {
		gen++
	}
	log.Printf("[CUTOVER] plan: mode %s -> %s, generation %d -> %d", center.Authority, newMode, center.Generation, gen)
	if !apply {
		log.Printf("[CUTOVER] DRY_RUN: nothing written. Re-run with APPLY=true.")
		return
	}

	if bump {
		cursor, err := db.Collection("spaces").Find(ctx, bson.M{"sport_center_id": centerID}, options.Find().SetProjection(bson.M{"_id": 1}))
		must(err)
		var spaces []struct {
			ID primitive.ObjectID `bson:"_id"`
		}
		must(cursor.All(ctx, &spaces))
		ids := make([]primitive.ObjectID, len(spaces))
		for i, sp := range spaces {
			ids[i] = sp.ID
		}

		c, err := occredis.NewCoordinator(occredis.Config{URL: env("REDIS_URL", ""), KeyPrefix: env("REDIS_KEY_PREFIX", "reservaloya")})
		must(err)
		defer c.Close()
		must(c.Health(ctx))
		// Redis first: from here on, writers with the old generation are rejected.
		var publisher app.GenerationPublisher = c
		must(publisher.PublishGeneration(ctx, centerID, ids, gen))
		log.Printf("[CUTOVER] generation %d published to %d spaces in Redis", gen, len(ids))
	}

	res, err := db.Collection("sport_centers").UpdateOne(ctx,
		bson.M{"_id": centerID, "occupancy_generation": bson.M{"$in": []int64{center.Generation}}},
		bson.M{"$set": bson.M{"occupancy_authority": newMode, "occupancy_generation": gen}})
	must(err)
	if res.MatchedCount == 0 && center.Generation != 0 {
		log.Fatal("[CUTOVER] center changed concurrently; re-run")
	}
	if res.MatchedCount == 0 { // generation field absent (0): match on the center only
		_, err = db.Collection("sport_centers").UpdateOne(ctx, bson.M{"_id": centerID},
			bson.M{"$set": bson.M{"occupancy_authority": newMode, "occupancy_generation": gen}})
		must(err)
	}
	log.Printf("[CUTOVER] done: mode=%s generation=%d (instances pick it up within ~30s)", newMode, gen)
}

func must(err error) {
	if err != nil {
		log.Fatalf("[CUTOVER] %v", err)
	}
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
