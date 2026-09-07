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

type scheduleDocument struct {
	ID      primitive.ObjectID `bson:"_id"`
	Date    time.Time          `bson:"date"`
	Hour    int                `bson:"hour"`
	Minutes int                `bson:"minutes"`
}

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

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		log.Fatalf("connect to MongoDB: %v", err)
	}
	defer client.Disconnect(context.Background())

	if err := client.Ping(ctx, nil); err != nil {
		log.Fatalf("ping MongoDB: %v", err)
	}

	db := client.Database(dbName)
	for _, collectionName := range []string{"bookings", "slot_holds"} {
		updated, skipped, err := backfillCollection(ctx, db.Collection(collectionName))
		if err != nil {
			log.Fatalf("backfill %s: %v", collectionName, err)
		}
		log.Printf("%s: updated=%d skipped=%d", collectionName, updated, skipped)
	}
}

func backfillCollection(ctx context.Context, collection *mongo.Collection) (int64, int64, error) {
	cursor, err := collection.Find(ctx, bson.M{"local_date": bson.M{"$exists": false}})
	if err != nil {
		return 0, 0, err
	}
	defer cursor.Close(ctx)

	var updated, skipped int64
	for cursor.Next(ctx) {
		var document scheduleDocument
		if err := cursor.Decode(&document); err != nil {
			return updated, skipped, err
		}

		date, localDate, timezone, scheduledAt, err := domain.NormalizeSchedule(document.Date, document.Hour, document.Minutes)
		if err != nil {
			log.Printf("%s/%s skipped: %v", collection.Name(), document.ID.Hex(), err)
			skipped++
			continue
		}

		result, err := collection.UpdateOne(ctx,
			bson.M{"_id": document.ID, "local_date": bson.M{"$exists": false}},
			bson.M{"$set": bson.M{
				"date":         date,
				"local_date":   localDate,
				"timezone":     timezone,
				"scheduled_at": scheduledAt,
			}},
		)
		if err != nil {
			return updated, skipped, err
		}
		updated += result.ModifiedCount
	}

	return updated, skipped, cursor.Err()
}
