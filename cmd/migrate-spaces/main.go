package main

import (
	"context"
	"log"
	"os"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// migrate-spaces migra las colecciones `courts` y `facilities` a una única
// colección `spaces` con campo `category` (cancha | quincho | sede | piscina).
//
// Preserva el `_id` de cada documento para que `bookings.court_id`,
// `recurring_reservations.court_id`, `slot_holds.court_id` y
// `facility_inquiries.facility_id` sigan apuntando a IDs válidos.
//
// Es idempotente: re-ejecutarlo no duplica documentos (usa upsert por `_id`).
// Opcionalmente elimina las colecciones originales con DROP_ORIGINAL=true.
//
//	MONGO_URI=mongodb://localhost:27017 go run ./cmd/migrate-spaces
//	DROP_ORIGINAL=true MONGO_URI=... go run ./cmd/migrate-spaces
func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	mongoURI := os.Getenv("MONGO_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}

	dbName := os.Getenv("MONGO_DB")
	if dbName == "" {
		dbName = "sport_booking"
	}

	dropOriginal := os.Getenv("DROP_ORIGINAL") == "true"

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		log.Fatalf("Error connecting to MongoDB: %v", err)
	}
	defer client.Disconnect(ctx)

	if err := client.Ping(ctx, nil); err != nil {
		log.Fatalf("Error pinging MongoDB: %v", err)
	}

	db := client.Database(dbName)
	courtsColl := db.Collection("courts")
	facilitiesColl := db.Collection("facilities")
	spacesColl := db.Collection("spaces")
	centersColl := db.Collection("sport_centers")

	log.Println("Migrating courts -> spaces (category=cancha)...")
	courtsMigrated, courtsErrors := migrateCourts(ctx, courtsColl, spacesColl)

	log.Println("Migrating facilities -> spaces (category=type)...")
	facilitiesMigrated, facilitiesErrors := migrateFacilities(ctx, facilitiesColl, spacesColl)

	log.Println("Recomputing courts_count on sport_centers...")
	if err := recomputeCourtsCount(ctx, spacesColl, centersColl); err != nil {
		log.Printf("Warning recomputing courts_count: %v", err)
	}

	if dropOriginal {
		if err := courtsColl.Drop(ctx); err != nil {
			log.Printf("Warning dropping courts collection: %v", err)
		}
		if err := facilitiesColl.Drop(ctx); err != nil {
			log.Printf("Warning dropping facilities collection: %v", err)
		}
		log.Println("Original collections dropped.")
	} else {
		log.Println("Original collections kept (set DROP_ORIGINAL=true to drop them).")
	}

	log.Printf("Migration complete. courts: %d migrated, %d errors | facilities: %d migrated, %d errors",
		courtsMigrated, courtsErrors, facilitiesMigrated, facilitiesErrors)
}

func migrateCourts(ctx context.Context, courtsColl, spacesColl *mongo.Collection) (int, int) {
	cursor, err := courtsColl.Find(ctx, bson.M{})
	if err != nil {
		log.Fatalf("Error reading courts: %v", err)
	}
	defer cursor.Close(ctx)

	var migrated, errors int
	for cursor.Next(ctx) {
		var doc bson.M
		if err := cursor.Decode(&doc); err != nil {
			errors++
			continue
		}
		id, ok := doc["_id"]
		if !ok {
			errors++
			continue
		}

		doc["category"] = "cancha"
		doc["enabled"] = true
		if doc["type"] != nil {
			delete(doc, "type")
		}
		if doc["created_at"] == nil {
			doc["created_at"] = time.Now()
		}
		if doc["updated_at"] == nil {
			doc["updated_at"] = time.Now()
		}

		opts := options.Replace().SetUpsert(true)
		res, err := spacesColl.ReplaceOne(ctx, bson.M{"_id": id}, doc, opts)
		if err != nil {
			log.Printf("Error upserting court %v: %v", id, err)
			errors++
			continue
		}
		if res.UpsertedCount > 0 {
			migrated++
		}
	}
	return migrated, errors
}

func migrateFacilities(ctx context.Context, facilitiesColl, spacesColl *mongo.Collection) (int, int) {
	cursor, err := facilitiesColl.Find(ctx, bson.M{})
	if err != nil {
		log.Fatalf("Error reading facilities: %v", err)
	}
	defer cursor.Close(ctx)

	var migrated, errors int
	for cursor.Next(ctx) {
		var doc bson.M
		if err := cursor.Decode(&doc); err != nil {
			errors++
			continue
		}
		id, ok := doc["_id"]
		if !ok {
			errors++
			continue
		}

		category, ok := doc["type"].(string)
		if !ok || category == "" {
			category = "quincho"
		}
		doc["category"] = category
		if doc["enabled"] == nil {
			doc["enabled"] = true
		}
		if doc["created_at"] == nil {
			doc["created_at"] = time.Now()
		}
		if doc["updated_at"] == nil {
			doc["updated_at"] = time.Now()
		}

		opts := options.Replace().SetUpsert(true)
		res, err := spacesColl.ReplaceOne(ctx, bson.M{"_id": id}, doc, opts)
		if err != nil {
			log.Printf("Error upserting facility %v: %v", id, err)
			errors++
			continue
		}
		if res.UpsertedCount > 0 {
			migrated++
		}
	}
	return migrated, errors
}

func recomputeCourtsCount(ctx context.Context, spacesColl, centersColl *mongo.Collection) error {
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"category": "cancha"}}},
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: "$sport_center_id"},
			{Key: "count", Value: bson.D{{Key: "$sum", Value: 1}}},
		}}},
	}
	cursor, err := spacesColl.Aggregate(ctx, pipeline)
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)

	var results []struct {
		ID    interface{} `bson:"_id"`
		Count int         `bson:"count"`
	}
	if err := cursor.All(ctx, &results); err != nil {
		return err
	}

	for _, r := range results {
		if _, err := centersColl.UpdateOne(ctx, bson.M{"_id": r.ID}, bson.M{"$set": bson.M{"courts_count": r.Count, "updated_at": time.Now()}}); err != nil {
			log.Printf("Warning updating courts_count for center %v: %v", r.ID, err)
		}
	}
	return nil
}
