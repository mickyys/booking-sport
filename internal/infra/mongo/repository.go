package mongo

import (
	"context"

	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func Connect(ctx context.Context, opts *options.ClientOptions) (*mongo.Client, error) {
	client, err := mongo.Connect(ctx, opts)
	if err != nil {
		return nil, err
	}
	err = client.Ping(ctx, nil)
	return client, err
}

type SportCenterRepository struct {
	db         *mongo.Database
	collection *mongo.Collection
}

func NewSportCenterRepository(db *mongo.Database) *SportCenterRepository {
	return &SportCenterRepository{
		db:         db,
		collection: db.Collection("sport_centers"),
	}
}

func (r *SportCenterRepository) Create(ctx context.Context, center *domain.SportCenter) error {
	if center.ID.IsZero() {
		center.ID = primitive.NewObjectID()
	}
	_, err := r.collection.InsertOne(ctx, center)
	return err
}

func (r *SportCenterRepository) FindByID(ctx context.Context, id primitive.ObjectID) (*domain.SportCenter, error) {
	var center domain.SportCenter
	err := r.collection.FindOne(ctx, bson.M{"_id": id}).Decode(&center)
	if err != nil {
		return nil, err
	}
	return &center, nil
}

func (r *SportCenterRepository) FindBySlug(ctx context.Context, slug string) (*domain.SportCenter, error) {
	var center domain.SportCenter
	err := r.collection.FindOne(ctx, bson.M{"slug": slug}).Decode(&center)
	if err != nil {
		return nil, err
	}
	return &center, nil
}

func (r *SportCenterRepository) FindAll(ctx context.Context) ([]domain.SportCenter, error) {
	cursor, err := r.collection.Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var centers []domain.SportCenter
	if err = cursor.All(ctx, &centers); err != nil {
		return nil, err
	}
	return centers, nil
}

func (r *SportCenterRepository) FindPaged(ctx context.Context, page, limit int) ([]domain.SportCenter, int64, error) {
	total, err := r.collection.CountDocuments(ctx, bson.M{})
	if err != nil {
		return nil, 0, err
	}

	skip := int64((page - 1) * limit)
	opts := options.Find().SetLimit(int64(limit)).SetSkip(skip)

	cursor, err := r.collection.Find(ctx, bson.M{}, opts)
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)

	var centers []domain.SportCenter
	if err = cursor.All(ctx, &centers); err != nil {
		return nil, 0, err
	}

	// Asegurar que si no hay resultados, no sea nil sino una lista vacía.
	if centers == nil {
		centers = []domain.SportCenter{}
	}

	return centers, total, nil
}

func (r *SportCenterRepository) FindByUserID(ctx context.Context, userID string) ([]domain.SportCenter, error) {
	// MongoDB find where "users" array contains userID
	cursor, err := r.collection.Find(ctx, bson.M{"users": userID})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var centers []domain.SportCenter
	if err = cursor.All(ctx, &centers); err != nil {
		return nil, err
	}
	
	if centers == nil {
		centers = []domain.SportCenter{}
	}
	
	return centers, nil
}

func (r *SportCenterRepository) Update(ctx context.Context, center *domain.SportCenter) error {
	_, err := r.collection.ReplaceOne(ctx, bson.M{"_id": center.ID}, center)
	return err
}

type CourtRepository struct {
	db         *mongo.Database
	collection *mongo.Collection
}

func NewCourtRepository(db *mongo.Database) *CourtRepository {
	return &CourtRepository{
		db:         db,
		collection: db.Collection("courts"),
	}
}

func (r *CourtRepository) Create(ctx context.Context, court *domain.Court) error {
	if court.ID.IsZero() {
		court.ID = primitive.NewObjectID()
	}
	_, err := r.collection.InsertOne(ctx, court)
	return err
}

func (r *CourtRepository) FindByCenterID(ctx context.Context, centerID primitive.ObjectID) ([]domain.Court, error) {
	cursor, err := r.collection.Find(ctx, bson.M{"sport_center_id": centerID})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var courts []domain.Court
	if err = cursor.All(ctx, &courts); err != nil {
		return nil, err
	}
	return courts, nil
}

func (r *CourtRepository) FindByID(ctx context.Context, id primitive.ObjectID) (*domain.Court, error) {
	var court domain.Court
	err := r.collection.FindOne(ctx, bson.M{"_id": id}).Decode(&court)
	if err != nil {
		return nil, err
	}
	return &court, nil
}

func (r *CourtRepository) Update(ctx context.Context, court *domain.Court) error {
	_, err := r.collection.ReplaceOne(ctx, bson.M{"_id": court.ID}, court)
	return err
}

func (r *CourtRepository) Delete(ctx context.Context, id primitive.ObjectID) error {
	_, err := r.collection.DeleteOne(ctx, bson.M{"_id": id})
	return err
}

func (r *CourtRepository) FindAllPaged(ctx context.Context, page, limit int) ([]domain.Court, int64, error) {
	total, err := r.collection.CountDocuments(ctx, bson.M{})
	if err != nil {
		return nil, 0, err
	}

	skip := int64((page - 1) * limit)
	opts := options.Find().SetLimit(int64(limit)).SetSkip(skip)

	cursor, err := r.collection.Find(ctx, bson.M{}, opts)
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)

	var courts []domain.Court
	if err = cursor.All(ctx, &courts); err != nil {
		return nil, 0, err
	}

	if courts == nil {
		courts = []domain.Court{}
	}

	return courts, total, nil
}

type UserRepository struct {
	collection *mongo.Collection
}

func NewUserRepository(db *mongo.Database) *UserRepository {
	return &UserRepository{
		collection: db.Collection("users"),
	}
}

func (r *UserRepository) Create(ctx context.Context, user *domain.User) error {
	if user.ID.IsZero() {
		user.ID = primitive.NewObjectID()
	}
	_, err := r.collection.InsertOne(ctx, user)
	return err
}

func (r *UserRepository) FindByUsername(ctx context.Context, username string) (*domain.User, error) {
	var user domain.User
	err := r.collection.FindOne(ctx, bson.M{"username": username}).Decode(&user)
	if err != nil {
		return nil, err
	}
	return &user, nil
}
