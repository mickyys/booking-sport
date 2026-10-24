package mongo

import (
	"context"
	"log"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
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

	if err := ensureSpaceIndexes(ctx, db); err != nil {
		return err
	}

	if err := ensureBookingIndexes(ctx, db); err != nil {
		return err
	}

	if err := ensureUserIndexes(ctx, db); err != nil {
		return err
	}

	if err := ensureRecurringReservationIndexes(ctx, db); err != nil {
		return err
	}

	if err := ensureSlotHoldIndexes(ctx, db); err != nil {
		return err
	}

	if err := ensureJobLockIndexes(ctx, db); err != nil {
		return err
	}

	if err := ensureFacilityIndexes(ctx, db); err != nil {
		return err
	}

	if err := ensureScheduleIndexes(ctx, db); err != nil {
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
			Keys:    bson.D{{Key: "subdomain", Value: 1}},
			Options: options.Index().SetUnique(true).SetSparse(true).SetName("idx_sport_centers_subdomain"),
		},
		{
			Keys:    bson.D{{Key: "domains", Value: 1}},
			Options: options.Index().SetUnique(true).SetSparse(true).SetName("idx_sport_centers_domains"),
		},
		{
			Keys:    bson.D{{Key: "users", Value: 1}},
			Options: options.Index().SetName("idx_sport_centers_users"),
		},
		{
			Keys: bson.D{{Key: "admin_email", Value: 1}},
			Options: options.Index().
				SetUnique(true).
				SetPartialFilterExpression(bson.M{"admin_email": bson.M{"$gt": ""}}).
				SetName("idx_sport_centers_admin_email_unique"),
		},
		{
			Keys:    bson.D{{Key: "city", Value: 1}},
			Options: options.Index().SetName("idx_sport_centers_city"),
		},
		{
			Keys:    bson.D{{Key: "location", Value: "2dsphere"}},
			Options: options.Index().SetName("idx_sport_centers_location"),
		},
		{
			Keys:    bson.D{{Key: "name", Value: 1}},
			Options: options.Index().SetName("idx_sport_centers_name"),
		},
		{
			Keys: bson.D{
				{Key: "name", Value: "text"},
				{Key: "city", Value: "text"},
			},
			Options: options.Index().SetName("idx_sport_centers_text"),
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

func ensureSpaceIndexes(ctx context.Context, db *mongo.Database) error {
	collection := db.Collection("spaces")

	indexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "sport_center_id", Value: 1}},
			Options: options.Index().SetName("idx_spaces_sport_center_id"),
		},
		{
			Keys: bson.D{
				{Key: "sport_center_id", Value: 1},
				{Key: "category", Value: 1},
			},
			Options: options.Index().SetName("idx_spaces_center_category"),
		},
		{
			Keys: bson.D{
				{Key: "sport_center_id", Value: 1},
				{Key: "enabled", Value: 1},
			},
			Options: options.Index().SetName("idx_spaces_center_enabled"),
		},
		{
			Keys:    bson.D{{Key: "category", Value: 1}},
			Options: options.Index().SetName("idx_spaces_category"),
		},
	}

	_, err := collection.Indexes().CreateMany(ctx, indexes)
	if err != nil {
		log.Printf("[MONGODB] Error creating spaces indexes: %v", err)
		return err
	}

	log.Println("[MONGODB] spaces indexes created")
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
			Keys:    bson.D{{Key: "preference_id", Value: 1}},
			Options: options.Index().SetName("idx_bookings_preference_id"),
		},
		{
			Keys:    bson.D{{Key: "mp_preference_id", Value: 1}},
			Options: options.Index().SetName("idx_bookings_mp_preference_id"),
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
				{Key: "sport_center_id", Value: 1},
				{Key: "date", Value: 1},
				{Key: "hour", Value: 1},
				{Key: "minutes", Value: 1},
				{Key: "status", Value: 1},
			},
			Options: options.Index().SetName("idx_bookings_center_date_hour_minute_status"),
		},
		{
			Keys: bson.D{
				{Key: "user_id", Value: 1},
				{Key: "date", Value: 1},
				{Key: "hour", Value: 1},
				{Key: "minutes", Value: 1},
			},
			Options: options.Index().SetName("idx_bookings_user_date_hour_minute"),
		},
		{
			Keys:    bson.D{{Key: "user_id", Value: 1}},
			Options: options.Index().SetName("idx_bookings_user_id"),
		},
		{
			Keys:    bson.D{{Key: "series_id", Value: 1}},
			Options: options.Index().SetName("idx_bookings_series_id"),
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
		{
			Keys: bson.D{
				{Key: "sport_center_id", Value: 1},
				{Key: "status", Value: 1},
			},
			Options: options.Index().SetName("idx_bookings_center_status"),
		},
		{
			Keys: bson.D{
				{Key: "sport_center_id", Value: 1},
				{Key: "local_date", Value: 1},
				{Key: "status", Value: 1},
			},
			Options: options.Index().SetName("idx_bookings_center_local_date_status"),
		},
		{
			Keys: bson.D{
				{Key: "court_id", Value: 1},
				{Key: "scheduled_at", Value: 1},
			},
			Options: options.Index().SetName("idx_bookings_court_scheduled_at"),
		},
		{
			Keys: bson.D{
				{Key: "court_id", Value: 1},
				{Key: "local_date", Value: 1},
				{Key: "hour", Value: 1},
				{Key: "minutes", Value: 1},
			},
			Options: options.Index().
				SetUnique(true).
				SetPartialFilterExpression(bson.M{"status": "confirmed", "local_date": bson.M{"$type": "string"}}).
				SetName("idx_bookings_unique_confirmed_local_slot"),
		},
		{
			Keys: bson.D{
				{Key: "court_id", Value: 1},
				{Key: "local_date", Value: 1},
				{Key: "hour", Value: 1},
				{Key: "minutes", Value: 1},
			},
			Options: options.Index().
				SetUnique(true).
				SetPartialFilterExpression(bson.M{"status": "pending", "local_date": bson.M{"$type": "string"}}).
				SetName("idx_bookings_unique_pending_local_slot"),
		},
		{
			Keys: bson.D{
				{Key: "court_id", Value: 1},
				{Key: "date", Value: 1},
				{Key: "hour", Value: 1},
				{Key: "minutes", Value: 1},
			},
			Options: options.Index().
				SetUnique(true).
				SetPartialFilterExpression(bson.M{"status": "confirmed"}).
				SetName("idx_bookings_unique_confirmed_slot_v2"),
		},
		{
			Keys: bson.D{
				{Key: "court_id", Value: 1},
				{Key: "date", Value: 1},
				{Key: "hour", Value: 1},
				{Key: "minutes", Value: 1},
			},
			Options: options.Index().
				SetUnique(true).
				SetPartialFilterExpression(bson.M{"status": "pending"}).
				SetName("idx_bookings_unique_pending_slot_v2"),
		},
		{
			Keys:    bson.D{{Key: "hold_id", Value: 1}},
			Options: options.Index().SetName("idx_bookings_hold_id"),
		},
		{
			Keys:    bson.D{{Key: "recurring_id", Value: 1}},
			Options: options.Index().SetName("idx_bookings_recurring_id"),
		},
		{
			Keys: bson.D{
				{Key: "recurring_id", Value: 1},
				{Key: "date", Value: 1},
			},
			Options: options.Index().SetName("idx_bookings_recurring_date"),
		},
		{
			Keys: bson.D{
				{Key: "recurring_id", Value: 1},
				{Key: "status", Value: 1},
				{Key: "scheduled_at", Value: 1},
			},
			Options: options.Index().SetName("idx_bookings_recurring_status_scheduled"),
		},
	}

	_, err := collection.Indexes().DropOne(ctx, "idx_bookings_ttl_pending")
	if err != nil {
		log.Printf("[MONGODB] DropOne idx_bookings_ttl_pending (ignorable if not exists): %v", err)
	}

	_, err = collection.Indexes().DropOne(ctx, "idx_bookings_unique_confirmed_slot")
	if err != nil {
		log.Printf("[MONGODB] DropOne idx_bookings_unique_confirmed_slot (ignorable if not exists): %v", err)
	}

	_, err = collection.Indexes().DropOne(ctx, "idx_bookings_unique_pending_slot")
	if err != nil {
		log.Printf("[MONGODB] DropOne idx_bookings_unique_pending_slot (ignorable if not exists): %v", err)
	}

	_, err = collection.Indexes().CreateMany(ctx, indexes)
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
			Keys: bson.D{
				{Key: "court_id", Value: 1},
				{Key: "local_date", Value: 1},
				{Key: "hour", Value: 1},
				{Key: "minutes", Value: 1},
			},
			Options: options.Index().
				SetUnique(true).
				SetPartialFilterExpression(bson.M{"local_date": bson.M{"$type": "string"}}).
				SetName("idx_slot_holds_unique_local_slot"),
		},
		{
			Keys: bson.D{
				{Key: "court_id", Value: 1},
				{Key: "scheduled_at", Value: 1},
			},
			Options: options.Index().SetName("idx_slot_holds_court_scheduled_at"),
		},
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

