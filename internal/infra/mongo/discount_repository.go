package mongo

import (
	"context"
	"errors"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// DiscountRepository implementa códigos, contadores atómicos por usuario y el
// historial de usos.
type DiscountRepository struct {
	codes    *mongo.Collection
	counters *mongo.Collection
	usages   *mongo.Collection
}

func NewDiscountRepository(db *mongo.Database) *DiscountRepository {
	return &DiscountRepository{
		codes:    db.Collection("discount_codes"),
		counters: db.Collection("discount_usage_counters"),
		usages:   db.Collection("discount_usages"),
	}
}

// --- Códigos ---

func (r *DiscountRepository) CreateCode(ctx context.Context, code *domain.DiscountCode) error {
	res, err := r.codes.InsertOne(ctx, code)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return domain.NewConflictError("el código ya existe en este centro")
		}
		return err
	}
	code.ID = res.InsertedID.(primitive.ObjectID)
	return nil
}

func (r *DiscountRepository) CreateCodes(ctx context.Context, codes []domain.DiscountCode) error {
	if len(codes) == 0 {
		return nil
	}
	docs := make([]interface{}, 0, len(codes))
	for i := range codes {
		docs = append(docs, codes[i])
	}
	_, err := r.codes.InsertMany(ctx, docs, options.InsertMany().SetOrdered(true))
	if err != nil && mongo.IsDuplicateKeyError(err) {
		return domain.NewConflictError("alguno de los códigos generados ya existe; intenta nuevamente")
	}
	return err
}

func (r *DiscountRepository) FindCodeByID(ctx context.Context, id primitive.ObjectID) (*domain.DiscountCode, error) {
	var code domain.DiscountCode
	if err := r.codes.FindOne(ctx, bson.M{"_id": id}).Decode(&code); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return &code, nil
}

func (r *DiscountRepository) FindCodeByCode(ctx context.Context, centerID primitive.ObjectID, code string) (*domain.DiscountCode, error) {
	var doc domain.DiscountCode
	filter := bson.M{"sport_center_id": centerID, "code": code}
	if err := r.codes.FindOne(ctx, filter).Decode(&doc); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return &doc, nil
}

