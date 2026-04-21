package mongo

import (
	"context"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

type RecurringReservationRepository struct {
	collection *mongo.Collection
	db         *mongo.Database
}

func NewRecurringReservationRepository(db *mongo.Database) *RecurringReservationRepository {
	return &RecurringReservationRepository{
		collection: db.Collection("recurring_reservations"),
		db:         db,
	}
}

func (r *RecurringReservationRepository) Create(ctx context.Context, reservation *domain.RecurringReservation) error {
	res, err := r.collection.InsertOne(ctx, reservation)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return err
		}
		return err
	}
	reservation.ID = res.InsertedID.(primitive.ObjectID)
	return nil
}

func (r *RecurringReservationRepository) FindByID(ctx context.Context, id primitive.ObjectID) (*domain.RecurringReservation, error) {
	var reservation domain.RecurringReservation
	err := r.collection.FindOne(ctx, bson.M{"_id": id}).Decode(&reservation)
	if err != nil {
		return nil, err
	}
	return &reservation, nil
}

func (r *RecurringReservationRepository) FindByCourtHourAndDay(ctx context.Context, courtID primitive.ObjectID, hour int, dayOfWeek int) (*domain.RecurringReservation, error) {
	var reservation domain.RecurringReservation
	err := r.collection.FindOne(ctx, bson.M{
		"court_id":    courtID,
		"hour":        hour,
		"day_of_week": dayOfWeek,
		"status":      domain.RecurringReservationStatusActive,
	}).Decode(&reservation)
	if err != nil {
		return nil, err
	}
	return &reservation, nil
}

func (r *RecurringReservationRepository) FindByCourtAndHour(ctx context.Context, courtID primitive.ObjectID, hour int) (*domain.RecurringReservation, error) {
	var reservation domain.RecurringReservation
	err := r.collection.FindOne(ctx, bson.M{
		"court_id": courtID,
		"hour":     hour,
		"status":   domain.RecurringReservationStatusActive,
	}).Decode(&reservation)
	if err != nil {
		return nil, err
	}
	return &reservation, nil
}

func (r *RecurringReservationRepository) FindActiveByCourtAndHour(ctx context.Context, courtID primitive.ObjectID, hour int) (*domain.RecurringReservation, error) {
	return r.FindByCourtAndHour(ctx, courtID, hour)
}

func (r *RecurringReservationRepository) FindByCenterID(ctx context.Context, centerID primitive.ObjectID) ([]domain.RecurringReservation, error) {
	cursor, err := r.collection.Find(ctx, bson.M{
		"sport_center_id": centerID,
		"status":          domain.RecurringReservationStatusActive,
	})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var reservations []domain.RecurringReservation
	if err := cursor.All(ctx, &reservations); err != nil {
		return nil, err
	}
	return reservations, nil
}

func (r *RecurringReservationRepository) FindByCourtID(ctx context.Context, courtID primitive.ObjectID) ([]domain.RecurringReservation, error) {
	cursor, err := r.collection.Find(ctx, bson.M{
		"court_id": courtID,
		"status":   domain.RecurringReservationStatusActive,
	})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var reservations []domain.RecurringReservation
	if err := cursor.All(ctx, &reservations); err != nil {
		return nil, err
	}
	return reservations, nil
}

func (r *RecurringReservationRepository) FindByCenterIDAndDayOfWeek(ctx context.Context, centerID primitive.ObjectID, dayOfWeek int) ([]domain.RecurringReservation, error) {
	cursor, err := r.collection.Find(ctx, bson.M{
		"sport_center_id": centerID,
		"day_of_week":   dayOfWeek,
		"status":        domain.RecurringReservationStatusActive,
	})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var reservations []domain.RecurringReservation
	if err := cursor.All(ctx, &reservations); err != nil {
		return nil, err
	}
	return reservations, nil
}

func (r *RecurringReservationRepository) Update(ctx context.Context, reservation *domain.RecurringReservation) error {
	filter := bson.M{"_id": reservation.ID}
	update := bson.M{"$set": reservation}
	_, err := r.collection.UpdateOne(ctx, filter, update)
	return err
}

func (r *RecurringReservationRepository) Cancel(ctx context.Context, id primitive.ObjectID, cancelledBy string, reason string) error {
	filter := bson.M{"_id": id}
	update := bson.M{"$set": bson.M{
		"status":        domain.RecurringReservationStatusCancelled,
		"cancelled_by":  cancelledBy,
		"cancel_reason": reason,
		"updated_at":    time.Now(),
	}}
	_, err := r.collection.UpdateOne(ctx, filter, update)
	return err
}

func (r *RecurringReservationRepository) Delete(ctx context.Context, id primitive.ObjectID) error {
	_, err := r.collection.DeleteOne(ctx, bson.M{"_id": id})
	return err
}