func ensureRecurringReservationIndexes(ctx context.Context, db *mongo.Database) error {
	collection := db.Collection("recurring_reservations")

	indexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "sport_center_id", Value: 1}},
			Options: options.Index().SetName("idx_recurring_sport_center_id"),
		},
		{
			Keys: bson.D{
				{Key: "sport_center_id", Value: 1},
				{Key: "day_of_week", Value: 1},
				{Key: "status", Value: 1},
			},
			Options: options.Index().SetName("idx_recurring_center_day_status"),
		},
		{
			Keys:    bson.D{{Key: "court_id", Value: 1}},
			Options: options.Index().SetName("idx_recurring_court_id"),
		},
		{
			Keys: bson.D{
				{Key: "court_id", Value: 1},
				{Key: "hour", Value: 1},
				{Key: "minutes", Value: 1},
				{Key: "status", Value: 1},
			},
			Options: options.Index().SetName("idx_recurring_court_hour_minute_status"),
		},
		{
			Keys:    bson.D{{Key: "status", Value: 1}},
			Options: options.Index().SetName("idx_recurring_status"),
		},
	}

	_, err := collection.Indexes().CreateMany(ctx, indexes)
	if err != nil {
		log.Printf("[MONGODB] Error creating recurring_reservations indexes: %v", err)
		return err
	}

	log.Println("[MONGODB] recurring_reservations indexes created")

	// Índice único parcial: evita crear dos recurrencias activas para la misma
	// cancha, hora y día. Se crea por separado para que, si existieran datos
	// históricos duplicados, no impida crear el resto de los índices.
	_, err = collection.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{
			{Key: "court_id", Value: 1},
			{Key: "hour", Value: 1},
			{Key: "minutes", Value: 1},
			{Key: "day_of_week", Value: 1},
		},
		Options: options.Index().
			SetUnique(true).
			SetPartialFilterExpression(bson.M{"status": domain.RecurringReservationStatusActive}).
			SetName("idx_recurring_unique_active_slot"),
	})
	if err != nil {
		log.Printf("[MONGODB] Warning creating idx_recurring_unique_active_slot (posibles duplicados existentes): %v", err)
	}

	return nil
}

