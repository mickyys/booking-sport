package mongo

import (
	"context"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// UserDeviceRepository guarda los tokens FCM de las dos apps. Cada documento
// es (app, token): un mismo teléfono puede tener la app de clientes y la del
// panel, y cada una recibe solo sus propios avisos.
type UserDeviceRepository struct {
	collection *mongo.Collection
}

func NewUserDeviceRepository(db *mongo.Database) *UserDeviceRepository {
	coll := db.Collection("user_devices")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = coll.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "app", Value: 1}, {Key: "fcm_token", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "app", Value: 1}, {Key: "user_id", Value: 1}}},
		{Keys: bson.D{{Key: "app", Value: 1}, {Key: "sport_center_id", Value: 1}}},
		{
			Keys: bson.D{{Key: "guest_device_id", Value: 1}},
			Options: options.Index().SetPartialFilterExpression(
				bson.M{"guest_device_id": bson.M{"$exists": true, "$ne": ""}},
			),
		},
	})
	return &UserDeviceRepository{collection: coll}
}

// Upsert registra el token. El filtro incluye la app: la misma persona puede
// recibir avisos del panel en un teléfono y de sus reservas como cliente en
// otro (o en el mismo, con documentos distintos).
func (r *UserDeviceRepository) Upsert(ctx context.Context, device *domain.UserDevice) error {
	now := time.Now()
	filter := bson.M{
		"app":       device.App,
		"fcm_token": device.FCMToken,
	}

	update := bson.M{
		"$set": bson.M{
			"user_id":          device.UserID,
			"sport_center_id":  device.SportCenterID,
			"guest_device_id":  device.GuestDeviceID,
			"platform":         device.Platform,
			"device_name":      device.DeviceName,
			"os_version":       device.OSVersion,
			"last_activity_at": now,
			"updated_at":       now,
		},
		"$setOnInsert": bson.M{
			"created_at": now,
		},
	}

	_, err := r.collection.UpdateOne(ctx, filter, update, options.Update().SetUpsert(true))
	return err
}

func (r *UserDeviceRepository) FindByUserIDAndApp(ctx context.Context, userID, app string) ([]domain.UserDevice, error) {
	return r.find(ctx, bson.M{"user_id": userID, "app": app})
}

func (r *UserDeviceRepository) FindBySportCenterIDAndApp(ctx context.Context, centerID, app string) ([]domain.UserDevice, error) {
	return r.find(ctx, bson.M{"sport_center_id": centerID, "app": app})
}

func (r *UserDeviceRepository) FindByGuestDeviceID(ctx context.Context, guestDeviceID string) ([]domain.UserDevice, error) {
	return r.find(ctx, bson.M{"guest_device_id": guestDeviceID, "app": domain.DeviceAppClient})
}

func (r *UserDeviceRepository) find(ctx context.Context, filter bson.M) ([]domain.UserDevice, error) {
	cursor, err := r.collection.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var devices []domain.UserDevice
	if err := cursor.All(ctx, &devices); err != nil {
		return nil, err
	}
	return devices, nil
}

// DeleteByTokenAndApp quita el token de una app (cierre de sesión). Si userID
// no está vacío, solo borra el documento de ese usuario.
func (r *UserDeviceRepository) DeleteByTokenAndApp(ctx context.Context, token, app, userID string) error {
	filter := bson.M{"fcm_token": token, "app": app}
	if userID != "" {
		filter["user_id"] = userID
	}
	_, err := r.collection.DeleteOne(ctx, filter)
	return err
}

// DeleteByTokens poda tokens que FCM declaró inválidos o no registrados.
func (r *UserDeviceRepository) DeleteByTokens(ctx context.Context, tokens []string) error {
	if len(tokens) == 0 {
		return nil
	}
	_, err := r.collection.DeleteMany(ctx, bson.M{"fcm_token": bson.M{"$in": tokens}})
	return err
}
