package mongo

import (
	"context"
	"time"

	"github.com/hamp/booking-sport/internal/app"
	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// OccupancyReconcileStore implements app.OccupancyReconcileStore on bookings.
type OccupancyReconcileStore struct {
	collection *mongo.Collection
}

var _ app.OccupancyReconcileStore = (*OccupancyReconcileStore)(nil)

func NewOccupancyReconcileStore(db *mongo.Database) *OccupancyReconcileStore {
	return &OccupancyReconcileStore{collection: db.Collection("bookings")}
}

func (s *OccupancyReconcileStore) FindLinkedBookings(ctx context.Context, limit int64) ([]app.LinkedBooking, error) {
	opts := options.Find().SetProjection(bson.M{
		"status": 1, "ends_at": 1, "occupancy_id": 1, "occupancy_token": 1,
	})
	if limit > 0 {
		opts.SetLimit(limit)
	}
	cursor, err := s.collection.Find(ctx, bson.M{"occupancy_id": bson.M{"$exists": true, "$ne": ""}}, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var out []app.LinkedBooking
	for cursor.Next(ctx) {
		var doc struct {
			ID          primitive.ObjectID   `bson:"_id"`
			Status      domain.BookingStatus `bson:"status"`
			EndsAt      time.Time            `bson:"ends_at"`
			OccupancyID string               `bson:"occupancy_id"`
			Token       string               `bson:"occupancy_token"`
		}
		if err := cursor.Decode(&doc); err != nil {
			return nil, err
		}
		out = append(out, app.LinkedBooking{
			ID: doc.ID, Status: doc.Status, EndsAt: doc.EndsAt,
			OccupancyID: doc.OccupancyID, Token: doc.Token,
		})
	}
	return out, cursor.Err()
}

func (s *OccupancyReconcileStore) ClearOccupancyLink(ctx context.Context, bookingID primitive.ObjectID) error {
	_, err := s.collection.UpdateOne(ctx, bson.M{"_id": bookingID},
		bson.M{"$unset": bson.M{"occupancy_id": "", "occupancy_token": ""}})
	return err
}
