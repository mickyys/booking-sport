package mongo

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// JobLockRepository implementa un lock distribuido de un solo documento para
// jobs idempotentes. El documento expira por TTL y además se compara
// explícitamente la fecha de expiración para tolerar el retraso del monitor
// TTL de MongoDB (que corre aproximadamente cada 60 segundos).
type JobLockRepository struct {
	collection *mongo.Collection
}

func NewJobLockRepository(db *mongo.Database) *JobLockRepository {
	return &JobLockRepository{collection: db.Collection("job_locks")}
}

// TryAcquire intenta tomar el lock `name` para `owner` durante `ttl`. Devuelve
// true únicamente si el lock quedó en manos de `owner`.
func (r *JobLockRepository) TryAcquire(ctx context.Context, name, owner string, ttl time.Duration, now time.Time) (bool, error) {
	filter := bson.M{
		"_id": name,
		"$or": []bson.M{
			{"expires_at": bson.M{"$lt": now}},
			{"owner": owner},
			{"expires_at": bson.M{"$exists": false}},
		},
	}
	update := bson.M{"$set": bson.M{
		"owner":      owner,
		"expires_at": now.Add(ttl),
		"updated_at": now,
	}}
	res, err := r.collection.UpdateOne(ctx, filter, update, options.Update().SetUpsert(true))
	if err != nil {
		// Si el documento existe con otro owner vigente, el upsert intenta
		// insertar un _id duplicado. Ese caso significa "lock ocupado".
		if mongo.IsDuplicateKeyError(err) {
			return false, nil
		}
		return false, err
	}
	if res.UpsertedCount > 0 {
		return true, nil
	}
	return res.MatchedCount > 0, nil
}

// Release libera el lock solo si pertenece a `owner`.
func (r *JobLockRepository) Release(ctx context.Context, name, owner string) error {
	_, err := r.collection.DeleteOne(ctx, bson.M{"_id": name, "owner": owner})
	return err
}
