package mongo

import (
	"context"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	mongodb "go.mongodb.org/mongo-driver/mongo"
)

// ScheduleRuleRepository persiste las reglas de horario semanal (schedule_rules).
type ScheduleRuleRepository struct {
	collection *mongodb.Collection
}

func NewScheduleRuleRepository(db *mongodb.Database) *ScheduleRuleRepository {
	return &ScheduleRuleRepository{collection: db.Collection("schedule_rules")}
}

func (r *ScheduleRuleRepository) Create(ctx context.Context, rule *domain.ScheduleRule) error {
	if rule.ID.IsZero() {
		rule.ID = primitive.NewObjectID()
	}
	rule.CreatedAt = time.Now()
	rule.UpdatedAt = time.Now()
	if !rule.Active {
		rule.Active = true
	}
	_, err := r.collection.InsertOne(ctx, rule)
	return err
}

func (r *ScheduleRuleRepository) Update(ctx context.Context, rule *domain.ScheduleRule) error {
	rule.UpdatedAt = time.Now()
	_, err := r.collection.ReplaceOne(ctx, bson.M{"_id": rule.ID}, rule)
	return err
}

func (r *ScheduleRuleRepository) FindByID(ctx context.Context, id primitive.ObjectID) (*domain.ScheduleRule, error) {
	var rule domain.ScheduleRule
	err := r.collection.FindOne(ctx, bson.M{"_id": id}).Decode(&rule)
	if err != nil {
		return nil, err
	}
	return &rule, nil
}

func (r *ScheduleRuleRepository) FindBySpaceID(ctx context.Context, spaceID primitive.ObjectID) ([]domain.ScheduleRule, error) {
	cursor, err := r.collection.Find(ctx, bson.M{"space_id": spaceID, "active": true})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var rules []domain.ScheduleRule
	if err := cursor.All(ctx, &rules); err != nil {
		return nil, err
	}
	if rules == nil {
		rules = []domain.ScheduleRule{}
	}
	return rules, nil
}

func (r *ScheduleRuleRepository) FindBySportCenterID(ctx context.Context, centerID primitive.ObjectID) ([]domain.ScheduleRule, error) {
	cursor, err := r.collection.Find(ctx, bson.M{"sport_center_id": centerID, "active": true})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var rules []domain.ScheduleRule
	if err := cursor.All(ctx, &rules); err != nil {
		return nil, err
	}
	if rules == nil {
		rules = []domain.ScheduleRule{}
	}
	return rules, nil
}

func (r *ScheduleRuleRepository) Delete(ctx context.Context, id primitive.ObjectID) error {
	_, err := r.collection.DeleteOne(ctx, bson.M{"_id": id})
	return err
}

func (r *ScheduleRuleRepository) DeleteBySpaceID(ctx context.Context, spaceID primitive.ObjectID) error {
	_, err := r.collection.DeleteMany(ctx, bson.M{"space_id": spaceID})
	return err
}

// SyncPaymentModeSlots alinea todas las franjas del centro con su modalidad
// comercial: con pago online las franjas exigen pago; con solo reservas el
// pago se realiza en el centro.
func (r *ScheduleRuleRepository) SyncPaymentModeSlots(ctx context.Context, centerID primitive.ObjectID, paymentRequired bool) (int64, error) {
	result, err := r.collection.UpdateMany(
		ctx,
		bson.M{"sport_center_id": centerID},
		bson.M{
			"$set": bson.M{
				"slots.$[].payment_required": paymentRequired,
				"slots.$[].payment_optional": false,
				"updated_at":                 time.Now(),
			},
		},
	)
	if err != nil {
		return 0, err
	}
	return result.ModifiedCount, nil
}

// ScheduleExceptionRepository persiste excepciones de horario (schedule_exceptions).
type ScheduleExceptionRepository struct {
	collection *mongodb.Collection
}

func NewScheduleExceptionRepository(db *mongodb.Database) *ScheduleExceptionRepository {
	return &ScheduleExceptionRepository{collection: db.Collection("schedule_exceptions")}
}

func (r *ScheduleExceptionRepository) Create(ctx context.Context, ex *domain.ScheduleException) error {
	if ex.ID.IsZero() {
		ex.ID = primitive.NewObjectID()
	}
	ex.CreatedAt = time.Now()
	ex.UpdatedAt = time.Now()
	_, err := r.collection.InsertOne(ctx, ex)
	return err
}

func (r *ScheduleExceptionRepository) Update(ctx context.Context, ex *domain.ScheduleException) error {
	ex.UpdatedAt = time.Now()
	_, err := r.collection.ReplaceOne(ctx, bson.M{"_id": ex.ID}, ex)
	return err
}

func (r *ScheduleExceptionRepository) FindByID(ctx context.Context, id primitive.ObjectID) (*domain.ScheduleException, error) {
	var ex domain.ScheduleException
	err := r.collection.FindOne(ctx, bson.M{"_id": id}).Decode(&ex)
	if err != nil {
		return nil, err
	}
	return &ex, nil
}

