// ...existing code...

package mongo

import (
	"context"
	"strings"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

type BookingRepository struct {
	collection *mongo.Collection
	db         *mongo.Database
	ruleColl   *mongo.Collection
}

func NewBookingRepository(db *mongo.Database) *BookingRepository {
	return &BookingRepository{
		collection: db.Collection("bookings"),
		db:         db,
		ruleColl:   db.Collection("recurring_rules"),
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

func (r *BookingRepository) DeleteBySeriesID(ctx context.Context, seriesID string) error {
	filter := bson.M{"series_id": seriesID}
	_, err := r.collection.DeleteMany(ctx, filter)
	return err
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

func (r *BookingRepository) UpdateCancellation(ctx context.Context, id primitive.ObjectID, status domain.BookingStatus, cancelledBy string, reason string) error {
	filter := bson.M{"_id": id}
	update := bson.M{"$set": bson.M{
		"status":              status,
		"cancelled_by":        cancelledBy,
		"cancellation_reason": reason,
		"updated_at":          time.Now(),
	}}
	_, err := r.collection.UpdateOne(ctx, filter, update)
	return err
}

func (r *BookingRepository) UpdateFintocPaymentIntentID(ctx context.Context, id primitive.ObjectID, paymentIntentID string) error {
	filter := bson.M{"_id": id}
	update := bson.M{"$set": bson.M{"fintoc_payment_intent_id": paymentIntentID}}
	_, err := r.collection.UpdateOne(ctx, filter, update)
	return err
}

func (r *BookingRepository) FindByMPPreferenceID(ctx context.Context, preferenceID string) (*domain.Booking, error) {
	var booking domain.Booking
	err := r.collection.FindOne(ctx, bson.M{"mp_preference_id": preferenceID}).Decode(&booking)
	if err != nil {
		return nil, err
	}
	return &booking, nil
}

func (r *BookingRepository) FindByMPPaymentID(ctx context.Context, paymentID string) (*domain.Booking, error) {
	var booking domain.Booking
	err := r.collection.FindOne(ctx, bson.M{"mp_payment_id": paymentID}).Decode(&booking)
	if err != nil {
		return nil, err
	}
	return &booking, nil
}

func (r *BookingRepository) UpdateMPPaymentID(ctx context.Context, id primitive.ObjectID, mpPaymentID string) error {
	filter := bson.M{"_id": id}
	update := bson.M{"$set": bson.M{"mp_payment_id": mpPaymentID}}
	_, err := r.collection.UpdateOne(ctx, filter, update)
	return err
}

func (r *BookingRepository) FindByCourtAndDate(ctx context.Context, courtID primitive.ObjectID, date time.Time) ([]domain.Booking, error) {
	// Normalizar fecha al inicio del día en zona horaria de Chile
	loc, _ := time.LoadLocation("America/Santiago")
	startDate := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, loc)
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

func (r *BookingRepository) FindBySportCenterAndDate(ctx context.Context, centerID primitive.ObjectID, date time.Time) ([]domain.Booking, error) {
	// Normalizar fecha al inicio y fin del día en zona horaria de Chile
	loc, _ := time.LoadLocation("America/Santiago")
	startDate := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, loc)
	endDate := startDate.AddDate(0, 0, 1)

	cursor, err := r.collection.Find(ctx, bson.M{
		"sport_center_id": centerID,
		"date": bson.M{
			"$gte": startDate,
			"$lt":  endDate,
		},
		"status": domain.BookingStatusConfirmed,
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

	loc, _ := time.LoadLocation("America/Santiago")
	now := time.Now().In(loc)
	// Normalize today for date comparison
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	currentHour := now.Hour()

	if isOld {
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
		{{Key: "$match", Value: filter}},
		{{Key: "$sort", Value: bson.M{"created_at": -1}}},
		{{Key: "$skip", Value: int64(skip)}},
		{{Key: "$limit", Value: int64(limit)}},
		{{Key: "$lookup", Value: bson.M{
			"from":         "courts",
			"localField":   "court_id",
			"foreignField": "_id",
			"as":           "court_info",
		}}},
		{{Key: "$unwind", Value: bson.M{
			"path":                       "$court_info",
			"preserveNullAndEmptyArrays": true,
		}}},
		{{Key: "$lookup", Value: bson.M{
			"from":         "sport_centers",
			"localField":   "court_info.sport_center_id",
			"foreignField": "_id",
			"as":           "sport_center_info",
		}}},
		{{Key: "$unwind", Value: bson.M{
			"path":                       "$sport_center_info",
			"preserveNullAndEmptyArrays": true,
		}}},
		{{Key: "$addFields", Value: bson.M{
			"sport_center_name":  "$sport_center_info.name",
			"court_name":         "$court_info.name",
			"payment_method":     bson.M{"$ifNull": []interface{}{"$payment_method", "fintoc"}},
			"cancellation_hours": bson.M{"$ifNull": []interface{}{"$sport_center_info.cancellation_hours", 3}},
			"retention_percent":  bson.M{"$ifNull": []interface{}{"$sport_center_info.retention_percent", 10}},
		}}},
		{{Key: "$project", Value: bson.M{
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
		{{Key: "$match", Value: filter}},
		{{Key: "$sort", Value: bson.M{"created_at": -1}}},
		{{Key: "$lookup", Value: bson.M{
			"from":         "courts",
			"localField":   "court_id",
			"foreignField": "_id",
			"as":           "court_info",
		}}},
		{{Key: "$unwind", Value: bson.M{
			"path":                       "$court_info",
			"preserveNullAndEmptyArrays": true,
		}}},
		{{Key: "$lookup", Value: bson.M{
			"from":         "sport_centers",
			"localField":   "court_info.sport_center_id",
			"foreignField": "_id",
			"as":           "sport_center_info",
		}}},
		{{Key: "$unwind", Value: bson.M{
			"path":                       "$sport_center_info",
			"preserveNullAndEmptyArrays": true,
		}}},
		{{Key: "$addFields", Value: bson.M{
			"sport_center_name": "$sport_center_info.name",
			"court_name":        "$court_info.name",
		}}},
		{{Key: "$project", Value: bson.M{
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

func (r *BookingRepository) AddRefundByBookingID(ctx context.Context, bookingID primitive.ObjectID, refund domain.Refund) error {
	filter := bson.M{"_id": bookingID}

	update := bson.M{
		"$push": bson.M{"refunds": refund},
		"$inc":  bson.M{"final_price": -float64(refund.Amount)},
	}

	_, err := r.collection.UpdateOne(ctx, filter, update)
	return err
}

func (r *BookingRepository) CountConfirmedByUserID(ctx context.Context, userID string) (int64, error) {
	loc, _ := time.LoadLocation("America/Santiago")
	now := time.Now().In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
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

func (r *BookingRepository) Delete(ctx context.Context, id primitive.ObjectID) error {
	_, err := r.collection.DeleteOne(ctx, bson.M{"_id": id})
	return err
}
func (r *BookingRepository) GetRecurringSeries(ctx context.Context, centerIDs []primitive.ObjectID) ([]domain.RecurringSeries, error) {
	// Si no tiene centros, devolvemos lista vacía
	if len(centerIDs) == 0 {
		return []domain.RecurringSeries{}, nil
	}

	match := bson.M{
		"series_id":       bson.M{"$ne": ""},
		"sport_center_id": bson.M{"$in": centerIDs},
	}

	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: match}},
		{{Key: "$sort", Value: bson.M{"date": 1}}},
		{{Key: "$lookup", Value: bson.M{
			"from":         "courts",
			"localField":   "court_id",
			"foreignField": "_id",
			"as":           "court_info",
		}}},
		{{Key: "$unwind", Value: bson.M{
			"path":                       "$court_info",
			"preserveNullAndEmptyArrays": true,
		}}},
		{{Key: "$group", Value: bson.M{
			"_id":            "$series_id",
			"customer_name":  bson.M{"$first": "$customer_name"},
			"customer_phone": bson.M{"$first": "$customer_phone"},
			"court_name":     bson.M{"$first": "$court_info.name"},
			"hour":           bson.M{"$first": "$hour"},
			"start_date":     bson.M{"$min": "$date"},
			"end_date":       bson.M{"$max": "$date"},
			"bookings_count": bson.M{"$sum": 1},
		}}},
	}

	cursor, err := r.collection.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var results []domain.RecurringSeries
	if err := cursor.All(ctx, &results); err != nil {
		return nil, err
	}

	return results, nil
}

func (r *BookingRepository) GetDashboardData(ctx context.Context, sportCenterIDs []primitive.ObjectID, page, limit int, dateStr, name, code, status string) (*domain.AdminDashboardData, error) {
	loc, _ := time.LoadLocation("America/Santiago")
	now := time.Now().In(loc)
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	todayEnd := todayStart.Add(24 * time.Hour)

	// Parse date range for global filters
	var dateFilter bson.M
	if dateStr != "" {
		if strings.Contains(dateStr, "|") {
			parts := strings.SplitN(dateStr, "|", 2)
			startT, err1 := time.Parse("2006-01-02", parts[0])
			endT, err2 := time.Parse("2006-01-02", parts[1])
			if err1 == nil && err2 == nil {
				start := time.Date(startT.Year(), startT.Month(), startT.Day(), 0, 0, 0, 0, loc)
				end := time.Date(endT.Year(), endT.Month(), endT.Day(), 0, 0, 0, 0, loc).Add(24 * time.Hour)
				dateFilter = bson.M{"$gte": start, "$lt": end}
			}
		} else {
			t, err := time.Parse("2006-01-02", dateStr)
			if err == nil {
				start := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
				end := start.Add(24 * time.Hour)
				dateFilter = bson.M{"$gte": start, "$lt": end}
			}
		}
	}

	// Get stats

	// 1. Today's Bookings Count
	todayFilter := bson.M{
		"sport_center_id": bson.M{"$in": sportCenterIDs},
		"date":            bson.M{"$gte": todayStart, "$lt": todayEnd},
		"status":          domain.BookingStatusConfirmed,}
	rulesToday, _ := r.getActiveRulesForDate(ctx, sportCenterIDs, todayStart)
	todayCount, _ := r.collection.CountDocuments(ctx, todayFilter)
	todayCount += int64(len(rulesToday))

	// Helper function to extract revenue from aggregation result
	getRevenueValues := func(result []bson.M, totalKey, onlineKey, venueKey string) (float64, float64, float64) {
		total := 0.0
		online := 0.0
		venue := 0.0
		if len(result) > 0 {
			to_float := func(v interface{}) float64 {
				switch val := v.(type) {
				case float64:
					return val
				case int32:
					return float64(val)
				case int64:
					return float64(val)
				default:
					return 0.0
				}
			}
			total = to_float(result[0][totalKey])
			online = to_float(result[0][onlineKey])
			venue = to_float(result[0][venueKey])
		}
		return total, online, venue
	}

	// 2. Today's Revenue
	pipelineTodayRevenue := mongo.Pipeline{
		{{Key: "$match", Value: todayFilter}},
		{{Key: "$group", Value: bson.M{
			"_id":           nil,
			"today_revenue": bson.M{"$sum": "$price"},
			"online_revenue": bson.M{"$sum": bson.M{
				"$cond": []interface{}{
					bson.M{"$eq": []interface{}{"$payment_method", "mercadopago"}},
					"$price",
					0,
				},
			}},
			"venue_revenue": bson.M{"$sum": bson.M{
				"$cond": []interface{}{
					bson.M{"$in": []interface{}{"$payment_method", []string{"venue", "internal"}}},
					"$price",
					0,
				},
			}},
		}}},
	}
	cursorRevenue, err := r.collection.Aggregate(ctx, pipelineTodayRevenue)
	if err != nil {
		return nil, err
	}
	var todayRevenueResult []bson.M
	cursorRevenue.All(ctx, &todayRevenueResult)
	todayRevenue, todayOnlineRevenue, todayVenueRevenue := getRevenueValues(todayRevenueResult, "today_revenue", "online_revenue", "venue_revenue")
	for _, rule := range rulesToday {
		todayRevenue += rule.Price
		todayVenueRevenue += rule.Price
	}

	// 3. Total Revenue (Confirmed)
	totalRevenueMatch := bson.M{
		"sport_center_id": bson.M{"$in": sportCenterIDs},
		"status":          domain.BookingStatusConfirmed,
	}
	if dateFilter != nil {
		totalRevenueMatch["date"] = dateFilter
	}

	pipelineTotalRevenue := mongo.Pipeline{
		{{Key: "$match", Value: totalRevenueMatch}},
		{{Key: "$group", Value: bson.M{
			"_id":           nil,
			"total_revenue": bson.M{"$sum": "$price"},
			"online_revenue": bson.M{"$sum": bson.M{
				"$cond": []interface{}{
					bson.M{"$eq": []interface{}{"$payment_method", "mercadopago"}},
					"$price",
					0,
				},
			}},
			"venue_revenue": bson.M{"$sum": bson.M{
				"$cond": []interface{}{
					bson.M{"$in": []interface{}{"$payment_method", []string{"venue", "internal"}}},
					"$price",
					0,
				},
			}},
		}}},
	}
	cursorTotal, err := r.collection.Aggregate(ctx, pipelineTotalRevenue)
	if err != nil {
		return nil, err
	}
	var totalRevenueResult []bson.M
	cursorTotal.All(ctx, &totalRevenueResult)
	totalRevenue, totalOnlineRevenue, totalVenueRevenue := getRevenueValues(totalRevenueResult, "total_revenue", "online_revenue", "venue_revenue")

	// 4. Cancelled Count
	cancelledFilter := bson.M{
		"sport_center_id": bson.M{"$in": sportCenterIDs},
		"status":          domain.BookingStatusCancelled,
	}
	if dateFilter != nil {
		cancelledFilter["date"] = dateFilter
	}
	cancelledCount, _ := r.collection.CountDocuments(ctx, cancelledFilter)

	// 5. Recent Bookings with filters and pagination
	recentMatch := bson.M{"sport_center_id": bson.M{"$in": sportCenterIDs}}
	if dateFilter != nil {
		recentMatch["date"] = dateFilter
	}
	if name != "" {
		recentMatch["$or"] = []bson.M{
			{"customer_name": bson.M{"$regex": name, "$options": "i"}},
			{"guest_details.name": bson.M{"$regex": name, "$options": "i"}},
		}
	}
	if code != "" {
		recentMatch["booking_code"] = bson.M{"$regex": code, "$options": "i"}
	}
	if status != "" {
		recentMatch["status"] = status
	}

	totalRecentCount, _ := r.collection.CountDocuments(ctx, recentMatch)

	pipelineRecent := mongo.Pipeline{
		{{Key: "$match", Value: recentMatch}},
		{{Key: "$sort", Value: bson.M{"created_at": -1}}},
		{{Key: "$skip", Value: int64((page - 1) * limit)}},
		{{Key: "$limit", Value: int64(limit)}},
		{{Key: "$lookup", Value: bson.M{
			"from":         "courts",
			"localField":   "court_id",
			"foreignField": "_id",
			"as":           "court_info",
		}}},
		{{Key: "$unwind", Value: bson.M{
			"path":                       "$court_info",
			"preserveNullAndEmptyArrays": true,
		}}},
		{{Key: "$lookup", Value: bson.M{
			"from":         "sport_centers",
			"localField":   "court_info.sport_center_id",
			"foreignField": "_id",
			"as":           "sport_center_info",
		}}},
		{{Key: "$unwind", Value: bson.M{
			"path":                       "$sport_center_info",
			"preserveNullAndEmptyArrays": true,
		}}},
		{{Key: "$addFields", Value: bson.M{
			"sport_center_name":  "$sport_center_info.name",
			"court_name":         "$court_info.name",
			"user_name":          bson.M{"$ifNull": []interface{}{"$customer_name", "$guest_details.name", "Usuario"}},
			"customer_name":      bson.M{"$ifNull": []interface{}{"$customer_name", "$guest_details.name", ""}},
			"customer_phone":     bson.M{"$ifNull": []interface{}{"$customer_phone", "$guest_details.phone", ""}},
			"customer_email":     bson.M{"$ifNull": []interface{}{"$customer_email", "$guest_details.email", ""}},
			"is_guest":           bson.M{"$cond": []interface{}{bson.M{"$ne": []interface{}{"$guest_details", nil}}, true, false}},
			"payment_method":     bson.M{"$ifNull": []interface{}{"$payment_method", "fintoc"}},
			"cancelled_by":       bson.M{"$ifNull": []interface{}{"$cancelled_by", ""}},
			"cancellation_hours": bson.M{"$ifNull": []interface{}{"$sport_center_info.cancellation_hours", 3}},
			"retention_percent":  bson.M{"$ifNull": []interface{}{"$sport_center_info.retention_percent", 10}},
		}}},
		{{Key: "$project", Value: bson.M{
			"id":                 "$_id",
			"sport_center_name":  1,
			"customer_name":      1,
			"customer_phone":     1,
			"customer_email":     1,
			"date":               1,
			"hour":               1,
			"booking_code":       1,
			"court_name":         1,
			"status":             1,
			"price":              1,
			"user_name":          1,
			"is_guest":           1,
			"payment_method":     1,
			"cancelled_by":       1,
			"cancellation_hours": 1,
			"retention_percent":  1,
		}}},
	}

	cursorRecent, err := r.collection.Aggregate(ctx, pipelineRecent)
	if err != nil {
		return nil, err
	}
	defer cursorRecent.Close(ctx)

	var recentBookings []domain.BookingSummary
	if err := cursorRecent.All(ctx, &recentBookings); err != nil {
		return nil, err
	}

	totalPages := int((totalRecentCount + int64(limit) - 1) / int64(limit))

	// Inject recurring rules into recent bookings if we are looking at a specific date or today
	if dateStr != "" || (dateStr == "" && page == 1) {
		searchDate := todayStart
		if dateStr != "" && !strings.Contains(dateStr, "|") {
			t, err := time.Parse("2006-01-02", dateStr)
			if err == nil {
				searchDate = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
			}
		}

		rulesForDate, _ := r.getActiveRulesForDate(ctx, sportCenterIDs, searchDate)
		for _, rule := range rulesForDate {
			if name != "" && !strings.Contains(strings.ToLower(rule.CustomerName), strings.ToLower(name)) {
				continue
			}
			if status != "" && status != string(domain.BookingStatusConfirmed) {
				continue
			}

			recentBookings = append([]domain.BookingSummary{{
				ID:              rule.ID,
				CustomerName:    rule.CustomerName,
				CustomerPhone:   rule.CustomerPhone,
				Date:            searchDate,
				Hour:            rule.Hour,
				CourtName:       rule.CourtName,
				Status:          domain.BookingStatusConfirmed,
				Price:           rule.Price,
				FinalPrice:      rule.Price,
				PaymentMethod:   "internal",
			}}, recentBookings...)
			totalRecentCount++
		}
		// Recalculate totalPages if we added rules
		totalPages = int((totalRecentCount + int64(limit) - 1) / int64(limit))
	}

	return &domain.AdminDashboardData{
		TodayBookingsCount: int(todayCount),
		TodayRevenue:       todayRevenue,
		TodayOnlineRevenue: todayOnlineRevenue,
		TodayVenueRevenue:  todayVenueRevenue,
		TotalRevenue:       totalRevenue,
		TotalOnlineRevenue: totalOnlineRevenue,
		TotalVenueRevenue:  totalVenueRevenue,
		CancelledCount:     int(cancelledCount),
		RecentBookings:     recentBookings,
		TotalRecentCount:   totalRecentCount,
		Page:               page,
		Limit:              limit,
		TotalPages:         totalPages,
	}, nil
}
func (r *BookingRepository) getActiveRulesForDate(ctx context.Context, centerIDs []primitive.ObjectID, date time.Time) ([]domain.RecurringRule, error) {
	dayOfWeek := int(date.Weekday())
	filter := bson.M{
		"sport_center_id": bson.M{"$in": centerIDs},
		"day_of_week":      dayOfWeek,
		"start_date":      bson.M{"$lte": date},
		"$or": []bson.M{
			{"end_date": nil},
			{"end_date": bson.M{"$gte": date}},
		},
	}
	cursor, err := r.ruleColl.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var rules []domain.RecurringRule
	cursor.All(ctx, &rules)
	return rules, nil
}
