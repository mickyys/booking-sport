// Command migrate-booking-schedule-fields backfills the canonical UTC fields of
// existing bookings: scheduled_at, ends_at, timezone, local_date and
// schedule_schema_version.
//
// It is idempotent and only touches legacy documents (schedule_schema_version
// missing or lower than 2). Documents already written with the v2 contract are
// never modified.
//
// Por defecto corre en modo DRY_RUN: sólo informa. Para aplicar cambios:
//
//	APPLY=true MONGO_URI=... go run ./cmd/migrate-booking-schedule-fields
//
// Filtros opcionales:
//
//	FROM=2026-01-01 TO=2026-12-31   rango sobre la fecha civil (date)
//	LIMIT=1000                      máximo de documentos a procesar
//	CENTER_ID=<objectid>            sólo un centro
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"github.com/hamp/booking-sport/internal/domain/civil"
	"github.com/hamp/booking-sport/pkg/migratelog"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const schemaVersionUTC = 2

type scheduleDocument struct {
	ID                    primitive.ObjectID `bson:"_id"`
	SportCenterID         primitive.ObjectID `bson:"sport_center_id"`
	Date                  time.Time          `bson:"date"`
	Hour                  int                `bson:"hour"`
	Minutes               int                `bson:"minutes"`
	DurationMinutes       int                `bson:"duration_minutes"`
	Timezone              string             `bson:"timezone"`
	ScheduleSchemaVersion int                `bson:"schedule_schema_version"`
}

type centerTimezone struct {
	ID       primitive.ObjectID `bson:"_id"`
	Timezone string             `bson:"timezone"`
}

type stats struct {
	scanned  int
	updated  int
	skipped  int
	byReason map[string]int
}

func (s *stats) skip(reason string) {
	s.skipped++
	if s.byReason == nil {
		s.byReason = map[string]int{}
	}
	s.byReason[reason]++
}

func main() {
	defer migratelog.Start("migrate-booking-schedule-fields")()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	mongoURI := env("MONGO_URI", "mongodb://localhost:27017")
	dbName := env("MONGO_DB", "sport_booking")
	apply := env("APPLY", "false") == "true"
	limit := int64(envInt("LIMIT", 0))
	batchSize := int32(envInt("BATCH_SIZE", 500))

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		log.Fatalf("connect to MongoDB: %v", err)
	}
	defer client.Disconnect(context.Background())

	if err := client.Ping(ctx, nil); err != nil {
		log.Fatalf("ping MongoDB: %v", err)
	}
	db := client.Database(dbName)

	zones, err := loadCenterTimezones(ctx, db)
	if err != nil {
		log.Fatalf("load sport centers: %v", err)
	}

	filter, err := buildFilter()
	if err != nil {
		log.Fatalf("filter: %v", err)
	}

	mode := "DRY_RUN"
	if apply {
		mode = "APPLY"
	}
	log.Printf("[MIGRATE] mode=%s db=%s batch_size=%d", mode, dbName, batchSize)

	opts := options.Find().
		SetBatchSize(batchSize).
		SetSort(bson.D{{Key: "_id", Value: 1}})
	if limit > 0 {
		opts.SetLimit(limit)
	}

	cursor, err := db.Collection("bookings").Find(ctx, filter, opts)
	if err != nil {
		log.Fatalf("find bookings: %v", err)
	}
	defer cursor.Close(ctx)

	result := &stats{byReason: map[string]int{}}

	for cursor.Next(ctx) {
		var document scheduleDocument
		if err := cursor.Decode(&document); err != nil {
			log.Fatalf("decode booking: %v", err)
		}
		result.scanned++

		if document.ScheduleSchemaVersion >= schemaVersionUTC {
			result.skip("already_v2")
			continue
		}

		loc, err := resolveLocation(document, zones)
		if err != nil {
			result.skip("invalid_timezone")
			log.Printf("[MIGRATE] %s skipped: %v", document.ID.Hex(), err)
			continue
		}

		canonicalDate, localDate, scheduledAt, err := civil.Normalize(document.Date, document.Hour, document.Minutes, loc)
		if err != nil {
			reason := "normalize_error"
			var nonexistent *civil.NonexistentLocalTimeError
			var ambiguous *civil.AmbiguousLocalTimeError
			switch {
			case errors.As(err, &nonexistent):
				reason = "nonexistent_local_time"
			case errors.As(err, &ambiguous):
				reason = "ambiguous_local_time"
			}
			result.skip(reason)
			log.Printf("[MIGRATE] %s skipped (%s): %v", document.ID.Hex(), reason, err)
			continue
		}

		duration := document.DurationMinutes
		if duration <= 0 {
			duration = 60
		}

		set := bson.M{
			"date":                    canonicalDate,
			"local_date":              localDate,
			"timezone":                loc.String(),
			"scheduled_at":            scheduledAt.UTC(),
			"duration_minutes":        duration,
			"ends_at":                 scheduledAt.UTC().Add(time.Duration(duration) * time.Minute),
			"schedule_schema_version": schemaVersionUTC,
		}

		if !apply {
			result.updated++
			continue
		}

		// Conditional update: never overwrite a document that another writer
		// already migrated.
		updateResult, err := db.Collection("bookings").UpdateOne(ctx,
			bson.M{
				"_id": document.ID,
				"$or": []bson.M{
					{"schedule_schema_version": bson.M{"$exists": false}},
					{"schedule_schema_version": bson.M{"$lt": schemaVersionUTC}},
				},
			},
			bson.M{"$set": set},
		)
		if err != nil {
			log.Fatalf("update %s: %v", document.ID.Hex(), err)
		}
		if updateResult.ModifiedCount == 0 {
			result.skip("concurrent_update")
			continue
		}
		result.updated++

		if result.updated%1000 == 0 {
			log.Printf("[MIGRATE] progress scanned=%d updated=%d skipped=%d", result.scanned, result.updated, result.skipped)
		}
	}

	if err := cursor.Err(); err != nil {
		log.Fatalf("cursor: %v", err)
	}

	log.Printf("[MIGRATE] done mode=%s scanned=%d updated=%d skipped=%d",
		mode, result.scanned, result.updated, result.skipped)

	reasons := make([]string, 0, len(result.byReason))
	for reason := range result.byReason {
		reasons = append(reasons, reason)
	}
	sort.Strings(reasons)
	for _, reason := range reasons {
		log.Printf("[MIGRATE]   %-24s %d", reason, result.byReason[reason])
	}

	if !apply {
		log.Printf("[MIGRATE] DRY_RUN: no se escribió nada. Repetir con APPLY=true para aplicar.")
	}
}

