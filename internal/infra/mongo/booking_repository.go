// ...existing code...

package mongo

import (
	"context"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

type BookingRepository struct {
	collection *mongo.Collection
}

func NewBookingRepository(db *mongo.Database) *BookingRepository {
	return &BookingRepository{
		collection: db.Collection("bookings"),
	}
}

// FindByUserIDAndStatusPaged retorna reservas de un usuario filtradas por estado, paginadas
func (r *BookingRepository) FindByUserIDAndStatusPaged(ctx context.Context, userID string, status domain.BookingStatus, page, limit int) ([]domain.BookingSummary, int64, error) {
	skip := (page - 1) * limit
	filter := bson.M{"user_id": userID, "status": status}

	total, err := r.collection.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, err
	}

	pipeline := mongo.Pipeline{
		bson.D{{Key: "$match", Value: filter}},
		bson.D{{Key: "$sort", Value: bson.M{"created_at": -1}}},
		bson.D{{Key: "$skip", Value: int64(skip)}},
		bson.D{{Key: "$limit", Value: int64(limit)}},
		bson.D{{Key: "$lookup", Value: bson.M{
			"from":         "courts",
			"localField":   "court_id",
			"foreignField": "_id",
			"as":           "court_info",
		}}},
		bson.D{{Key: "$unwind", Value: bson.M{
			"path":                       "$court_info",
			"preserveNullAndEmptyArrays": true,
		}}},
		bson.D{{Key: "$lookup", Value: bson.M{
			"from":         "sport_centers",
			"localField":   "court_info.sport_center_id",
			"foreignField": "_id",
			"as":           "sport_center_info",
		}}},
		bson.D{{Key: "$unwind", Value: bson.M{
			"path":                       "$sport_center_info",
			"preserveNullAndEmptyArrays": true,
		}}},
		bson.D{{Key: "$addFields", Value: bson.M{
			"sport_center_name":  "$sport_center_info.name",
			"court_name":         "$court_info.name",
			"payment_method":     bson.M{"$ifNull": []interface{}{"$payment_method", "fintoc"}},
			"cancellation_hours": bson.M{"$ifNull": []interface{}{"$sport_center_info.cancellation_hours", 3}},
			"retention_percent":  bson.M{"$ifNull": []interface{}{"$sport_center_info.retention_percent", 10}},
		}}},
		bson.D{{Key: "$project", Value: bson.M{
			"id":                 "$_id",
			"sport_center_name":  1,
			"date":               1,
			"hour":               1,
			"court_name":         1,
			"status":             1,
			"price":              1,
			"final_price":        1,
			"payment_method":     1,
			"cancellation_hours": 1,
			"retention_percent":  1,
		}}},
	}

	cursor, err := r.collection.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)

	bookings := []domain.BookingSummary{}
	if err := cursor.All(ctx, &bookings); err != nil {
		return nil, 0, err
	}
	return bookings, total, nil
}

func (r *BookingRepository) Create(ctx context.Context, booking *domain.Booking) error {
	res, err := r.collection.InsertOne(ctx, booking)
	if err != nil {
		return err
	}
	booking.ID = res.InsertedID.(primitive.ObjectID)
	return nil
}

func (r *BookingRepository) Update(ctx context.Context, booking *domain.Booking) error {
	filter := bson.M{"_id": booking.ID}
	update := bson.M{"$set": booking}
	_, err := r.collection.UpdateOne(ctx, filter, update)
	return err
}

func (r *BookingRepository) FindByID(ctx context.Context, id primitive.ObjectID) (*domain.Booking, error) {
	var booking domain.Booking
	err := r.collection.FindOne(ctx, bson.M{"_id": id}).Decode(&booking)
	if err != nil {
		return nil, err
	}
	return &booking, nil
}

func (r *BookingRepository) FindByPreferenceID(ctx context.Context, preferenceID string) (*domain.Booking, error) {
	var booking domain.Booking
	err := r.collection.FindOne(ctx, bson.M{"preference_id": preferenceID}).Decode(&booking)
	if err != nil {
		return nil, err
	}
	return &booking, nil
}