func (r *DiscountRepository) FindCodesByCenter(ctx context.Context, centerID primitive.ObjectID) ([]domain.DiscountCode, error) {
	cursor, err := r.codes.Find(ctx, bson.M{"sport_center_id": centerID}, options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	codes := []domain.DiscountCode{}
	if err := cursor.All(ctx, &codes); err != nil {
		return nil, err
	}
	return codes, nil
}

func (r *DiscountRepository) FindCodesByBatch(ctx context.Context, centerID primitive.ObjectID, batchID string) ([]domain.DiscountCode, error) {
	filter := bson.M{"sport_center_id": centerID, "batch_id": batchID}
	cursor, err := r.codes.Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "code", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	codes := []domain.DiscountCode{}
	if err := cursor.All(ctx, &codes); err != nil {
		return nil, err
	}
	return codes, nil
}

func (r *DiscountRepository) UpdateCodeStatus(ctx context.Context, id primitive.ObjectID, status domain.DiscountCodeStatus) error {
	_, err := r.codes.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": bson.M{"status": status, "updated_at": time.Now()}})
	return err
}

func (r *DiscountRepository) UpdateBatchStatus(ctx context.Context, centerID primitive.ObjectID, batchID string, status domain.DiscountCodeStatus) error {
	_, err := r.codes.UpdateMany(ctx, bson.M{"sport_center_id": centerID, "batch_id": batchID}, bson.M{"$set": bson.M{"status": status, "updated_at": time.Now()}})
	return err
}

// TryIncrementCodeUsed reserva atómicamente un uso global del código. Si
// maxUses es 0 el código es ilimitado.
func (r *DiscountRepository) TryIncrementCodeUsed(ctx context.Context, id primitive.ObjectID, maxUses int) (bool, error) {
	filter := bson.M{"_id": id, "status": domain.DiscountCodeStatusActive}
	if maxUses > 0 {
		filter["$expr"] = bson.M{"$lt": bson.A{"$used_count", maxUses}}
	}
	res, err := r.codes.UpdateOne(ctx, filter, bson.M{"$inc": bson.M{"used_count": 1}, "$set": bson.M{"updated_at": time.Now()}})
	if err != nil {
		return false, err
	}
	return res.MatchedCount == 1, nil
}

func (r *DiscountRepository) ReleaseCodeUsed(ctx context.Context, id primitive.ObjectID) error {
	_, err := r.codes.UpdateOne(ctx,
		bson.M{"_id": id, "used_count": bson.M{"$gt": 0}},
		bson.M{"$inc": bson.M{"used_count": -1}, "$set": bson.M{"updated_at": time.Now()}},
	)
	return err
}

// --- Contadores por usuario ---

// TryReserveCounter reserva un uso para una identidad y período. Devuelve
// false cuando se alcanzó el límite. La reserva es atómica: si dos checkouts
// compiten por el último uso, solo uno gana.
func (r *DiscountRepository) TryReserveCounter(ctx context.Context, centerID primitive.ObjectID, source string, sourceID primitive.ObjectID, identityKey, periodKey string, limit int) (bool, error) {
	now := time.Now()
	filter := bson.M{
		"source":       source,
		"source_id":    sourceID,
		"identity_key": identityKey,
		"period_key":   periodKey,
	}
	if limit > 0 {
		filter["$expr"] = bson.M{"$lt": bson.A{
			bson.M{"$add": bson.A{"$reserved", "$confirmed"}},
			limit,
		}}
	}
	update := bson.M{
		"$inc": bson.M{"reserved": 1},
		"$set": bson.M{"updated_at": now},
		"$setOnInsert": bson.M{
			"sport_center_id": centerID,
			"source":          source,
			"source_id":       sourceID,
			"identity_key":    identityKey,
			"period_key":      periodKey,
		},
	}
	err := r.counters.FindOneAndUpdate(ctx, filter, update, options.FindOneAndUpdate().SetUpsert(true)).Err()
	if err != nil {
		// El upsert intenta insertar cuando el documento existe pero el
		// límite ya se alcanzó; el índice único lo rechaza.
		if mongo.IsDuplicateKeyError(err) || errors.Is(err, mongo.ErrNoDocuments) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (r *DiscountRepository) FindCounter(ctx context.Context, source string, sourceID primitive.ObjectID, identityKey, periodKey string) (*domain.DiscountUsageCounter, error) {
	var counter domain.DiscountUsageCounter
	filter := bson.M{"source": source, "source_id": sourceID, "identity_key": identityKey, "period_key": periodKey}
	if err := r.counters.FindOne(ctx, filter).Decode(&counter); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return &counter, nil
}

func (r *DiscountRepository) ConfirmCounter(ctx context.Context, source string, sourceID primitive.ObjectID, identityKey, periodKey string) error {
	_, err := r.counters.UpdateOne(ctx,
		bson.M{"source": source, "source_id": sourceID, "identity_key": identityKey, "period_key": periodKey, "reserved": bson.M{"$gt": 0}},
		bson.M{"$inc": bson.M{"reserved": -1, "confirmed": 1}, "$set": bson.M{"updated_at": time.Now()}},
	)
	return err
}

func (r *DiscountRepository) ReleaseCounter(ctx context.Context, source string, sourceID primitive.ObjectID, identityKey, periodKey string) error {
	_, err := r.counters.UpdateOne(ctx,
		bson.M{"source": source, "source_id": sourceID, "identity_key": identityKey, "period_key": periodKey, "reserved": bson.M{"$gt": 0}},
		bson.M{"$inc": bson.M{"reserved": -1}, "$set": bson.M{"updated_at": time.Now()}},
	)
	return err
}

// ReleaseConfirmedCounter devuelve un uso ya confirmado (por ejemplo cuando se
// cancela una reserva pagada) para que el socio recupere su cupo.
func (r *DiscountRepository) ReleaseConfirmedCounter(ctx context.Context, source string, sourceID primitive.ObjectID, identityKey, periodKey string) error {
	_, err := r.counters.UpdateOne(ctx,
		bson.M{"source": source, "source_id": sourceID, "identity_key": identityKey, "period_key": periodKey, "confirmed": bson.M{"$gt": 0}},
		bson.M{"$inc": bson.M{"confirmed": -1}, "$set": bson.M{"updated_at": time.Now()}},
	)
	return err
}

// --- Usos ---

// CreateUsage guarda el uso del beneficio para una reserva. Si la reserva ya
// tenía un uso (por ejemplo al reintentar el pago con otro código), se
// reemplaza en lugar de ignorarse: el uso anterior debe haber sido liberado
// antes y el nuevo estado debe quedar reservado.
func (r *DiscountRepository) CreateUsage(ctx context.Context, usage *domain.DiscountUsage) error {
	now := time.Now()
	if usage.CreatedAt.IsZero() {
		usage.CreatedAt = now
	}
	usage.UpdatedAt = now

	filter := bson.M{"booking_id": usage.BookingID}
	update := bson.M{
		"$set": bson.M{
			"sport_center_id": usage.SportCenterID,
			"source":          usage.Source,
			"source_id":       usage.SourceID,
			"identity_key":    usage.IdentityKey,
			"period_key":      usage.PeriodKey,
			"member_id":       usage.MemberID,
			"code":            usage.Code,
			"status":          usage.Status,
			"base_amount":     usage.BaseAmount,
			"discount_amount": usage.DiscountAmount,
			"final_amount":    usage.FinalAmount,
			"customer_email":  usage.CustomerEmail,
			"customer_name":   usage.CustomerName,
			"customer_phone":  usage.CustomerPhone,
			"booking_code":    usage.BookingCode,
			"updated_at":      now,
		},
		"$setOnInsert": bson.M{"created_at": usage.CreatedAt},
	}

	var stored domain.DiscountUsage
	opts := options.FindOneAndUpdate().
		SetUpsert(true).
		SetReturnDocument(options.After)
	if err := r.usages.FindOneAndUpdate(ctx, filter, update, opts).Decode(&stored); err != nil {
		return err
	}
	usage.ID = stored.ID
	return nil
}

func (r *DiscountRepository) FindUsageByBookingID(ctx context.Context, bookingID primitive.ObjectID) (*domain.DiscountUsage, error) {
	var usage domain.DiscountUsage
	if err := r.usages.FindOne(ctx, bson.M{"booking_id": bookingID}).Decode(&usage); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return &usage, nil
}

func (r *DiscountRepository) FindUsagesByCodeID(ctx context.Context, codeID primitive.ObjectID) ([]domain.DiscountUsage, error) {
	cursor, err := r.usages.Find(ctx, bson.M{"source_id": codeID, "source": domain.DiscountSourceCode}, options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	usages := []domain.DiscountUsage{}
	if err := cursor.All(ctx, &usages); err != nil {
		return nil, err
	}
	return usages, nil
}

func (r *DiscountRepository) FindUsagesByCenter(ctx context.Context, centerID primitive.ObjectID, limit int) ([]domain.DiscountUsage, error) {
	opts := options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}})
	if limit > 0 {
		opts.SetLimit(int64(limit))
	}
	cursor, err := r.usages.Find(ctx, bson.M{"sport_center_id": centerID}, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	usages := []domain.DiscountUsage{}
	if err := cursor.All(ctx, &usages); err != nil {
		return nil, err
	}
	return usages, nil
}

func (r *DiscountRepository) UpdateUsageStatus(ctx context.Context, bookingID primitive.ObjectID, status string) error {
	_, err := r.usages.UpdateOne(ctx, bson.M{"booking_id": bookingID}, bson.M{"$set": bson.M{"status": status, "updated_at": time.Now()}})
	return err
}
