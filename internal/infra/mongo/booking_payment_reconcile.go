package mongo

import (
	"context"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func (r *BookingRepository) ClaimPaymentTransition(ctx context.Context, id primitive.ObjectID, expected, next domain.BookingStatus, paidAmount, pendingAmount float64, paymentInfo *domain.PaymentInfo, mpPaymentID, mpStatus string) (bool, error) {
	now := time.Now()
	set := bson.M{
		"status":               next,
		"paid_amount":          paidAmount,
		"pending_amount":       pendingAmount,
		"updated_at":           now,
		"last_payment_sync_at": now,
		"mp_status":            mpStatus,
	}
	if mpPaymentID != "" {
		set["mp_payment_id"] = mpPaymentID
	}
	if paymentInfo != nil {
		set["payment_info"] = paymentInfo
	}
	res, err := r.collection.UpdateOne(ctx,
		bson.M{"_id": id, "status": expected},
		bson.M{"$set": set, "$unset": bson.M{"lock_expires_at": ""}},
	)
	if err != nil {
		return false, err
	}
	return res.MatchedCount == 1, nil
}

func (r *BookingRepository) MarkPaymentSync(ctx context.Context, id primitive.ObjectID, mpStatus, mpPaymentID string, at time.Time) error {
	set := bson.M{"last_payment_sync_at": at}
	if mpStatus != "" {
		set["mp_status"] = mpStatus
	}
	if mpPaymentID != "" {
		set["mp_payment_id"] = mpPaymentID
	}
	_, err := r.collection.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": set})
	return err
}

func (r *BookingRepository) ClaimConfirmationEmail(ctx context.Context, id primitive.ObjectID, at time.Time) (bool, error) {
	res, err := r.collection.UpdateOne(ctx,
		bson.M{"_id": id, "confirmation_email_sent_at": bson.M{"$exists": false}},
		bson.M{"$set": bson.M{"confirmation_email_sent_at": at}},
	)
	if err != nil {
		return false, err
	}
	return res.MatchedCount == 1, nil
}

func (r *BookingRepository) ReleaseConfirmationEmail(ctx context.Context, id primitive.ObjectID) error {
	_, err := r.collection.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$unset": bson.M{"confirmation_email_sent_at": ""}})
	return err
}

func (r *BookingRepository) FindPendingMercadoPago(ctx context.Context, createdAfter, createdBefore time.Time, limit int) ([]domain.Booking, error) {
	if limit <= 0 {
		limit = 100
	}
	cursor, err := r.collection.Find(ctx, bson.M{
		"status":         domain.BookingStatusPending,
		"payment_method": "mercadopago",
		"booking_code":   bson.M{"$exists": true, "$ne": ""},
		"created_at":     bson.M{"$gte": createdAfter, "$lte": createdBefore},
	}, options.Find().SetLimit(int64(limit)).SetSort(bson.D{{Key: "created_at", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var out []domain.Booking
	if err := cursor.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}
