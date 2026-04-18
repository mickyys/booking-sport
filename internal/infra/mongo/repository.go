package mongo

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	mongodb "go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func Connect(ctx context.Context, opts *options.ClientOptions) (*mongodb.Client, error) {
	client, err := mongodb.Connect(ctx, opts)
	if err != nil {
		return nil, err
	}
	err = client.Ping(ctx, nil)
	return client, err
}

type SportCenterRepository struct {
	db         *mongodb.Database
	collection *mongodb.Collection
}

func NewSportCenterRepository(db *mongodb.Database) *SportCenterRepository {
	return &SportCenterRepository{
		db:         db,
		collection: db.Collection("sport_centers"),
	}
}

func (r *SportCenterRepository) Create(ctx context.Context, center *domain.SportCenter) error {
	if center.ID.IsZero() {
		center.ID = primitive.NewObjectID()
	}
	_, err := r.collection.InsertOne(ctx, center)
	return err
}

func (r *SportCenterRepository) FindByID(ctx context.Context, id primitive.ObjectID) (*domain.SportCenter, error) {
	var center domain.SportCenter
	err := r.collection.FindOne(ctx, bson.M{"_id": id}).Decode(&center)
	if err != nil {
		return nil, err
	}
	return &center, nil
}

func (r *SportCenterRepository) FindBySlug(ctx context.Context, slug string) (*domain.SportCenter, error) {
	var center domain.SportCenter
	err := r.collection.FindOne(ctx, bson.M{"slug": slug}).Decode(&center)
	if err != nil {
		return nil, err
	}
	return &center, nil
}

func (r *SportCenterRepository) FindAll(ctx context.Context) ([]domain.SportCenter, error) {
	cursor, err := r.collection.Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var centers []domain.SportCenter
	if err = cursor.All(ctx, &centers); err != nil {
		return nil, err
	}
	return centers, nil
}

func (r *SportCenterRepository) FindPaged(ctx context.Context, page, limit int, name, city string, date *time.Time, hour *int) ([]domain.SportCenter, int64, error) {
	match := bson.M{}
	if name != "" || city != "" {
		searchText := ""
		if name != "" {
			searchText += name + " "
		}
		if city != "" {
			searchText += city
		}
		match["$text"] = bson.M{"$search": searchText}
	}

	pipeline := mongodb.Pipeline{
		{{"$match", match}},
	}

	if hour != nil && date != nil {
		// Normalizar la fecha
		searchDate := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location())

		pipeline = append(pipeline,
			// Join with courts
			bson.D{{Key: "$lookup", Value: bson.D{
				{Key: "from", Value: "courts"},
				{Key: "localField", Value: "_id"},
				{Key: "foreignField", Value: "sport_center_id"},
				{Key: "as", Value: "courts"},
			}}},
			// Filter courts that have the requested hour in their schedule
			bson.D{{Key: "$addFields", Value: bson.D{
				{Key: "available_courts", Value: bson.D{
					{Key: "$filter", Value: bson.D{
						{Key: "input", Value: "$courts"},
						{Key: "as", Value: "court"},
						{Key: "cond", Value: bson.D{
							{Key: "$anyElementTrue", Value: bson.A{
								bson.D{{Key: "$map", Value: bson.D{
									{Key: "input", Value: "$$court.schedule"},
									{Key: "as", Value: "s"},
									{Key: "in", Value: bson.D{
										{Key: "$and", Value: bson.A{
											bson.D{{Key: "$eq", Value: bson.A{"$$s.hour", *hour}}},
											bson.D{{Key: "$eq", Value: bson.A{"$$s.status", "available"}}},
										}},
									}},
								}}},
							}},
						}},
					}},
				}},
			}}},
			// Join with bookings to check if those courts are already booked
			bson.D{{Key: "$lookup", Value: bson.D{
				{Key: "from", Value: "bookings"},
				{Key: "let", Value: bson.D{{Key: "center_id", Value: "$_id"}}},
				{Key: "pipeline", Value: mongodb.Pipeline{
					{{Key: "$match", Value: bson.D{
						{Key: "$expr", Value: bson.D{
							{Key: "$and", Value: bson.A{
								bson.D{{Key: "$eq", Value: bson.A{"$sport_center_id", "$$center_id"}}},
								bson.D{{Key: "$eq", Value: bson.A{"$date", searchDate}}},
								bson.D{{Key: "$eq", Value: bson.A{"$hour", *hour}}},
								bson.D{{Key: "$eq", Value: bson.A{"$status", "confirmed"}}},
							}},
						}},
					}}},
				}},
				{Key: "as", Value: "bookings"},
			}}},
			// Filter available courts that don't have a booking for that hour
			bson.D{{Key: "$addFields", Value: bson.D{
				{Key: "final_available_courts", Value: bson.D{
					{Key: "$filter", Value: bson.D{
						{Key: "input", Value: "$available_courts"},
						{Key: "as", Value: "court"},
						{Key: "cond", Value: bson.D{
							{Key: "$not", Value: bson.A{
								bson.D{{Key: "$in", Value: bson.A{"$$court._id", "$bookings.court_id"}}},
							}},
						}},
					}},
				}},
			}}},
			// Keep only centers with at least one final available court
			bson.D{{Key: "$match", Value: bson.D{
				{Key: "final_available_courts.0", Value: bson.D{{Key: "$exists", Value: true}}},
			}}},
		)
	}

	// For total count
	countPipeline := append(pipeline, bson.D{{Key: "$count", Value: "total"}})
	cursor, err := r.collection.Aggregate(ctx, countPipeline)
	if err != nil {
		return nil, 0, err
	}
	var countResult []bson.M
	if err = cursor.All(ctx, &countResult); err != nil {
		return nil, 0, err
	}
	var total int64
	if len(countResult) > 0 {
		if t, ok := countResult[0]["total"].(int32); ok {
			total = int64(t)
		} else if t, ok := countResult[0]["total"].(int64); ok {
			total = t
		}
	}

	// Pagination
	skip := int64((page - 1) * limit)
	pipeline = append(pipeline,
		bson.D{{Key: "$skip", Value: skip}},
		bson.D{{Key: "$limit", Value: int64(limit)}},
	)

	cursor, err = r.collection.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)

	var centers []domain.SportCenter
	if err = cursor.All(ctx, &centers); err != nil {
		return nil, 0, err
	}

	if centers == nil {
		centers = []domain.SportCenter{}
	}

	return centers, total, nil
}

