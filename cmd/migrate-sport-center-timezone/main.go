// Command migrate-sport-center-timezone assigns an IANA timezone to sport
// centers that do not have one yet.
//
// Historic centers are interpreted as America/Santiago by the booking domain
// until this migration runs (see docs/README-reservas-utc-redis.md). The
// command is idempotent: centers that already have a timezone are never
// modified.
//
// Por defecto corre en modo DRY_RUN:
//
//	MONGO_URI=... go run ./cmd/migrate-sport-center-timezone
//
// Para aplicar:
//
//	APPLY=true MONGO_URI=... go run ./cmd/migrate-sport-center-timezone
//
// Opciones:
//
//	TIMEZONE=America/Santiago   zona a asignar (default America/Santiago)
//	CENTER_ID=<objectid>        sólo un centro (permite corregir uno concreto)
package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type center struct {
	ID       primitive.ObjectID `bson:"_id"`
	Name     string             `bson:"name"`
	Slug     string             `bson:"slug"`
	City     string             `bson:"city"`
	Timezone string             `bson:"timezone"`
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	mongoURI := env("MONGO_URI", "mongodb://localhost:27017")
	dbName := env("MONGO_DB", "sport_booking")
	apply := env("APPLY", "false") == "true"
	timezone := env("TIMEZONE", domain.SantiagoTimezone)

	if _, err := domain.LoadTimezone(timezone); err != nil {
		log.Fatalf("TIMEZONE inválida: %v", err)
	}

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		log.Fatalf("connect to MongoDB: %v", err)
	}
	defer client.Disconnect(context.Background())

	if err := client.Ping(ctx, nil); err != nil {
		log.Fatalf("ping MongoDB: %v", err)
	}
	db := client.Database(dbName)

	filter := bson.M{
		"$or": []bson.M{
			{"timezone": bson.M{"$exists": false}},
			{"timezone": ""},
		},
	}

	if centerID := os.Getenv("CENTER_ID"); centerID != "" {
		oid, err := primitive.ObjectIDFromHex(centerID)
		if err != nil {
			log.Fatalf("CENTER_ID inválido: %v", err)
		}
		filter["_id"] = oid
	}

	cursor, err := db.Collection("sport_centers").Find(ctx, filter,
		options.Find().SetSort(bson.D{{Key: "_id", Value: 1}}))
	if err != nil {
		log.Fatalf("find sport_centers: %v", err)
	}
	defer cursor.Close(ctx)

	mode := "DRY_RUN"
	if apply {
		mode = "APPLY"
	}
	log.Printf("[TZ] mode=%s db=%s timezone=%s", mode, dbName, timezone)

	var candidates []center
	for cursor.Next(ctx) {
		var c center
		if err := cursor.Decode(&c); err != nil {
			log.Fatalf("decode sport center: %v", err)
		}
		candidates = append(candidates, c)
	}
	if err := cursor.Err(); err != nil {
		log.Fatalf("cursor: %v", err)
	}

	if len(candidates) == 0 {
		log.Printf("[TZ] no hay centros sin timezone")
		return
	}

	for _, c := range candidates {
		log.Printf("[TZ] %-24s %-28s city=%-20q slug=%s", c.ID.Hex(), c.Name, c.City, c.Slug)
	}

	if !apply {
		log.Printf("[TZ] DRY_RUN: %d centro(s) se asignarían a %s. Repetir con APPLY=true.", len(candidates), timezone)
		return
	}

	updated := int64(0)
	for _, c := range candidates {
		// Conditional per center: never overwrite a timezone set by another
		// operator while this command was running.
		result, err := db.Collection("sport_centers").UpdateOne(ctx,
			bson.M{
				"_id": c.ID,
				"$or": []bson.M{
					{"timezone": bson.M{"$exists": false}},
					{"timezone": ""},
				},
			},
			bson.M{"$set": bson.M{"timezone": timezone}},
		)
		if err != nil {
			log.Fatalf("update sport center %s: %v", c.ID.Hex(), err)
		}
		updated += result.ModifiedCount
	}

	log.Printf("[TZ] done: candidates=%d modified=%d timezone=%s", len(candidates), updated, timezone)
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
