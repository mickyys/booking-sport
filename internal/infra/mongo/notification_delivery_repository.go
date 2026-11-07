package mongo

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const notificationDeliveryTTL = 30 * 24 * time.Hour

// NotificationDeliveryRepository registra qué avisos ya se enviaron.
//
// La clave es (dedupe_key, channel): los webhooks de MercadoPago y el job de
// recordatorios se reintentan, y sin esto el usuario recibiría la misma push
// varias veces.
type NotificationDeliveryRepository struct {
	collection *mongo.Collection
}

func NewNotificationDeliveryRepository(db *mongo.Database) *NotificationDeliveryRepository {
	coll := db.Collection("notification_deliveries")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = coll.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "dedupe_key", Value: 1}, {Key: "channel", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys:    bson.D{{Key: "created_at", Value: 1}},
			Options: options.Index().SetExpireAfterSeconds(int32(notificationDeliveryTTL.Seconds())),
		},
	})
	return &NotificationDeliveryRepository{collection: coll}
}

// Claim reserva el envío. Devuelve true solo la primera vez; si el índice
// único todavía no existe, un duplicado puede pasar: es best-effort.
func (r *NotificationDeliveryRepository) Claim(ctx context.Context, dedupeKey, channel string) (bool, error) {
	_, err := r.collection.InsertOne(ctx, bson.M{
		"dedupe_key": dedupeKey,
		"channel":    channel,
		"created_at": time.Now(),
	})
	if mongo.IsDuplicateKeyError(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