func (r *BookingRepository) FindByFintocPaymentID(ctx context.Context, fintocPaymentID string) (*domain.Booking, error) {
	var booking domain.Booking
	err := r.collection.FindOne(ctx, bson.M{"fintoc_payment_id": fintocPaymentID}).Decode(&booking)
	if err != nil {
		return nil, err
	}
	return &booking, nil
}

func (r *BookingRepository) FindByFintocPaymentIntentID(ctx context.Context, paymentIntentID string) (*domain.Booking, error) {
	var booking domain.Booking
	err := r.collection.FindOne(ctx, bson.M{"fintoc_payment_intent_id": paymentIntentID}).Decode(&booking)
	if err != nil {
		return nil, err
	}
	return &booking, nil
}

func (r *BookingRepository) FindByBookingCode(ctx context.Context, code string) (*domain.Booking, error) {
	var booking domain.Booking
	err := r.collection.FindOne(ctx, bson.M{"booking_code": code}).Decode(&booking)
	if err != nil {
		return nil, err
	}
	return &booking, nil
}

func (r *BookingRepository) UpdateStatus(ctx context.Context, id primitive.ObjectID, status domain.BookingStatus) error {
	filter := bson.M{"_id": id}
	update := bson.M{"$set": bson.M{"status": status}}
	_, err := r.collection.UpdateOne(ctx, filter, update)
	return err
}

func (r *BookingRepository) UpdateFintocPaymentIntentID(ctx context.Context, id primitive.ObjectID, paymentIntentID string) error {
	filter := bson.M{"_id": id}
	update := bson.M{"$set": bson.M{"fintoc_payment_intent_id": paymentIntentID}}
	_, err := r.collection.UpdateOne(ctx, filter, update)
	return err
}

func (r *BookingRepository) FindByCourtAndDate(ctx context.Context, courtID primitive.ObjectID, date time.Time) ([]domain.Booking, error) {
	// Normalizar fecha al inicio del día
	startDate := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location())
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

	var bookings []domain.Booking
	if err := cursor.All(ctx, &bookings); err != nil {
		return nil, err
	}
	return bookings, nil
}

func (r *BookingRepository) FindByUserIDPaged(ctx context.Context, userID string, page, limit int, isOld bool) ([]domain.BookingSummary, int64, error) {
	skip := (page - 1) * limit
	filter := bson.M{"user_id": userID}

	if isOld {
		now := time.Now()
		// Normalize today for date comparison
		today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		currentHour := now.Hour()

		filter["$or"] = []bson.M{
			// Cases where the date is strictly before today
			{"date": bson.M{"$lt": today}},
			// Cases where the date is today but the hour has already passed
			{
				"$and": []bson.M{
					{"date": bson.M{"$eq": today}},
					{"hour": bson.M{"$lt": currentHour}},
				},
			},
		}
	} else {
		now := time.Now()
		// Normalize today for date comparison
		today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		currentHour := now.Hour()

		filter["$or"] = []bson.M{
			// Cases where the date is strictly in the future
			{"date": bson.M{"$gt": today}},
			// Cases where the date is today but the hour is current or in the future
			{
				"$and": []bson.M{
					{"date": bson.M{"$eq": today}},
					{"hour": bson.M{"$gte": currentHour}},
				},
			},
		}
	}

	total, err := r.collection.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, err
	}

	pipeline := mongo.Pipeline{
		{{"$match", filter}},
		{{"$sort", bson.M{"created_at": -1}}},
		{{"$skip", int64(skip)}},
		{{"$limit", int64(limit)}},
		{{"$lookup", bson.M{
			"from":         "courts",
			"localField":   "court_id",
			"foreignField": "_id",
			"as":           "court_info",
		}}},
		{{"$unwind", bson.M{
			"path":                       "$court_info",
			"preserveNullAndEmptyArrays": true,
		}}},
		{{"$lookup", bson.M{
			"from":         "sport_centers",
			"localField":   "court_info.sport_center_id",
			"foreignField": "_id",
			"as":           "sport_center_info",
		}}},
		{{"$unwind", bson.M{
			"path":                       "$sport_center_info",
			"preserveNullAndEmptyArrays": true,
		}}},
		{{"$addFields", bson.M{
			"sport_center_name":  "$sport_center_info.name",
			"court_name":         "$court_info.name",
			"payment_method":     bson.M{"$ifNull": []interface{}{"$payment_method", "fintoc"}},
			"cancellation_hours": bson.M{"$ifNull": []interface{}{"$sport_center_info.cancellation_hours", 3}},
			"retention_percent":  bson.M{"$ifNull": []interface{}{"$sport_center_info.retention_percent", 10}},
		}}},
		{{"$project", bson.M{
			"id":                 "$_id",
			"_id":                1,
			"sport_center_name":  1,
			"date":               1,
			"hour":               1,
			"court_name":         1,
			"status":             1,
			"price":              1,
			"final_price":        1,
			"payment_method":     1,
			"cancellation_hours": 1,
			"retention_percent":  1,
		}}},
	}

	cursor, err := r.collection.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)

	bookings := []domain.BookingSummary{}
	if err := cursor.All(ctx, &bookings); err != nil {
		return nil, 0, err
	}
	return bookings, total, nil
}

