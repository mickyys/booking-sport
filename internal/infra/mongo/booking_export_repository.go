package mongo

import (
	"context"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// FindForExport devuelve las reservas de los centros cuyo día cae entre start y
// end (ambos inclusive, fechas civiles de Santiago). Si statuses está vacío se
// omiten las reservas expiradas.
func (r *BookingRepository) FindForExport(ctx context.Context, centerIDs []primitive.ObjectID, start, end time.Time, statuses []domain.BookingStatus, limit int64) ([]domain.Booking, error) {
	filter := bson.M{
		"sport_center_id": bson.M{"$in": centerIDs},
		"date":            buildDateRangeFilter(start, end),
	}
	if len(statuses) > 0 {
		filter["status"] = bson.M{"$in": statuses}
	} else {
		filter["status"] = bson.M{"$ne": domain.BookingStatusExpired}
	}

	cursor, err := r.collection.Find(ctx, filter, options.Find().
		SetSort(bson.D{{Key: "date", Value: 1}, {Key: "hour", Value: 1}, {Key: "minutes", Value: 1}}).
		SetLimit(limit))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var bookings []domain.Booking
	if err := cursor.All(ctx, &bookings); err != nil {
		return nil, err
	}
	return bookings, nil
}