func (r *ScheduleExceptionRepository) FindBySpaceID(ctx context.Context, spaceID primitive.ObjectID) ([]domain.ScheduleException, error) {
	cursor, err := r.collection.Find(ctx, bson.M{"space_id": spaceID})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var exs []domain.ScheduleException
	if err := cursor.All(ctx, &exs); err != nil {
		return nil, err
	}
	if exs == nil {
		exs = []domain.ScheduleException{}
	}
	return exs, nil
}

// FindBySpaceIDAndDate retorna la excepción de un espacio para una fecha
// normalizada (medianoche de Chile). Devuelve nil si no existe.
func (r *ScheduleExceptionRepository) FindBySpaceIDAndDate(ctx context.Context, spaceID primitive.ObjectID, date time.Time) (*domain.ScheduleException, error) {
	loc := domain.GetSantiagoLocation()
	dateCL := date.In(loc)
	searchDate := time.Date(dateCL.Year(), dateCL.Month(), dateCL.Day(), 0, 0, 0, 0, loc)

	var ex domain.ScheduleException
	err := r.collection.FindOne(ctx, bson.M{"space_id": spaceID, "date": searchDate}).Decode(&ex)
	if err != nil {
		if err == mongodb.ErrNoDocuments {
			return nil, nil
		}
		return nil, err
	}
	return &ex, nil
}

func (r *ScheduleExceptionRepository) FindBySportCenterID(ctx context.Context, centerID primitive.ObjectID) ([]domain.ScheduleException, error) {
	cursor, err := r.collection.Find(ctx, bson.M{"sport_center_id": centerID})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var exs []domain.ScheduleException
	if err := cursor.All(ctx, &exs); err != nil {
		return nil, err
	}
	if exs == nil {
		exs = []domain.ScheduleException{}
	}
	return exs, nil
}

func (r *ScheduleExceptionRepository) Delete(ctx context.Context, id primitive.ObjectID) error {
	_, err := r.collection.DeleteOne(ctx, bson.M{"_id": id})
	return err
}

// BlockRepository persiste bloqueos puntuales de agenda (blocks).
type BlockRepository struct {
	collection *mongodb.Collection
}

func NewBlockRepository(db *mongodb.Database) *BlockRepository {
	return &BlockRepository{collection: db.Collection("blocks")}
}

func (r *BlockRepository) Create(ctx context.Context, block *domain.Block) error {
	if block.ID.IsZero() {
		block.ID = primitive.NewObjectID()
	}
	block.CreatedAt = time.Now()
	block.UpdatedAt = time.Now()
	_, err := r.collection.InsertOne(ctx, block)
	return err
}

func (r *BlockRepository) FindByID(ctx context.Context, id primitive.ObjectID) (*domain.Block, error) {
	var block domain.Block
	err := r.collection.FindOne(ctx, bson.M{"_id": id}).Decode(&block)
	if err != nil {
		return nil, err
	}
	return &block, nil
}

func (r *BlockRepository) FindBySpaceID(ctx context.Context, spaceID primitive.ObjectID) ([]domain.Block, error) {
	cursor, err := r.collection.Find(ctx, bson.M{"space_id": spaceID})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var blocks []domain.Block
	if err := cursor.All(ctx, &blocks); err != nil {
		return nil, err
	}
	if blocks == nil {
		blocks = []domain.Block{}
	}
	return blocks, nil
}

// FindBySpaceIDAndDate retorna los bloqueos de un espacio para una fecha
// normalizada (medianoche de Chile).
func (r *BlockRepository) FindBySpaceIDAndDate(ctx context.Context, spaceID primitive.ObjectID, date time.Time) ([]domain.Block, error) {
	loc := domain.GetSantiagoLocation()
	dateCL := date.In(loc)
	searchDate := time.Date(dateCL.Year(), dateCL.Month(), dateCL.Day(), 0, 0, 0, 0, loc)

	cursor, err := r.collection.Find(ctx, bson.M{"space_id": spaceID, "date": searchDate})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var blocks []domain.Block
	if err := cursor.All(ctx, &blocks); err != nil {
		return nil, err
	}
	if blocks == nil {
		blocks = []domain.Block{}
	}
	return blocks, nil
}

func (r *BlockRepository) FindBySportCenterID(ctx context.Context, centerID primitive.ObjectID) ([]domain.Block, error) {
	cursor, err := r.collection.Find(ctx, bson.M{"sport_center_id": centerID})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var blocks []domain.Block
	if err := cursor.All(ctx, &blocks); err != nil {
		return nil, err
	}
	if blocks == nil {
		blocks = []domain.Block{}
	}
	return blocks, nil
}

func (r *BlockRepository) Delete(ctx context.Context, id primitive.ObjectID) error {
	_, err := r.collection.DeleteOne(ctx, bson.M{"_id": id})
	return err
}
