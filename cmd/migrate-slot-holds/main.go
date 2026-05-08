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

func main() {
	mongoURI := os.Getenv("MONGO_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}

	log.Println("Migrating existing PENDING bookings to use slot_holds...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		log.Fatal(err)
	}
	defer client.Disconnect(ctx)

	db := client.Database("sport_booking")
	bookingsColl := db.Collection("bookings")
	holdsColl := db.Collection("slot_holds")

	cursor, err := bookingsColl.Find(ctx, bson.M{
		"status":  "pending",
		"hold_id": bson.M{"$exists": false},
	})
	if err != nil {
		log.Fatal(err)
	}
	defer cursor.Close(ctx)

	var migrated, skipped, errors int

	for cursor.Next(ctx) {
		var booking struct {
			ID        interface{} `bson:"_id"`
			CourtID   interface{} `bson:"court_id"`
			Date      time.Time   `bson:"date"`
			Hour      int         `bson:"hour"`
			Minutes   int         `bson:"minutes"`
			UserID    string      `bson:"user_id"`
			CreatedAt time.Time   `bson:"created_at"`
		}
		if err := cursor.Decode(&booking); err != nil {
			errors++
			continue
		}

		hold := bson.M{
			"court_id":   booking.CourtID,
			"date":       booking.Date,
			"hour":       booking.Hour,
			"minutes":    booking.Minutes,
			"user_id":    booking.UserID,
			"booking_id": booking.ID,
			"expires_at": booking.CreatedAt.Add(20 * time.Minute),
			"created_at": booking.CreatedAt,
		}

		if time.Now().After(booking.CreatedAt.Add(20 * time.Minute)) {
			bookingsColl.UpdateOne(ctx,
				bson.M{"_id": booking.ID},
				bson.M{"$set": bson.M{
					"status":     "expired",
					"expired_at": time.Now(),
					"updated_at": time.Now(),
				}},
			)
			skipped++
			continue
		}

		var existingHold bson.M
		err := holdsColl.FindOne(ctx, bson.M{
			"court_id": booking.CourtID,
			"date":     booking.Date,
			"hour":     booking.Hour,
		}).Decode(&existingHold)

		if err == mongo.ErrNoDocuments {
			result, err := holdsColl.InsertOne(ctx, hold)
			if err != nil {
				errors++
				continue
			}

			bookingsColl.UpdateOne(ctx,
				bson.M{"_id": booking.ID},
				bson.M{"$set": bson.M{
					"hold_id":         result.InsertedID,
					"lock_expires_at": booking.CreatedAt.Add(20 * time.Minute),
					"version":         1,
				}},
			)
			migrated++
		} else {
			skipped++
		}
	}

	log.Printf("Migration complete: %d migrated, %d skipped, %d errors", migrated, skipped, errors)
}
