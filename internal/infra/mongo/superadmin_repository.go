package mongo

import (
	"context"
	"regexp"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	mongodb "go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// AuditLog registra una acción del superadmin.
type AuditLog struct {
	ID         primitive.ObjectID     `bson:"_id,omitempty" json:"id"`
	ActorID    string                 `bson:"actor_id" json:"actor_id"`
	ActorEmail string                 `bson:"actor_email,omitempty" json:"actor_email,omitempty"`
	Action     string                 `bson:"action" json:"action"`
	EntityType string                 `bson:"entity_type" json:"entity_type"`
	EntityID   string                 `bson:"entity_id" json:"entity_id"`
	Reason     string                 `bson:"reason,omitempty" json:"reason,omitempty"`
	Details    map[string]interface{} `bson:"details,omitempty" json:"details,omitempty"`
	IP         string                 `bson:"ip,omitempty" json:"ip,omitempty"`
	CreatedAt  time.Time              `bson:"created_at" json:"created_at"`
}

type SuperAdminCenterFilter struct {
	Query       string
	Status      string
	PaymentMode string
	City        string
	Featured    *bool
}

type SuperAdminBookingFilter struct {
	CenterID *primitive.ObjectID
	Status   string
	From     *time.Time
	To       *time.Time
	Query    string
}

type SuperAdminRepository struct {
	centers  *mongodb.Collection
	bookings *mongodb.Collection
	audit    *mongodb.Collection
}

func NewSuperAdminRepository(db *mongodb.Database) *SuperAdminRepository {
	return &SuperAdminRepository{
		centers:  db.Collection("sport_centers"),
		bookings: db.Collection("bookings"),
		audit:    db.Collection("audit_logs"),
	}
}

func (r *SuperAdminRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.audit.Indexes().CreateOne(ctx, mongodb.IndexModel{
		Keys:    bson.D{{Key: "created_at", Value: -1}},
		Options: options.Index().SetName("idx_audit_created_at"),
	})
	return err
}

func centerFilter(f SuperAdminCenterFilter) bson.M {
	q := bson.M{}
	if f.Query != "" {
		rx := bson.M{"$regex": regexp.QuoteMeta(f.Query), "$options": "i"}
		q["$or"] = bson.A{
			bson.M{"name": rx}, bson.M{"slug": rx}, bson.M{"admin_email": rx}, bson.M{"rut": rx},
		}
	}
	switch f.Status {
	case "":
	case domain.SportCenterStatusActive:
		q["status"] = bson.M{"$in": bson.A{"", domain.SportCenterStatusActive, nil}}
	default:
		q["status"] = f.Status
	}
	if f.PaymentMode != "" {
		q["payment_mode"] = f.PaymentMode
	}
	if f.City != "" {
		q["city"] = bson.M{"$regex": "^" + regexp.QuoteMeta(f.City), "$options": "i"}
	}
	if f.Featured != nil {
		if *f.Featured {
			q["is_featured"] = true
		} else {
			q["is_featured"] = bson.M{"$ne": true}
		}
	}
	return q
}

func (r *SuperAdminRepository) ListCenters(ctx context.Context, f SuperAdminCenterFilter, page, limit int) ([]domain.SportCenter, int64, error) {
	q := centerFilter(f)
	total, err := r.centers.CountDocuments(ctx, q)
	if err != nil {
		return nil, 0, err
	}
	cur, err := r.centers.Find(ctx, q, options.Find().
		SetSort(bson.D{{Key: "is_featured", Value: -1}, {Key: "featured_priority", Value: -1}, {Key: "created_at", Value: -1}}).
		SetSkip(int64((page-1)*limit)).SetLimit(int64(limit)))
	if err != nil {
		return nil, 0, err
	}
	defer cur.Close(ctx)
	out := []domain.SportCenter{}
	if err := cur.All(ctx, &out); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

type CenterStats struct {
	Total     int64 `json:"total"`
	Active    int64 `json:"active"`
	Suspended int64 `json:"suspended"`
	Blocked   int64 `json:"blocked"`
	Featured  int64 `json:"featured"`
	PayOnline int64 `json:"pay_online"`
}

func (r *SuperAdminRepository) CenterStats(ctx context.Context) (*CenterStats, error) {
	count := func(q bson.M) int64 {
		n, _ := r.centers.CountDocuments(ctx, q)
		return n
	}
	return &CenterStats{
		Total:     count(bson.M{}),
		Active:    count(bson.M{"status": bson.M{"$nin": bson.A{domain.SportCenterStatusSuspended, domain.SportCenterStatusBlocked}}}),
		Suspended: count(bson.M{"status": domain.SportCenterStatusSuspended}),
		Blocked:   count(bson.M{"status": domain.SportCenterStatusBlocked}),
		Featured:  count(bson.M{"is_featured": true}),
		PayOnline: count(bson.M{"payment_mode": "pay_online"}),
	}, nil
}

func (r *SuperAdminRepository) GetCenter(ctx context.Context, id primitive.ObjectID) (*domain.SportCenter, error) {
	var c domain.SportCenter
	if err := r.centers.FindOne(ctx, bson.M{"_id": id}).Decode(&c); err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *SuperAdminRepository) SetStatus(ctx context.Context, id primitive.ObjectID, status, reason, actor string) error {
	now := time.Now()
	res, err := r.centers.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": bson.M{
		"status":            status,
		"status_reason":     reason,
		"status_changed_at": now,
		"status_changed_by": actor,
		"updated_at":        now,
	}})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return mongodb.ErrNoDocuments
	}
	return nil
}

