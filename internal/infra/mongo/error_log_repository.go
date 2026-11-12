package mongo

import (
	"context"
	"regexp"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ErrorLogTTL es el tiempo que se conserva un error antes de que Mongo lo elimine.
const ErrorLogTTL = 90 * 24 * time.Hour

type ErrorLogRepository struct {
	collection *mongo.Collection
}

// ErrorLogFilter filtra el listado del superadmin.
type ErrorLogFilter struct {
	Query         string // tracking_id, email, user_id, booking_code o mp_payment_id
	Operation     string
	SportCenterID string
	Resolved      *bool
	From, To      time.Time
}

func NewErrorLogRepository(db *mongo.Database) *ErrorLogRepository {
	coll := db.Collection("booking_error_logs")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = coll.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "created_at", Value: 1}},
			Options: options.Index().SetName("idx_error_logs_ttl").SetExpireAfterSeconds(int32(ErrorLogTTL.Seconds())),
		},
		{
			Keys:    bson.D{{Key: "tracking_id", Value: 1}},
			Options: options.Index().SetName("idx_error_logs_tracking").SetUnique(true),
		},
		{Keys: bson.D{{Key: "sport_center_id", Value: 1}, {Key: "created_at", Value: -1}}},
		{Keys: bson.D{{Key: "user_id", Value: 1}}},
		{Keys: bson.D{{Key: "booking_id", Value: 1}}},
	})
	return &ErrorLogRepository{collection: coll}
}

// Insert guarda el error. Devuelve true si el tracking_id ya existía.
func (r *ErrorLogRepository) Insert(ctx context.Context, l *domain.BookingErrorLog) (duplicate bool, err error) {
	if l.CreatedAt.IsZero() {
		l.CreatedAt = time.Now()
	}
	_, err = r.collection.InsertOne(ctx, l)
	if mongo.IsDuplicateKeyError(err) {
		return true, nil
	}
	return false, err
}

func (r *ErrorLogRepository) GetByTrackingID(ctx context.Context, trackingID string) (*domain.BookingErrorLog, error) {
	var out domain.BookingErrorLog
	err := r.collection.FindOne(ctx, bson.M{"tracking_id": trackingID}).Decode(&out)
	if err == mongo.ErrNoDocuments {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (r *ErrorLogRepository) List(ctx context.Context, f ErrorLogFilter, page, limit int) ([]domain.BookingErrorLog, int64, error) {
	q := bson.M{}
	if f.Query != "" {
		rx := bson.M{"$regex": regexp.QuoteMeta(f.Query), "$options": "i"}
		q["$or"] = bson.A{
			bson.M{"tracking_id": rx}, bson.M{"user_email": rx}, bson.M{"user_id": rx},
			bson.M{"booking_code": rx}, bson.M{"booking_id": f.Query}, bson.M{"mp_payment_id": f.Query},
		}
	}
	if f.Operation != "" {
		q["operation"] = f.Operation
	}
	if f.SportCenterID != "" {
		q["sport_center_id"] = f.SportCenterID
	}
	if f.Resolved != nil {
		q["resolved"] = *f.Resolved
	}
	if !f.From.IsZero() || !f.To.IsZero() {
		r := bson.M{}
		if !f.From.IsZero() {
			r["$gte"] = f.From
		}
		if !f.To.IsZero() {
			r["$lt"] = f.To
		}
		q["created_at"] = r
	}
	total, err := r.collection.CountDocuments(ctx, q)
	if err != nil {
		return nil, 0, err
	}
	cur, err := r.collection.Find(ctx, q, options.Find().
		SetSort(bson.D{{Key: "created_at", Value: -1}}).
		SetSkip(int64((page-1)*limit)).SetLimit(int64(limit)))
	if err != nil {
		return nil, 0, err
	}
	out := []domain.BookingErrorLog{}
	if err := cur.All(ctx, &out); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

func (r *ErrorLogRepository) SetResolved(ctx context.Context, id primitive.ObjectID, resolved bool, by string) error {
	set := bson.M{"resolved": resolved}
	unset := bson.M{}
	if resolved {
		now := time.Now()
		set["resolved_by"] = by
		set["resolved_at"] = now
	} else {
		unset["resolved_by"] = ""
		unset["resolved_at"] = ""
	}
	upd := bson.M{"$set": set}
	if len(unset) > 0 {
		upd["$unset"] = unset
	}
	res, err := r.collection.UpdateByID(ctx, id, upd)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return domain.ErrNotFound
	}
	return nil
}
