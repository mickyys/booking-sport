package app

import (
	"context"
	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type RecurringRuleRepository interface {
	Create(ctx context.Context, rule *domain.RecurringRule) error
	FindByCenter(ctx context.Context, centerIDs []primitive.ObjectID) ([]domain.RecurringRule, error)
	FindByID(ctx context.Context, id primitive.ObjectID) (*domain.RecurringRule, error)
	Update(ctx context.Context, rule *domain.RecurringRule) error
	Delete(ctx context.Context, id primitive.ObjectID) error
	FindConflicts(ctx context.Context, courtID primitive.ObjectID, dayOfWeek int, hour int) ([]domain.RecurringRule, error)
}
