package mongo

import (
	"context"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const inboxTTL = 90 * 24 * time.Hour

type InboxRepository struct {
	collection *mongo.Collection
}

func NewInboxRepository(db *mongo.Database) *InboxRepository {
	coll := db.Collection("inbox_messages")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = coll.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "user_id", Value: 1}, {Key: "_id", Value: -1}}},
		{
			Keys: bson.D{{Key: "user_id", Value: 1}, {Key: "dedupe_key", Value: 1}},
			Options: options.Index().SetUnique(true).
				SetPartialFilterExpression(bson.M{"dedupe_key": bson.M{"$type": "string"}}),
		},
		{
			Keys:    bson.D{{Key: "created_at", Value: 1}},
			Options: options.Index().SetExpireAfterSeconds(int32(inboxTTL.Seconds())),
		},
	})
	return &InboxRepository{collection: coll}
}

func (r *InboxRepository) Insert(ctx context.Context, msg *domain.InboxMessage) error {
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now()
	}
	_, err := r.collection.InsertOne(ctx, msg)
	if mongo.IsDuplicateKeyError(err) {
		return nil
	}
	return err
}

func (r *InboxRepository) List(ctx context.Context, userID, before string, limit int) ([]domain.InboxMessage, error) {
	filter := bson.M{"user_id": userID}
	if before != "" {
		if oid, err := primitive.ObjectIDFromHex(before); err == nil {
			filter["_id"] = bson.M{"$lt": oid}
		}
	}
	cur, err := r.collection.Find(ctx, filter,
		options.Find().SetSort(bson.D{{Key: "_id", Value: -1}}).SetLimit(int64(limit)))
	if err != nil {
		return nil, err
	}
	var out []domain.InboxMessage
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *InboxRepository) CountUnread(ctx context.Context, userID string) (int64, error) {
	return r.collection.CountDocuments(ctx, bson.M{"user_id": userID, "read_at": bson.M{"$exists": false}})
}

func (r *InboxRepository) MarkRead(ctx context.Context, userID, id string) (bool, error) {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return false, nil
	}
	res, err := r.collection.UpdateOne(ctx,
		bson.M{"_id": oid, "user_id": userID},
		bson.M{"$min": bson.M{"read_at": time.Now()}})
	if err != nil {
		return false, err
	}
	return res.MatchedCount > 0, nil
}

func (r *InboxRepository) MarkAllRead(ctx context.Context, userID string) error {
	_, err := r.collection.UpdateMany(ctx,
		bson.M{"user_id": userID, "read_at": bson.M{"$exists": false}},
		bson.M{"$set": bson.M{"read_at": time.Now()}})
	return err
}

func (r *InboxRepository) Delete(ctx context.Context, userID, id string) (bool, error) {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return false, nil
	}
	res, err := r.collection.DeleteOne(ctx, bson.M{"_id": oid, "user_id": userID})
	if err != nil {
		return false, err
	}
	return res.DeletedCount > 0, nil
}