func (r *SportCenterRepository) FindByUserID(ctx context.Context, userID string) ([]domain.SportCenter, error) {
	// MongoDB find where "users" array contains userID
	cursor, err := r.collection.Find(ctx, bson.M{"users": userID})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var centers []domain.SportCenter
	if err = cursor.All(ctx, &centers); err != nil {
		return nil, err
	}

	if centers == nil {
		centers = []domain.SportCenter{}
	}

	return centers, nil
}

func (r *SportCenterRepository) Update(ctx context.Context, center *domain.SportCenter) error {
	_, err := r.collection.ReplaceOne(ctx, bson.M{"_id": center.ID}, center)
	return err
}

func (r *SportCenterRepository) UpdateSettings(ctx context.Context, id primitive.ObjectID, slug string, cancellationHours int, retentionPercent int, partialPaymentEnabled bool, partialPaymentPercent int) error {
	_, err := r.collection.UpdateOne(ctx, bson.M{"_id": id}, bson.M{
		"$set": bson.M{
			"slug":                    slug,
			"cancellation_hours":      cancellationHours,
			"retention_percent":       retentionPercent,
			"partial_payment_enabled": partialPaymentEnabled,
			"partial_payment_percent": partialPaymentPercent,
			"updated_at":              time.Now(),
		},
	})
	return err
}

func (r *SportCenterRepository) GetCities(ctx context.Context) ([]string, error) {
	// Filter out empty cities
	values, err := r.collection.Distinct(ctx, "city", bson.M{"city": bson.M{"$ne": ""}})
	if err != nil {
		return nil, err
	}

	cities := make([]string, 0, len(values))
	for _, v := range values {
		if s, ok := v.(string); ok {
			cities = append(cities, s)
		}
	}
	return cities, nil
}

func (r *SportCenterRepository) SyncCourtsCount(ctx context.Context) error {
	log.Println("[MONGODB] Sincronizando contador de canchas...")
	cursor, err := r.collection.Find(ctx, bson.M{})
	if err != nil {
		return fmt.Errorf("error al obtener centros para sincronización: %w", err)
	}
	defer cursor.Close(ctx)

	updatedCount := 0
	for cursor.Next(ctx) {
		var center domain.SportCenter
		if err := cursor.Decode(&center); err != nil {
			log.Printf("[MONGODB] Error al decodificar centro en sincronización: %v", err)
			continue
		}

		count, err := r.db.Collection("courts").CountDocuments(ctx, bson.M{"sport_center_id": center.ID})
		if err != nil {
			log.Printf("[MONGODB] Error al contar canchas para centro %s: %v", center.ID.Hex(), err)
			continue
		}

		_, err = r.collection.UpdateOne(ctx, bson.M{"_id": center.ID}, bson.M{"$set": bson.M{"courts_count": int(count)}})
		if err != nil {
			log.Printf("[MONGODB] Error al actualizar contador para centro %s: %v", center.ID.Hex(), err)
			continue
		}
		updatedCount++
	}

	log.Printf("[MONGODB] Sincronización finalizada. %d centros actualizados\n", updatedCount)
	return nil
}

type CourtRepository struct {
	db         *mongodb.Database
	collection *mongodb.Collection
}

func NewCourtRepository(db *mongodb.Database) *CourtRepository {
	return &CourtRepository{
		db:         db,
		collection: db.Collection("courts"),
	}
}

