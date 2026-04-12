package mongo

import (
	"context"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	mongodb "go.mongodb.org/mongo-driver/mongo"
)

type RecurringRuleRepository struct {
	collection *mongodb.Collection
}

func NewRecurringRuleRepository(db *mongodb.Database) *RecurringRuleRepository {
	return &RecurringRuleRepository{
		collection: db.Collection("recurring_rules"),
	}
}

func (r *RecurringRuleRepository) Create(ctx context.Context, rule *domain.RecurringRule) error {
	if rule.ID.IsZero() {
		rule.ID = primitive.NewObjectID()
	}
	rule.CreatedAt = time.Now()
	rule.UpdatedAt = time.Now()
	_, err := r.collection.InsertOne(ctx, rule)
	return err
}

func (r *RecurringRuleRepository) FindByCenter(ctx context.Context, centerIDs []primitive.ObjectID) ([]domain.RecurringRule, error) {
	if len(centerIDs) == 0 {
		return []domain.RecurringRule{}, nil
	}
	cursor, err := r.collection.Find(ctx, bson.M{"sport_center_id": bson.M{"$in": centerIDs}})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var rules []domain.RecurringRule
	if err := cursor.All(ctx, &rules); err != nil {
		return nil, err
	}
	return rules, nil
}

func (r *RecurringRuleRepository) FindByID(ctx context.Context, id primitive.ObjectID) (*domain.RecurringRule, error) {
	var rule domain.RecurringRule
	err := r.collection.FindOne(ctx, bson.M{"_id": id}).Decode(&rule)
	if err != nil {
		return nil, err
	}
	return &rule, nil
}

func (r *RecurringRuleRepository) Update(ctx context.Context, rule *domain.RecurringRule) error {
	rule.UpdatedAt = time.Now()
	_, err := r.collection.ReplaceOne(ctx, bson.M{"_id": rule.ID}, rule)
	return err
}

func (r *RecurringRuleRepository) Delete(ctx context.Context, id primitive.ObjectID) error {
	_, err := r.collection.DeleteOne(ctx, bson.M{"_id": id})
	return err
}

func (r *RecurringRuleRepository) FindConflicts(ctx context.Context, courtID primitive.ObjectID, dayOfWeek int, hour int) ([]domain.RecurringRule, error) {
    filter := bson.M{
        "court_id": courtID,
        "day_of_week": dayOfWeek,
        "hour": hour,
    }
    cursor, err := r.collection.Find(ctx, filter)
    if err != nil {
        return nil, err
    }
    defer cursor.Close(ctx)

    var rules []domain.RecurringRule
    if err := cursor.All(ctx, &rules); err != nil {
        return nil, err
    }
    return rules, nil
}
