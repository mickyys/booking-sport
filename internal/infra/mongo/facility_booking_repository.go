package mongo

import (
	"context"
	"errors"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	mongodb "go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type FacilityBookingRepository struct {
	collection *mongodb.Collection
}

func NewFacilityBookingRepository(db *mongodb.Database) *FacilityBookingRepository {
	return &FacilityBookingRepository{collection: db.Collection("facility_bookings")}
}

// CreateMany inserta una reserva por día. El índice único parcial
// (space_id, local_date) rechaza un día ya tomado; en ese caso se revierten
// las inserciones previas y se devuelve domain.ErrBookingSlotTaken.
func (r *FacilityBookingRepository) CreateMany(ctx context.Context, bookings []*domain.FacilityBooking) error {
	inserted := make([]primitive.ObjectID, 0, len(bookings))
	for _, b := range bookings {
		if b.ID.IsZero() {
			b.ID = primitive.NewObjectID()
		}
		if _, err := r.collection.InsertOne(ctx, b); err != nil {
			if len(inserted) > 0 {
				_, _ = r.collection.DeleteMany(ctx, bson.M{"_id": bson.M{"$in": inserted}})
			}
			if mongodb.IsDuplicateKeyError(err) {
				return domain.ErrBookingSlotTaken
			}
			return err
		}
		inserted = append(inserted, b.ID)
	}
	return nil
}

// ExpireStale marca como expiradas las reservas pendientes de pago cuyo plazo
// venció, liberando sus días.
func (r *FacilityBookingRepository) ExpireStale(ctx context.Context, spaceID primitive.ObjectID, now time.Time) error {
	_, err := r.collection.UpdateMany(ctx,
		bson.M{
			"space_id":   spaceID,
			"status":     domain.BookingStatusPending,
			"expires_at": bson.M{"$lte": now},
		},
		bson.M{"$set": bson.M{"status": domain.BookingStatusExpired, "active": false, "updated_at": now}},
	)
	return err
}

func (r *FacilityBookingRepository) FindByCode(ctx context.Context, code string) ([]domain.FacilityBooking, error) {
	cursor, err := r.collection.Find(ctx, bson.M{"booking_code": code}, options.Find().SetSort(bson.D{{Key: "local_date", Value: 1}}))
	if err != nil {
		return nil, err
	}
	var out []domain.FacilityBooking
	if err := cursor.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *FacilityBookingRepository) FindByMPPaymentID(ctx context.Context, paymentID string) ([]domain.FacilityBooking, error) {
	cursor, err := r.collection.Find(ctx, bson.M{"mp_payment_id": paymentID})
	if err != nil {
		return nil, err
	}
	var out []domain.FacilityBooking
	if err := cursor.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *FacilityBookingRepository) FindByID(ctx context.Context, id primitive.ObjectID) (*domain.FacilityBooking, error) {
	var b domain.FacilityBooking
	if err := r.collection.FindOne(ctx, bson.M{"_id": id}).Decode(&b); err != nil {
		if errors.Is(err, mongodb.ErrNoDocuments) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return &b, nil
}

// ListActiveDates devuelve los días (YYYY-MM-DD) con reserva activa de un
// espacio dentro del rango [from, to] (inclusive).
func (r *FacilityBookingRepository) ListActiveDates(ctx context.Context, spaceID primitive.ObjectID, from, to string, now time.Time) ([]string, error) {
	cursor, err := r.collection.Find(ctx, bson.M{
		"space_id":   spaceID,
		"local_date": bson.M{"$gte": from, "$lte": to},
		"$or": bson.A{
			bson.M{"status": domain.BookingStatusConfirmed},
			bson.M{"status": domain.BookingStatusPending, "$or": bson.A{
				bson.M{"expires_at": bson.M{"$exists": false}},
				bson.M{"expires_at": bson.M{"$gt": now}},
			}},
		},
	}, options.Find().SetProjection(bson.M{"local_date": 1}))
	if err != nil {
		return nil, err
	}
	var docs []domain.FacilityBooking
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	dates := make([]string, 0, len(docs))
	for _, d := range docs {
		dates = append(dates, d.LocalDate)
	}
	return dates, nil
}

func (r *FacilityBookingRepository) ListByCenter(ctx context.Context, centerID primitive.ObjectID) ([]domain.FacilityBooking, error) {
	cursor, err := r.collection.Find(ctx,
		bson.M{"sport_center_id": centerID},
		options.Find().SetSort(bson.D{{Key: "local_date", Value: -1}}).SetLimit(500),
	)
	if err != nil {
		return nil, err
	}
	var out []domain.FacilityBooking
	if err := cursor.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *FacilityBookingRepository) SetPreference(ctx context.Context, code, preferenceID string) error {
	_, err := r.collection.UpdateMany(ctx, bson.M{"booking_code": code},
		bson.M{"$set": bson.M{"mp_preference_id": preferenceID, "updated_at": time.Now()}})
	return err
}

// SetPaymentID asocia el pago de Mercado Pago a todos los días de la reserva.
func (r *FacilityBookingRepository) SetPaymentID(ctx context.Context, code, paymentID string) error {
	_, err := r.collection.UpdateMany(ctx, bson.M{"booking_code": code},
		bson.M{"$set": bson.M{"mp_payment_id": paymentID, "updated_at": time.Now()}})
	return err
}

// Confirm confirma las reservas pendientes del grupo con el monto pagado por día.
func (r *FacilityBookingRepository) Confirm(ctx context.Context, code string, paidPerDay float64) error {
	now := time.Now()
	_, err := r.collection.UpdateMany(ctx,
		bson.M{"booking_code": code, "status": domain.BookingStatusPending},
		bson.A{bson.M{"$set": bson.M{
			"status":         domain.BookingStatusConfirmed,
			"paid_amount":    paidPerDay,
			"pending_amount": bson.M{"$max": bson.A{bson.M{"$subtract": bson.A{"$price", paidPerDay}}, 0}},
			"updated_at":     now,
		}}, bson.M{"$unset": "expires_at"}},
	)
	return err
}

// CancelByCode cancela (o expira) las reservas activas de todo el grupo.
func (r *FacilityBookingRepository) CancelByCode(ctx context.Context, code, cancelledBy string, status domain.BookingStatus) error {
	_, err := r.collection.UpdateMany(ctx,
		bson.M{"booking_code": code, "active": true},
		bson.M{"$set": bson.M{"status": status, "active": false, "cancelled_by": cancelledBy, "updated_at": time.Now()}},
	)
	return err
}

func (r *FacilityBookingRepository) CancelByID(ctx context.Context, id primitive.ObjectID, cancelledBy string) error {
	_, err := r.collection.UpdateOne(ctx,
		bson.M{"_id": id, "active": true},
		bson.M{"$set": bson.M{"status": domain.BookingStatusCancelled, "active": false, "cancelled_by": cancelledBy, "updated_at": time.Now()}},
	)
	return err
}
