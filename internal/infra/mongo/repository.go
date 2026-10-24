package mongo

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"
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

func (r *SportCenterRepository) FindBySubdomain(ctx context.Context, subdomain string) (*domain.SportCenter, error) {
	var center domain.SportCenter
	err := r.collection.FindOne(ctx, bson.M{"subdomain": subdomain}).Decode(&center)
	if err != nil {
		return nil, err
	}
	return &center, nil
}

func (r *SportCenterRepository) FindByNameAndRUT(ctx context.Context, name, rut string) (*domain.SportCenter, error) {
	var center domain.SportCenter
	err := r.collection.FindOne(ctx, bson.M{"name": name, "rut": rut}).Decode(&center)
	if err != nil {
		return nil, err
	}
	return &center, nil
}

func (r *SportCenterRepository) FindByAdminEmail(ctx context.Context, email string) (*domain.SportCenter, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	var center domain.SportCenter
	err := r.collection.FindOne(ctx, bson.M{"admin_email": email}).Decode(&center)
	if err != nil {
		return nil, err
	}
	return &center, nil
}

func (r *SportCenterRepository) FindByHost(ctx context.Context, host string) (*domain.SportCenter, error) {
	var center domain.SportCenter
	err := r.collection.FindOne(ctx, bson.M{"domains": host}).Decode(&center)
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

func (r *SportCenterRepository) FindPaged(ctx context.Context, page, limit int, name, city string, date *time.Time, hour *int, lat, lng, radiusKm *float64, priorityCity string) ([]domain.SportCenter, int64, bool, error) {
	pipeline := mongodb.Pipeline{}

	useGeoNear := lat != nil && lng != nil
	applyRadius := useGeoNear && radiusKm != nil && *radiusKm > 0

	// Construye la stage $geoNear; con withRadius aplica el corte por distancia.
	var geoNearBuilder func(withRadius bool) bson.D

	if useGeoNear {
		query := bson.D{}
		if name != "" {
			query = append(query, bson.E{
				Key:   "name",
				Value: bson.M{"$regex": regexp.QuoteMeta(name), "$options": "i"},
			})
		}
		if city != "" {
			query = append(query, bson.E{
				Key:   "city",
				Value: bson.M{"$regex": "^" + regexp.QuoteMeta(city), "$options": "i"},
			})
		}

		geoNearBuilder = func(withRadius bool) bson.D {
			// $geoNear debe ser el primer stage. Los filtros name/city van en su campo "query".
			geoNear := bson.D{
				{Key: "near", Value: bson.D{
					{Key: "type", Value: "Point"},
					{Key: "coordinates", Value: bson.A{*lng, *lat}},
				}},
				{Key: "distanceField", Value: "distance"},
			}
			if withRadius {
				geoNear = append(geoNear, bson.E{Key: "maxDistance", Value: *radiusKm * 1000})
			}
			geoNear = append(geoNear, bson.E{Key: "spherical", Value: true})
			if len(query) > 0 {
				geoNear = append(geoNear, bson.E{Key: "query", Value: query})
			}
			return geoNear
		}

		pipeline = append(pipeline, bson.D{{Key: "$geoNear", Value: geoNearBuilder(applyRadius)}})

		// Prioridad: destacados en la ciudad de referencia primero, luego por distancia.
		priorityExpr := bson.D{{Key: "$eq", Value: bson.A{"$is_featured", true}}}
		if priorityCity != "" {
			priorityExpr = bson.D{{Key: "$and", Value: bson.A{
				bson.D{{Key: "$eq", Value: bson.A{"$is_featured", true}}},
				bson.D{{Key: "$eq", Value: bson.A{"$city", priorityCity}}},
			}}}
		}
		pipeline = append(pipeline,
			bson.D{{Key: "$addFields", Value: bson.D{
				{Key: "priority", Value: priorityExpr},
				{Key: "distance_km", Value: bson.D{{Key: "$divide", Value: bson.A{"$distance", 1000}}}},
			}}},
			bson.D{{Key: "$sort", Value: bson.D{
				{Key: "priority", Value: -1},
				{Key: "distance", Value: 1},
			}}},
		)
	} else {
		matchStage := bson.D{}

		if name != "" {
			matchStage = append(matchStage, bson.E{
				Key:   "name",
				Value: bson.M{"$regex": regexp.QuoteMeta(name), "$options": "i"},
			})
		}

		if city != "" {
			matchStage = append(matchStage, bson.E{
				Key:   "city",
				Value: bson.M{"$regex": "^" + regexp.QuoteMeta(city), "$options": "i"},
			})
		}

		pipeline = append(pipeline, bson.D{{Key: "$match", Value: matchStage}})
	}

	if hour != nil && date != nil {
		// Normalizar la fecha
		searchDate := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location())

		pipeline = append(pipeline,
			// Join with spaces (canchas) que tengan el horario solicitado
			bson.D{{Key: "$lookup", Value: bson.D{
				{Key: "from", Value: "spaces"},
				{Key: "let", Value: bson.D{{Key: "center_id", Value: "$_id"}}},
				{Key: "pipeline", Value: mongodb.Pipeline{
					{{Key: "$match", Value: bson.D{
						{Key: "$expr", Value: bson.D{{Key: "$eq", Value: bson.A{"$sport_center_id", "$$center_id"}}}},
						{Key: "category", Value: domain.SpaceCategoryCancha},
					}}},
				}},
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

	// For total count. Copia la pipeline para no compartir el backing array.
	runCount := func(p mongodb.Pipeline) (int64, error) {
		countPipeline := append(mongodb.Pipeline{}, p...)
		countPipeline = append(countPipeline, bson.D{{Key: "$count", Value: "total"}})
		cursor, err := r.collection.Aggregate(ctx, countPipeline)
		if err != nil {
			return 0, err
		}
		defer cursor.Close(ctx)
		var countResult []bson.M
		if err = cursor.All(ctx, &countResult); err != nil {
			return 0, err
		}
		if len(countResult) == 0 {
			return 0, nil
		}
		switch t := countResult[0]["total"].(type) {
		case int32:
			return int64(t), nil
		case int64:
			return t, nil
		case int:
			return int64(t), nil
		}
		return 0, nil
	}

	total, err := runCount(pipeline)
	if err != nil {
		return nil, 0, false, err
	}

	// Si el radio no dejó ningún registro, se ignora y se retorna por defecto
	// la cantidad que corresponde al limit (los más cercanos sin corte de radio).
	usedRadiusFallback := false
	if applyRadius && total == 0 && geoNearBuilder != nil {
		usedRadiusFallback = true
		pipeline[0] = bson.D{{Key: "$geoNear", Value: geoNearBuilder(false)}}
		total, err = runCount(pipeline)
		if err != nil {
			return nil, 0, false, err
		}
	}

	// Pagination
	skip := int64((page - 1) * limit)
	pipeline = append(pipeline,
		bson.D{{Key: "$skip", Value: skip}},
		bson.D{{Key: "$limit", Value: int64(limit)}},
	)

	cursor, err := r.collection.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, 0, false, err
	}
	defer cursor.Close(ctx)

	var centers []domain.SportCenter
	if err = cursor.All(ctx, &centers); err != nil {
		return nil, 0, false, err
	}

	if centers == nil {
		centers = []domain.SportCenter{}
	}

	return centers, total, usedRadiusFallback, nil
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

// UpdateGooglePlaceID guarda (o limpia, si viene vacío) el Place ID de Google del centro.
func (r *SportCenterRepository) UpdateGooglePlaceID(ctx context.Context, id primitive.ObjectID, placeID string) error {
	update := bson.M{"$set": bson.M{"updated_at": time.Now()}}
	if placeID == "" {
		update["$unset"] = bson.M{"google_place_id": ""}
	} else {
		update["$set"].(bson.M)["google_place_id"] = placeID
	}
	_, err := r.collection.UpdateOne(ctx, bson.M{"_id": id}, update)
	return err
}

func (r *SportCenterRepository) UpdateSettings(ctx context.Context, id primitive.ObjectID, slug *string, cancellationHours *int, retentionPercent *int, partialPaymentEnabled *bool, partialPaymentPercent *int, imageURL *string, marketplaceFeePercent *int, isFeatured *bool, branding *domain.SportCenterBranding, address *string, city *string, coordinates *domain.Coordinates, paymentMode *string) error {
	updateData := bson.M{
		"updated_at": time.Now(),
	}

	if slug != nil {
		updateData["slug"] = *slug
	}
	if cancellationHours != nil {
		updateData["cancellation_hours"] = *cancellationHours
	}
	if retentionPercent != nil {
		updateData["retention_percent"] = *retentionPercent
	}
	if partialPaymentEnabled != nil {
		updateData["partial_payment_enabled"] = *partialPaymentEnabled
	}
	if partialPaymentPercent != nil {
		updateData["partial_payment_percent"] = *partialPaymentPercent
	}
	if imageURL != nil {
		updateData["image_url"] = *imageURL
	}
	if marketplaceFeePercent != nil {
		updateData["marketplace_fee_percent"] = *marketplaceFeePercent
	}
	if paymentMode != nil {
		updateData["payment_mode"] = *paymentMode
	}
	if isFeatured != nil {
		updateData["is_featured"] = *isFeatured
	}
	if address != nil {
		updateData["address"] = *address
	}
	if city != nil {
		updateData["city"] = *city
	}
	if coordinates != nil {
		updateData["coordinates"] = bson.M{
			"lat": coordinates.Lat,
			"lng": coordinates.Lng,
		}
		// location es la representación GeoJSON ([lng, lat]) usada por el índice
		// 2dsphere y $geoNear; debe mantenerse sincronizada con coordinates.
		updateData["location"] = bson.M{
			"type":        "Point",
			"coordinates": bson.A{coordinates.Lng, coordinates.Lat},
		}
	}

	if branding != nil {
		if branding.Name != "" {
			updateData["branding.name"] = branding.Name
		}
		if branding.LogoURL != "" {
			updateData["branding.logo_url"] = branding.LogoURL
		}
		if branding.PrimaryColor != "" {
			updateData["branding.primary_color"] = branding.PrimaryColor
		}
		if branding.PrimaryDark != "" {
			updateData["branding.primary_dark"] = branding.PrimaryDark
		}
		if branding.Accent != "" {
			updateData["branding.accent"] = branding.Accent
		}
		if branding.NavBg != "" {
			updateData["branding.nav_bg"] = branding.NavBg
		}
		if branding.HeroGradient != "" {
			updateData["branding.hero_gradient"] = branding.HeroGradient
		}
	}

	_, err := r.collection.UpdateOne(ctx, bson.M{"_id": id}, bson.M{
		"$set": updateData,
	})
	return err
}

// UpdateMercadoPagoConfig guarda o limpia las credenciales de Mercado Pago
// Connect de un centro deportivo. Si config es nil, elimina la configuración.
func (r *SportCenterRepository) UpdateMercadoPagoConfig(ctx context.Context, id primitive.ObjectID, config *domain.MercadoPagoConfig) error {
	if config == nil {
		_, err := r.collection.UpdateOne(ctx, bson.M{"_id": id}, bson.M{
			"$unset": bson.M{"mercadopago": ""},
			"$set":   bson.M{"updated_at": time.Now()},
		})
		return err
	}

	_, err := r.collection.UpdateOne(ctx, bson.M{"_id": id}, bson.M{
		"$set": bson.M{
			"mercadopago": config,
			"updated_at":  time.Now(),
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

		count, err := r.db.Collection("spaces").CountDocuments(ctx, bson.M{"sport_center_id": center.ID, "category": domain.SpaceCategoryCancha})
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
		collection: db.Collection("spaces"),
	}
}

func (r *CourtRepository) Create(ctx context.Context, court *domain.Court) error {
	space := domain.SpaceFromCourt(court)
	if space.ID.IsZero() {
		space.ID = primitive.NewObjectID()
		court.ID = space.ID
	}
	space.Enabled = true
	space.CreatedAt = time.Now()
	space.UpdatedAt = time.Now()
	_, err := r.collection.InsertOne(ctx, space)
	if err != nil {
		return err
	}

	// Incrementar contador de canchas en el centro deportivo
	_, err = r.db.Collection("sport_centers").UpdateOne(
		ctx,
		bson.M{"_id": space.SportCenterID},
		bson.M{"$inc": bson.M{"courts_count": 1}},
	)
	return err
}

func (r *CourtRepository) FindByCenterID(ctx context.Context, centerID primitive.ObjectID) ([]domain.Court, error) {
	cursor, err := r.collection.Find(ctx, bson.M{"sport_center_id": centerID, "category": domain.SpaceCategoryCancha})
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
	err := r.collection.FindOne(ctx, bson.M{"_id": id, "category": domain.SpaceCategoryCancha}).Decode(&court)
	if err != nil {
		return nil, err
	}
	return &court, nil
}

func (r *CourtRepository) Update(ctx context.Context, court *domain.Court) error {
	space := domain.SpaceFromCourt(court)
	space.Enabled = true
	space.UpdatedAt = time.Now()
	_, err := r.collection.ReplaceOne(ctx, bson.M{"_id": space.ID}, space)
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
	total, err := r.collection.CountDocuments(ctx, bson.M{"category": domain.SpaceCategoryCancha})
	if err != nil {
		return nil, 0, err
	}

	skip := int64((page - 1) * limit)
	opts := options.Find().SetLimit(int64(limit)).SetSkip(skip)

	cursor, err := r.collection.Find(ctx, bson.M{"category": domain.SpaceCategoryCancha}, opts)
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
			"category":                         domain.SpaceCategoryCancha,
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

// SyncPaymentModeSlots alinea el horario legacy de las canchas del centro con
// su modalidad comercial (pago online vs solo reservas).
func (r *CourtRepository) SyncPaymentModeSlots(ctx context.Context, centerID primitive.ObjectID, paymentRequired bool) (int64, error) {
	result, err := r.collection.UpdateMany(
		ctx,
		bson.M{
			"sport_center_id": centerID,
			"$or": bson.A{
				bson.M{"category": domain.SpaceCategoryCancha},
				bson.M{"category": bson.M{"$exists": false}},
				bson.M{"category": ""},
			},
		},
		bson.M{
			"$set": bson.M{
				"schedule.$[].payment_required": paymentRequired,
				"schedule.$[].payment_optional": false,
				"updated_at":                    time.Now(),
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