func (r *SuperAdminRepository) SetFeatured(ctx context.Context, id primitive.ObjectID, featured bool, priority int, until *time.Time) error {
	set := bson.M{"is_featured": featured, "featured_priority": priority, "updated_at": time.Now()}
	update := bson.M{"$set": set}
	if until != nil {
		set["featured_until"] = *until
	} else {
		update["$unset"] = bson.M{"featured_until": ""}
	}
	res, err := r.centers.UpdateOne(ctx, bson.M{"_id": id}, update)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return mongodb.ErrNoDocuments
	}
	return nil
}

func bookingFilter(f SuperAdminBookingFilter) bson.M {
	q := bson.M{}
	if f.CenterID != nil {
		q["sport_center_id"] = *f.CenterID
	}
	if f.Status != "" {
		q["status"] = f.Status
	}
	if f.From != nil || f.To != nil {
		d := bson.M{}
		if f.From != nil {
			d["$gte"] = *f.From
		}
		if f.To != nil {
			d["$lte"] = *f.To
		}
		q["date"] = d
	}
	if f.Query != "" {
		rx := bson.M{"$regex": regexp.QuoteMeta(f.Query), "$options": "i"}
		q["$or"] = bson.A{
			bson.M{"booking_code": rx}, bson.M{"guest_details.email": rx},
			bson.M{"guest_details.name": rx}, bson.M{"user_id": rx},
		}
	}
	return q
}

func (r *SuperAdminRepository) ListBookings(ctx context.Context, f SuperAdminBookingFilter, page, limit int) ([]domain.Booking, int64, error) {
	q := bookingFilter(f)
	total, err := r.bookings.CountDocuments(ctx, q)
	if err != nil {
		return nil, 0, err
	}
	cur, err := r.bookings.Find(ctx, q, options.Find().
		SetSort(bson.D{{Key: "date", Value: -1}, {Key: "hour", Value: -1}}).
		SetSkip(int64((page-1)*limit)).SetLimit(int64(limit)))
	if err != nil {
		return nil, 0, err
	}
	defer cur.Close(ctx)
	out := []domain.Booking{}
	if err := cur.All(ctx, &out); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

func (r *SuperAdminRepository) WriteAudit(ctx context.Context, l *AuditLog) error {
	l.CreatedAt = time.Now()
	_, err := r.audit.InsertOne(ctx, l)
	return err
}

func (r *SuperAdminRepository) ListAudit(ctx context.Context, entityID string, page, limit int) ([]AuditLog, int64, error) {
	q := bson.M{}
	if entityID != "" {
		q["entity_id"] = entityID
	}
	total, err := r.audit.CountDocuments(ctx, q)
	if err != nil {
		return nil, 0, err
	}
	cur, err := r.audit.Find(ctx, q, options.Find().
		SetSort(bson.D{{Key: "created_at", Value: -1}}).
		SetSkip(int64((page-1)*limit)).SetLimit(int64(limit)))
	if err != nil {
		return nil, 0, err
	}
	defer cur.Close(ctx)
	out := []AuditLog{}
	if err := cur.All(ctx, &out); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}
