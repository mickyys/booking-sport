package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/hamp/booking-sport/internal/app"
	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// migrate-schedule-rules migra el horario embebido `spaces.schedule`
// (modelo legacy de slots por hora) a la colección `schedule_rules`
// (modelo de reglas semanales con franjas por rango).
//
// Cada slot `CourtSchedule` se convierte en una franja de 60 minutos con su
// precio y reglas de pago. Los slots con `day_of_week == nil` se agrupan en
// una regla que aplica a todos los días; los slots con `day_of_week` específico
// se agrupan en una regla para ese día.
//
// Es idempotente: si ya existe una regla para el mismo espacio y días, se omite
// (a menos que FORCE=true, que la reemplaza).
//
//	MONGO_URI=mongodb://localhost:27017 go run ./cmd/migrate-schedule-rules
//	DRY_RUN=true MONGO_URI=... go run ./cmd/migrate-schedule-rules
//	FORCE=true MONGO_URI=... go run ./cmd/migrate-schedule-rules
func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	mongoURI := os.Getenv("MONGO_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}
	dbName := os.Getenv("MONGO_DB")
	if dbName == "" {
		dbName = "sport_booking"
	}
	dryRun := os.Getenv("DRY_RUN") == "true"
	force := os.Getenv("FORCE") == "true"

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		log.Fatalf("Error connecting to MongoDB: %v", err)
	}
	defer client.Disconnect(ctx)

	if err := client.Ping(ctx, nil); err != nil {
		log.Fatalf("Error pinging MongoDB: %v", err)
	}

	db := client.Database(dbName)
	spacesColl := db.Collection("spaces")
	rulesColl := db.Collection("schedule_rules")

	var migrated, skipped, errors, totalSlots int

	cursor, err := spacesColl.Find(ctx, bson.M{})
	if err != nil {
		log.Fatalf("Error reading spaces: %v", err)
	}
	defer cursor.Close(ctx)

	for cursor.Next(ctx) {
		var space struct {
			ID            primitive.ObjectID     `bson:"_id"`
			SportCenterID primitive.ObjectID     `bson:"sport_center_id"`
			Category      string                 `bson:"category"`
			Name          string                 `bson:"name"`
			Schedule      []domain.CourtSchedule `bson:"schedule"`
			OpenHour      int                    `bson:"open_hour"`
			CloseHour     int                    `bson:"close_hour"`
		}
		if err := cursor.Decode(&space); err != nil {
			errors++
			continue
		}

		var rules []domain.ScheduleRule
		switch space.Category {
		case string(domain.SpaceCategoryCancha):
			rules = app.BuildRulesFromLegacyCourtSchedule(space.Schedule)
		case string(domain.SpaceCategoryQuincho):
			rules = app.BuildRulesFromLegacyFacility(space.OpenHour, space.CloseHour)
		case string(domain.SpaceCategorySede), string(domain.SpaceCategoryPiscina):
			rules = app.BuildFullDayRule()
		default:
			// categoría desconocida: sin reglas
			continue
		}

		if dryRun {
			log.Printf("[DRY_RUN] space=%s (%s) category=%s: %d slot(s) -> %d rule(s)",
				space.ID.Hex(), space.Name, space.Category, len(space.Schedule), len(rules))
			totalSlots += len(space.Schedule)
			for _, r := range rules {
				log.Printf("[DRY_RUN]   rule days=%v franjas=%d", r.DaysOfWeek, len(r.Slots))
			}
			continue
		}

		for _, rule := range rules {
			rule.SpaceID = space.ID
			rule.SportCenterID = space.SportCenterID
			rule.Active = true

			existing := findExistingRule(ctx, rulesColl, space.ID, rule.DaysOfWeek)
			if existing != nil && !force {
				skipped++
				continue
			}

			now := time.Now()
			if existing != nil {
				rule.ID = existing.ID
				rule.CreatedAt = existing.CreatedAt
				rule.UpdatedAt = now
				if _, err := rulesColl.ReplaceOne(ctx, bson.M{"_id": rule.ID}, rule); err != nil {
					log.Printf("Error updating rule for space %s: %v", space.ID.Hex(), err)
					errors++
					continue
				}
			} else {
				rule.ID = primitive.NewObjectID()
				rule.CreatedAt = now
				rule.UpdatedAt = now
				if _, err := rulesColl.InsertOne(ctx, rule); err != nil {
					log.Printf("Error inserting rule for space %s: %v", space.ID.Hex(), err)
					errors++
					continue
				}
			}
			migrated++
			totalSlots += len(rule.Slots)
		}
	}

	log.Printf("Migration complete (dry_run=%v, force=%v). slots=%d, rules=%d, skipped=%d, errors=%d",
		dryRun, force, totalSlots, migrated, skipped, errors)
}

// ==================== Helpers de la migración ====================

// La conversión legacy → reglas vive en internal/app/legacy_schedule.go y se
// comparte con el fallback de lectura del panel de administración.

func findExistingRule(ctx context.Context, coll *mongo.Collection, spaceID primitive.ObjectID, days []int) *domain.ScheduleRule {
	filter := bson.M{"space_id": spaceID, "days_of_week": days}
	var rule domain.ScheduleRule
	err := coll.FindOne(ctx, filter).Decode(&rule)
	if err != nil {
		return nil
	}
	return &rule
}