func (r *CourtRepository) Create(ctx context.Context, court *domain.Court) error {
	if court.ID.IsZero() {
		court.ID = primitive.NewObjectID()
	}
	_, err := r.collection.InsertOne(ctx, court)
	if err != nil {
		return err
	}

	// Incrementar contador de canchas en el centro deportivo
	_, err = r.db.Collection("sport_centers").UpdateOne(
		ctx,
		bson.M{"_id": court.SportCenterID},
		bson.M{"$inc": bson.M{"courts_count": 1}},
	)
	return err
}

func (r *CourtRepository) FindByCenterID(ctx context.Context, centerID primitive.ObjectID) ([]domain.Court, error) {
	cursor, err := r.collection.Find(ctx, bson.M{"sport_center_id": centerID})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var courts []domain.Court
	if err = cursor.All(ctx, &courts); err != nil {
		return nil, err
	}
	return courts, nil
}

func (r *CourtRepository) FindByID(ctx context.Context, id primitive.ObjectID) (*domain.Court, error) {
	var court domain.Court
	err := r.collection.FindOne(ctx, bson.M{"_id": id}).Decode(&court)
	if err != nil {
		return nil, err
	}
	return &court, nil
}

func (r *CourtRepository) Update(ctx context.Context, court *domain.Court) error {
	_, err := r.collection.ReplaceOne(ctx, bson.M{"_id": court.ID}, court)
	return err
}

func (r *CourtRepository) UpdateScheduleSlot(ctx context.Context, id primitive.ObjectID, slot domain.CourtSchedule) error {
	// First, try to update the slot if it exists
	filter := bson.M{
		"_id":           id,
		"schedule.hour": slot.Hour,
	}
	update := bson.M{
		"$set": bson.M{
			"schedule.$": slot,
			"updated_at": time.Now(),
		},
	}

	result, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return err
	}

	// If no document was matched, it means the hour slot doesn't exist in the array, so push it
	if result.MatchedCount == 0 {
		filter = bson.M{"_id": id}
		update = bson.M{
			"$push": bson.M{"schedule": slot},
			"$set":  bson.M{"updated_at": time.Now()},
		}
		_, err = r.collection.UpdateOne(ctx, filter, update)
		return err
	}

	return nil
}

func (r *CourtRepository) Delete(ctx context.Context, id primitive.ObjectID) error {
	// Obtener el court para saber a qué centro pertenece
	court, err := r.FindByID(ctx, id)
	if err != nil {
		return err
	}

	_, err = r.collection.DeleteOne(ctx, bson.M{"_id": id})
	if err != nil {
		return err
	}

	// Decrementar contador de canchas en el centro deportivo
	_, err = r.db.Collection("sport_centers").UpdateOne(
		ctx,
		bson.M{"_id": court.SportCenterID},
		bson.M{"$inc": bson.M{"courts_count": -1}},
	)
	return err
}

func (r *CourtRepository) FindAllPaged(ctx context.Context, page, limit int) ([]domain.Court, int64, error) {
	total, err := r.collection.CountDocuments(ctx, bson.M{})
	if err != nil {
		return nil, 0, err
	}

	skip := int64((page - 1) * limit)
	opts := options.Find().SetLimit(int64(limit)).SetSkip(skip)

	cursor, err := r.collection.Find(ctx, bson.M{}, opts)
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)

	var courts []domain.Court
	if err = cursor.All(ctx, &courts); err != nil {
		return nil, 0, err
	}

	if courts == nil {
		courts = []domain.Court{}
	}

	return courts, total, nil
}

func (r *CourtRepository) SyncPartialPaymentSlots(ctx context.Context, centerID primitive.ObjectID, partialPaymentEnabled bool) (int64, error) {
	result, err := r.collection.UpdateMany(
		ctx,
		bson.M{
			"sport_center_id":                  centerID,
			"schedule.partial_payment_enabled": nil,
		},
		bson.M{
			"$set": bson.M{
				"schedule.$[].partial_payment_enabled": partialPaymentEnabled,
				"updated_at":                           time.Now(),
			},
		},
	)
	if err != nil {
		return 0, err
	}
	return result.ModifiedCount, nil
}

type UserRepository struct {
	collection *mongodb.Collection
}

func NewUserRepository(db *mongodb.Database) *UserRepository {
	return &UserRepository{
		collection: db.Collection("users"),
	}
}

func (r *UserRepository) Create(ctx context.Context, user *domain.User) error {
	if user.ID.IsZero() {
		user.ID = primitive.NewObjectID()
	}
	_, err := r.collection.InsertOne(ctx, user)
	return err
}

func (r *UserRepository) FindByUsername(ctx context.Context, username string) (*domain.User, error) {
	var user domain.User
	err := r.collection.FindOne(ctx, bson.M{"username": username}).Decode(&user)
	if err != nil {
		return nil, err
	}
	return &user, nil
}