func ensureJobLockIndexes(ctx context.Context, db *mongo.Database) error {
	collection := db.Collection("job_locks")
	indexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "expires_at", Value: 1}},
			Options: options.Index().SetExpireAfterSeconds(0).SetName("idx_job_locks_ttl"),
		},
	}
	_, err := collection.Indexes().CreateMany(ctx, indexes)
	if err != nil {
		log.Printf("[MONGODB] Error creating job_locks indexes: %v", err)
		return err
	}
	log.Println("[MONGODB] job_locks indexes created")
	return nil
}

func ensureSlotHoldIndexes(ctx context.Context, db *mongo.Database) error {
	collection := db.Collection("slot_holds")

	indexes := []mongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "court_id", Value: 1},
				{Key: "date", Value: 1},
				{Key: "hour", Value: 1},
				{Key: "minutes", Value: 1},
			},
			Options: options.Index().
				SetUnique(true).
				SetName("idx_slot_holds_unique_slot_v2"),
		},
		{
			Keys: bson.D{{Key: "expires_at", Value: 1}},
			Options: options.Index().
				SetExpireAfterSeconds(0).
				SetName("idx_slot_holds_ttl"),
		},
	}

	_, err := collection.Indexes().DropOne(ctx, "idx_slot_holds_unique_slot")
	if err != nil {
		log.Printf("[MONGODB] DropOne idx_slot_holds_unique_slot (ignorable if not exists): %v", err)
	}

	_, err = collection.Indexes().CreateMany(ctx, indexes)
	if err != nil {
		log.Printf("[MONGODB] Error creating slot_holds indexes: %v", err)
		return err
	}

	log.Println("[MONGODB] slot_holds indexes created")
	return nil
}

