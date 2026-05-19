package mongo

import (
	"context"
	"fmt"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type SlotHoldRepository struct {
	collection *mongo.Collection
}

func NewSlotHoldRepository(db *mongo.Database) *SlotHoldRepository {
	return &SlotHoldRepository{
		collection: db.Collection("slot_holds"),
	}
}

func (r *SlotHoldRepository) Insert(ctx context.Context, hold *domain.SlotHold) error {
	hold.ID = primitive.NewObjectID()
	_, err := r.collection.InsertOne(ctx, hold)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return fmt.Errorf("%w: slot already held", ErrDuplicateHold)
		}
		return err
	}
	return nil
}

func (r *SlotHoldRepository) FindBySlot(ctx context.Context, courtID primitive.ObjectID, date time.Time, hour int) (*domain.SlotHold, error) {
	loc, _ := time.LoadLocation("America/Santiago")
	dateCL := date.In(loc)
	startDate := time.Date(dateCL.Year(), dateCL.Month(), dateCL.Day(), 0, 0, 0, 0, loc)
	endDate := startDate.Add(24 * time.Hour)

	var hold domain.SlotHold
	err := r.collection.FindOne(ctx, bson.M{
		"court_id": courtID,
		"date": bson.M{
			"$gte": startDate,
			"$lt":  endDate,
		},
		"hour": hour,
	}).Decode(&hold)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, err
	}
	return &hold, nil
}

func (r *SlotHoldRepository) FindByBookingID(ctx context.Context, bookingID primitive.ObjectID) (*domain.SlotHold, error) {
	var hold domain.SlotHold
	err := r.collection.FindOne(ctx, bson.M{"booking_id": bookingID}).Decode(&hold)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, err
	}
	return &hold, nil
}

func (r *SlotHoldRepository) FindActiveByCourtAndDate(ctx context.Context, courtID primitive.ObjectID, date time.Time) ([]domain.SlotHold, error) {
	loc, _ := time.LoadLocation("America/Santiago")
	dateCL := date.In(loc)
	startDate := time.Date(dateCL.Year(), dateCL.Month(), dateCL.Day(), 0, 0, 0, 0, loc)
	endDate := startDate.Add(24 * time.Hour)

	cursor, err := r.collection.Find(ctx, bson.M{
		"court_id": courtID,
		"date": bson.M{
			"$gte": startDate,
			"$lt":  endDate,
		},
	})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var holds []domain.SlotHold
	if err := cursor.All(ctx, &holds); err != nil {
		return nil, err
	}
	return holds, nil
}

func (r *SlotHoldRepository) RenewExpiration(ctx context.Context, holdID primitive.ObjectID, newExpiresAt time.Time) error {
	_, err := r.collection.UpdateOne(ctx,
		bson.M{"_id": holdID},
		bson.M{"$set": bson.M{"expires_at": newExpiresAt}},
	)
	return err
}

func (r *SlotHoldRepository) DeleteIfExpired(ctx context.Context, holdID primitive.ObjectID, expectedExpiresAt time.Time) (bool, error) {
	result, err := r.collection.DeleteOne(ctx, bson.M{
		"_id":        holdID,
		"expires_at": expectedExpiresAt,
	})
	if err != nil {
		return false, err
	}
	return result.DeletedCount > 0, nil
}

func (r *SlotHoldRepository) Delete(ctx context.Context, holdID primitive.ObjectID) error {
	_, err := r.collection.DeleteOne(ctx, bson.M{"_id": holdID})
	return err
}

func (r *SlotHoldRepository) TryClaimSlot(ctx context.Context, hold *domain.SlotHold) (*domain.SlotHold, error) {
	hold.ID = primitive.NewObjectID()
	_, err := r.collection.InsertOne(ctx, hold)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return nil, ErrDuplicateHold
		}
		return nil, err
	}

	var inserted domain.SlotHold
	err = r.collection.FindOne(ctx, bson.M{"_id": hold.ID}).Decode(&inserted)
	if err != nil {
		return nil, err
	}
	return &inserted, nil
}

func (r *SlotHoldRepository) FindOneAndDeleteIfExpired(ctx context.Context, courtID primitive.ObjectID, date time.Time, hour int) (*domain.SlotHold, error) {
	loc, _ := time.LoadLocation("America/Santiago")
	dateCL := date.In(loc)
	startDate := time.Date(dateCL.Year(), dateCL.Month(), dateCL.Day(), 0, 0, 0, 0, loc)
	endDate := startDate.Add(24 * time.Hour)

	now := time.Now()
	var hold domain.SlotHold
	err := r.collection.FindOneAndDelete(ctx, bson.M{
		"court_id": courtID,
		"date": bson.M{
			"$gte": startDate,
			"$lt":  endDate,
		},
		"hour":       hour,
		"expires_at": bson.M{"$lt": now},
	}, options.FindOneAndDelete()).Decode(&hold)

	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, err
	}
	return &hold, nil
}

var ErrDuplicateHold = fmt.Errorf("duplicate hold")