func buildFilter() (bson.M, error) {
	filter := bson.M{
		"$or": []bson.M{
			{"schedule_schema_version": bson.M{"$exists": false}},
			{"schedule_schema_version": bson.M{"$lt": schemaVersionUTC}},
		},
	}

	if centerID := os.Getenv("CENTER_ID"); centerID != "" {
		oid, err := primitive.ObjectIDFromHex(centerID)
		if err != nil {
			return nil, fmt.Errorf("invalid CENTER_ID: %w", err)
		}
		filter["sport_center_id"] = oid
	}

	from, to := os.Getenv("FROM"), os.Getenv("TO")
	if from != "" || to != "" {
		rangeFilter := bson.M{}
		if from != "" {
			date, err := time.Parse(civil.DateLayout, from)
			if err != nil {
				return nil, fmt.Errorf("invalid FROM: %w", err)
			}
			rangeFilter["$gte"] = date
		}
		if to != "" {
			date, err := time.Parse(civil.DateLayout, to)
			if err != nil {
				return nil, fmt.Errorf("invalid TO: %w", err)
			}
			rangeFilter["$lte"] = date
		}
		filter["date"] = rangeFilter
	}

	return filter, nil
}

func loadCenterTimezones(ctx context.Context, db *mongo.Database) (map[primitive.ObjectID]string, error) {
	cursor, err := db.Collection("sport_centers").Find(ctx, bson.M{},
		options.Find().SetProjection(bson.M{"timezone": 1}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	zones := map[primitive.ObjectID]string{}
	for cursor.Next(ctx) {
		var center centerTimezone
		if err := cursor.Decode(&center); err != nil {
			return nil, err
		}
		zones[center.ID] = center.Timezone
	}
	return zones, cursor.Err()
}

func resolveLocation(document scheduleDocument, zones map[primitive.ObjectID]string) (*time.Location, error) {
	name := document.Timezone
	if name == "" {
		name = zones[document.SportCenterID]
	}
	if name == "" {
		return domain.GetSantiagoLocation(), nil
	}
	return domain.LoadTimezone(name)
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return fallback
	}
	return parsed
}
