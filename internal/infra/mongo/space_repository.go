package mongo

import (
	"context"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	mongodb "go.mongodb.org/mongo-driver/mongo"
)

type SpaceRepository struct {
	db         *mongodb.Database
	collection *mongodb.Collection
}

func NewSpaceRepository(db *mongodb.Database) *SpaceRepository {
	return &SpaceRepository{
		db:         db,
		collection: db.Collection("spaces"),
	}
}

func (r *SpaceRepository) Create(ctx context.Context, space *domain.Space) error {
	if space.ID.IsZero() {
		space.ID = primitive.NewObjectID()
	}
	if space.Enabled == false {
		space.Enabled = true
	}
	space.CreatedAt = time.Now()
	space.UpdatedAt = time.Now()
	if _, err := r.collection.InsertOne(ctx, space); err != nil {
		return err
	}

	// Incrementar contador de canchas en el centro deportivo
	if space.Category == domain.SpaceCategoryCancha {
		_, err := r.db.Collection("sport_centers").UpdateOne(
			ctx,
			bson.M{"_id": space.SportCenterID},
			bson.M{"$inc": bson.M{"courts_count": 1}},
		)
		return err
	}
	return nil
}

func (r *SpaceRepository) FindByID(ctx context.Context, id primitive.ObjectID) (*domain.Space, error) {
	var space domain.Space
	err := r.collection.FindOne(ctx, bson.M{"_id": id}).Decode(&space)
	if err != nil {
		return nil, err
	}
	return &space, nil
}

func (r *SpaceRepository) FindByCenterID(ctx context.Context, centerID primitive.ObjectID) ([]domain.Space, error) {
	cursor, err := r.collection.Find(ctx, bson.M{"sport_center_id": centerID})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var spaces []domain.Space
	if err = cursor.All(ctx, &spaces); err != nil {
		return nil, err
	}
	if spaces == nil {
		spaces = []domain.Space{}
	}
	return spaces, nil
}

func (r *SpaceRepository) FindByCenterAndCategory(ctx context.Context, centerID primitive.ObjectID, category domain.SpaceCategory) ([]domain.Space, error) {
	filter := bson.M{"sport_center_id": centerID}
	if category.IsFacility() {
		filter["category"] = bson.M{"$in": bson.A{
			domain.SpaceCategoryQuincho,
			domain.SpaceCategorySede,
			domain.SpaceCategoryPiscina,
		}}
	} else {
		filter["category"] = category
	}

	cursor, err := r.collection.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var spaces []domain.Space
	if err = cursor.All(ctx, &spaces); err != nil {
		return nil, err
	}
	if spaces == nil {
		spaces = []domain.Space{}
	}
	return spaces, nil
}

func (r *SpaceRepository) Update(ctx context.Context, space *domain.Space) error {
	space.UpdatedAt = time.Now()
	_, err := r.collection.ReplaceOne(ctx, bson.M{"_id": space.ID}, space)
	return err
}

func (r *SpaceRepository) Delete(ctx context.Context, id primitive.ObjectID) error {
	space, err := r.FindByID(ctx, id)
	if err != nil {
		return err
	}

	if _, err := r.collection.DeleteOne(ctx, bson.M{"_id": id}); err != nil {
		return err
	}

	// Decrementar contador de canchas en el centro deportivo
	if space.Category == domain.SpaceCategoryCancha {
		_, err = r.db.Collection("sport_centers").UpdateOne(
			ctx,
			bson.M{"_id": space.SportCenterID},
			bson.M{"$inc": bson.M{"courts_count": -1}},
		)
	}
	return err
}

func (r *SpaceRepository) UpdateScheduleSlot(ctx context.Context, id primitive.ObjectID, slot domain.CourtSchedule) error {
	filter := bson.M{
		"_id":           id,
		"schedule.hour": slot.Hour,
	}
	update := bson.M{
		"$set": bson.M{
			"schedule.$": slot,
			"updated_at": time.Now(),
		},
	}

	result, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return err
	}

	if result.MatchedCount == 0 {
		filter = bson.M{"_id": id}
		update = bson.M{
			"$push": bson.M{"schedule": slot},
			"$set":  bson.M{"updated_at": time.Now()},
		}
		_, err = r.collection.UpdateOne(ctx, filter, update)
		return err
	}

	return nil
}

func (r *SpaceRepository) SyncPartialPaymentSlots(ctx context.Context, centerID primitive.ObjectID, partialPaymentEnabled bool) (int64, error) {
	result, err := r.collection.UpdateMany(
		ctx,
		bson.M{
			"sport_center_id":                  centerID,
			"category":                         domain.SpaceCategoryCancha,
			"schedule.partial_payment_enabled": nil,
		},
		bson.M{
			"$set": bson.M{
				"schedule.$[].partial_payment_enabled": partialPaymentEnabled,
				"updated_at":                           time.Now(),
			},
		},
	)
	if err != nil {
		return 0, err
	}
	return result.ModifiedCount, nil
}
