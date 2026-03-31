package mongo

import (
	"context"
	"log"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func EnsureIndexes(ctx context.Context, db *mongo.Database) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	log.Println("[MONGODB] Creating indexes...")

	if err := ensureSportCenterIndexes(ctx, db); err != nil {
		return err
	}

	if err := ensureCourtIndexes(ctx, db); err != nil {
		return err
	}

	if err := ensureBookingIndexes(ctx, db); err != nil {
		return err
	}

	if err := ensureUserIndexes(ctx, db); err != nil {
		return err
	}

	log.Println("[MONGODB] All indexes created successfully")
	return nil
}

func ensureSportCenterIndexes(ctx context.Context, db *mongo.Database) error {
	collection := db.Collection("sport_centers")

	indexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "slug", Value: 1}},
			Options: options.Index().SetUnique(true).SetName("idx_sport_centers_slug"),
		},
		{
			Keys:    bson.D{{Key: "users", Value: 1}},
			Options: options.Index().SetName("idx_sport_centers_users"),
		},
		{
			Keys:    bson.D{{Key: "city", Value: 1}},
			Options: options.Index().SetName("idx_sport_centers_city"),
		},
	}

	_, err := collection.Indexes().CreateMany(ctx, indexes)
	if err != nil {
		log.Printf("[MONGODB] Error creating sport_centers indexes: %v", err)
		return err
	}

	log.Println("[MONGODB] sport_centers indexes created")
	return nil
}

func ensureCourtIndexes(ctx context.Context, db *mongo.Database) error {
	collection := db.Collection("courts")

	indexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "sport_center_id", Value: 1}},
			Options: options.Index().SetName("idx_courts_sport_center_id"),
		},
	}

	_, err := collection.Indexes().CreateMany(ctx, indexes)
	if err != nil {
		log.Printf("[MONGODB] Error creating courts indexes: %v", err)
		return err
	}

	log.Println("[MONGODB] courts indexes created")
	return nil
}

func ensureBookingIndexes(ctx context.Context, db *mongo.Database) error {
	collection := db.Collection("bookings")

	indexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "booking_code", Value: 1}},
			Options: options.Index().SetUnique(true).SetName("idx_bookings_booking_code"),
		},
		{
			Keys:    bson.D{{Key: "fintoc_payment_id", Value: 1}},
			Options: options.Index().SetName("idx_bookings_fintoc_payment_id"),
		},
		{
			Keys:    bson.D{{Key: "fintoc_payment_intent_id", Value: 1}},
			Options: options.Index().SetName("idx_bookings_fintoc_payment_intent_id"),
		},
		{
			Keys:    bson.D{{Key: "preference_id", Value: 1}},
			Options: options.Index().SetName("idx_bookings_preference_id"),
		},
		{
			Keys: bson.D{
				{Key: "court_id", Value: 1},
				{Key: "date", Value: 1},
			},
			Options: options.Index().SetName("idx_bookings_court_date"),
		},
		{
			Keys: bson.D{
				{Key: "sport_center_id", Value: 1},
				{Key: "date", Value: 1},
				{Key: "status", Value: 1},
			},
			Options: options.Index().SetName("idx_bookings_center_date_status"),
		},
		{
			Keys: bson.D{
				{Key: "user_id", Value: 1},
				{Key: "date", Value: 1},
				{Key: "hour", Value: 1},
			},
			Options: options.Index().SetName("idx_bookings_user_date_hour"),
		},
		{
			Keys:    bson.D{{Key: "user_id", Value: 1}},
			Options: options.Index().SetName("idx_bookings_user_id"),
		},
		{
			Keys: bson.D{
				{Key: "sport_center_id", Value: 1},
				{Key: "created_at", Value: -1},
			},
			Options: options.Index().SetName("idx_bookings_center_created"),
		},
		{
			Keys: bson.D{
				{Key: "user_id", Value: 1},
				{Key: "status", Value: 1},
			},
			Options: options.Index().SetName("idx_bookings_user_status"),
		},
	}

	_, err := collection.Indexes().CreateMany(ctx, indexes)
	if err != nil {
		log.Printf("[MONGODB] Error creating bookings indexes: %v", err)
		return err
	}

	log.Println("[MONGODB] bookings indexes created")
	return nil
}

func ensureUserIndexes(ctx context.Context, db *mongo.Database) error {
	collection := db.Collection("users")

	indexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "username", Value: 1}},
			Options: options.Index().SetUnique(true).SetName("idx_users_username"),
		},
	}

	_, err := collection.Indexes().CreateMany(ctx, indexes)
	if err != nil {
		log.Printf("[MONGODB] Error creating users indexes: %v", err)
		return err
	}

	log.Println("[MONGODB] users indexes created")
	return nil
}
