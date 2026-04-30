package mongo

import (
	"context"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type UserDeviceRepository struct {
	collection *mongo.Collection
}

func NewUserDeviceRepository(db *mongo.Database) *UserDeviceRepository {
	return &UserDeviceRepository{
		collection: db.Collection("user_devices"),
	}
}

func (r *UserDeviceRepository) Upsert(ctx context.Context, device *domain.UserDevice) error {
	filter := bson.M{
		"user_id":   device.UserID,
		"fcm_token": device.FCMToken,
	}

	update := bson.M{
		"$set": bson.M{
			"platform":         device.Platform,
			"sport_center_id":  device.SportCenterID,
			"device_name":      device.DeviceName,
			"os_version":       device.OSVersion,
			"last_activity_at": time.Now(),
			"updated_at":       time.Now(),
		},
		"$setOnInsert": bson.M{
			"created_at": time.Now(),
		},
	}

	opts := options.Update().SetUpsert(true)
	_, err := r.collection.UpdateOne(ctx, filter, update, opts)
	return err
}

func (r *UserDeviceRepository) FindByUserID(ctx context.Context, userID string) ([]domain.UserDevice, error) {
	cursor, err := r.collection.Find(ctx, bson.M{"user_id": userID})
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

func (r *UserDeviceRepository) DeleteByToken(ctx context.Context, token string) error {
	_, err := r.collection.DeleteOne(ctx, bson.M{"fcm_token": token})
	return err
}

func (r *UserDeviceRepository) UpdateLastActivity(ctx context.Context, userID string, token string) error {
	filter := bson.M{
		"user_id":   userID,
		"fcm_token": token,
	}
	update := bson.M{
		"$set": bson.M{
			"last_activity_at": time.Now(),
		},
	}
	_, err := r.collection.UpdateOne(ctx, filter, update)
	return err
}

func (r *UserDeviceRepository) FindBySportCenterID(ctx context.Context, centerID string) ([]domain.UserDevice, error) {
	cursor, err := r.collection.Find(ctx, bson.M{"sport_center_id": centerID})
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