func ensureFacilityIndexes(ctx context.Context, db *mongo.Database) error {
	collection := db.Collection("facilities")

	indexes := []mongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "sport_center_id", Value: 1},
				{Key: "enabled", Value: 1},
			},
			Options: options.Index().SetName("idx_facilities_center_enabled"),
		},
		{
			Keys:    bson.D{{Key: "type", Value: 1}},
			Options: options.Index().SetName("idx_facilities_type"),
		},
	}

	_, err := collection.Indexes().CreateMany(ctx, indexes)
	if err != nil {
		log.Printf("[MONGODB] Error creating facilities indexes: %v", err)
		return err
	}

	inquiries := db.Collection("facility_inquiries")
	inqIndexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "sport_center_id", Value: 1}},
			Options: options.Index().SetName("idx_facility_inquiries_center"),
		},
		{
			Keys:    bson.D{{Key: "facility_id", Value: 1}},
			Options: options.Index().SetName("idx_facility_inquiries_facility"),
		},
	}
	_, err = inquiries.Indexes().CreateMany(ctx, inqIndexes)
	if err != nil {
		log.Printf("[MONGODB] Error creating facility_inquiries indexes: %v", err)
		return err
	}

	log.Println("[MONGODB] facilities indexes created")
	return nil
}

func ensureScheduleIndexes(ctx context.Context, db *mongo.Database) error {
	rules := db.Collection("schedule_rules")
	ruleIndexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "space_id", Value: 1}},
			Options: options.Index().SetName("idx_schedule_rules_space"),
		},
		{
			Keys:    bson.D{{Key: "sport_center_id", Value: 1}},
			Options: options.Index().SetName("idx_schedule_rules_center"),
		},
	}
	if _, err := rules.Indexes().CreateMany(ctx, ruleIndexes); err != nil {
		log.Printf("[MONGODB] Error creating schedule_rules indexes: %v", err)
	}

	exceptions := db.Collection("schedule_exceptions")
	exceptionIndexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "space_id", Value: 1}, {Key: "date", Value: 1}},
			Options: options.Index().SetName("idx_schedule_exceptions_space_date"),
		},
		{
			Keys:    bson.D{{Key: "sport_center_id", Value: 1}},
			Options: options.Index().SetName("idx_schedule_exceptions_center"),
		},
	}
	if _, err := exceptions.Indexes().CreateMany(ctx, exceptionIndexes); err != nil {
		log.Printf("[MONGODB] Error creating schedule_exceptions indexes: %v", err)
	}

	blocks := db.Collection("blocks")
	blockIndexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "space_id", Value: 1}, {Key: "date", Value: 1}},
			Options: options.Index().SetName("idx_blocks_space_date"),
		},
		{
			Keys:    bson.D{{Key: "sport_center_id", Value: 1}},
			Options: options.Index().SetName("idx_blocks_center"),
		},
	}
	if _, err := blocks.Indexes().CreateMany(ctx, blockIndexes); err != nil {
		log.Printf("[MONGODB] Error creating blocks indexes: %v", err)
	}

	log.Println("[MONGODB] schedule indexes created")
	return nil
}
