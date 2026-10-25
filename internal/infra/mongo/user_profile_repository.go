package mongo

import (
	"context"
	"errors"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type UserProfileRepository struct {
	collection *mongo.Collection
}

func NewUserProfileRepository(db *mongo.Database) *UserProfileRepository {
	coll := db.Collection("user_profiles")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = coll.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "auth0_id", Value: 1}},
		Options: options.Index().SetUnique(true),
	})
	return &UserProfileRepository{collection: coll}
}

func (r *UserProfileRepository) FindByAuth0ID(ctx context.Context, auth0ID string) (*domain.UserProfile, error) {
	var profile domain.UserProfile
	err := r.collection.FindOne(ctx, bson.M{"auth0_id": auth0ID}).Decode(&profile)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &profile, nil
}

func (r *UserProfileRepository) UpsertPhone(ctx context.Context, auth0ID, phone string) error {
	now := time.Now()
	update := bson.M{
		"$set":         bson.M{"phone": phone, "updated_at": now},
		"$setOnInsert": bson.M{"auth0_id": auth0ID, "created_at": now},
	}
	_, err := r.collection.UpdateOne(ctx, bson.M{"auth0_id": auth0ID}, update, options.Update().SetUpsert(true))
	return err
}