func (r *BookingRepository) FindByUserID(ctx context.Context, userID string) ([]domain.Booking, error) {
	filter := bson.M{"user_id": userID}

	pipeline := mongo.Pipeline{
		{{"$match", filter}},
		{{"$sort", bson.M{"created_at": -1}}},
		{{"$lookup", bson.M{
			"from":         "courts",
			"localField":   "court_id",
			"foreignField": "_id",
			"as":           "court_info",
		}}},
		{{"$unwind", bson.M{
			"path":                       "$court_info",
			"preserveNullAndEmptyArrays": true,
		}}},
		{{"$lookup", bson.M{
			"from":         "sport_centers",
			"localField":   "court_info.sport_center_id",
			"foreignField": "_id",
			"as":           "sport_center_info",
		}}},
		{{"$unwind", bson.M{
			"path":                       "$sport_center_info",
			"preserveNullAndEmptyArrays": true,
		}}},
		{{"$addFields", bson.M{
			"sport_center_name": "$sport_center_info.name",
			"court_name":        "$court_info.name",
		}}},
		{{"$project", bson.M{
			"id":                "$_id",
			"_id":               1,
			"sport_center_name": 1,
			"date":              1,
			"hour":              1,
			"court_name":        1,
			"status":            1,
			"price":             1,
			"final_price":       1,
		}}},
	}

	cursor, err := r.collection.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	bookings := []domain.Booking{}
	if err := cursor.All(ctx, &bookings); err != nil {
		return nil, err
	}
	return bookings, nil
}

func (r *BookingRepository) AddRefund(ctx context.Context, paymentIntentID string, refund domain.Refund) error {
	filter := bson.M{"fintoc_payment_intent_id": paymentIntentID}

	// Agregamos el refund al array y restamos el monto del final_price
	update := bson.M{
		"$push": bson.M{"refunds": refund},
		"$inc":  bson.M{"final_price": -float64(refund.Amount)},
	}

	_, err := r.collection.UpdateOne(ctx, filter, update)
	return err
}

func (r *BookingRepository) CountConfirmedByUserID(ctx context.Context, userID string) (int64, error) {
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	currentHour := now.Hour()

	filter := bson.M{
		"user_id": userID,
		"status":  domain.BookingStatusConfirmed,
		"$or": []bson.M{
			// Reservas de días anteriores
			{"date": bson.M{"$lt": today}},
			// Reservas de hoy cuya hora ya pasó
			{
				"$and": []bson.M{
					{"date": bson.M{"$eq": today}},
					{"hour": bson.M{"$lt": currentHour}},
				},
			},
		},
	}
	return r.collection.CountDocuments(ctx, filter)
}
