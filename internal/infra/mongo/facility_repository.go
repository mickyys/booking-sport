package mongo

import (
	"context"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	mongodb "go.mongodb.org/mongo-driver/mongo"
)

var facilityCategories = bson.A{
	domain.SpaceCategoryQuincho,
	domain.SpaceCategorySede,
	domain.SpaceCategoryPiscina,
}

type FacilityRepository struct {
	db         *mongodb.Database
	collection *mongodb.Collection
	inquiries  *mongodb.Collection
}

func NewFacilityRepository(db *mongodb.Database) *FacilityRepository {
	return &FacilityRepository{
		db:         db,
		collection: db.Collection("spaces"),
		inquiries:  db.Collection("facility_inquiries"),
	}
}

func (r *FacilityRepository) Create(ctx context.Context, facility *domain.Facility) error {
	space := domain.SpaceFromFacility(facility)
	if space.ID.IsZero() {
		space.ID = primitive.NewObjectID()
		facility.ID = space.ID
	}
	if space.Enabled == false {
		space.Enabled = true
	}
	space.CreatedAt = time.Now()
	space.UpdatedAt = time.Now()
	_, err := r.collection.InsertOne(ctx, space)
	return err
}

func (r *FacilityRepository) FindByID(ctx context.Context, id primitive.ObjectID) (*domain.Facility, error) {
	var space domain.Space
	err := r.collection.FindOne(ctx, bson.M{"_id": id, "category": bson.M{"$in": facilityCategories}}).Decode(&space)
	if err != nil {
		return nil, err
	}
	return domain.SpaceToFacility(&space), nil
}

func (r *FacilityRepository) FindByCenterID(ctx context.Context, centerID primitive.ObjectID) ([]domain.Facility, error) {
	cursor, err := r.collection.Find(ctx, bson.M{
		"sport_center_id": centerID,
		"category":        bson.M{"$in": facilityCategories},
		"enabled":         true,
	})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var spaces []domain.Space
	if err = cursor.All(ctx, &spaces); err != nil {
		return nil, err
	}

	facilities := make([]domain.Facility, 0, len(spaces))
	for i := range spaces {
		facilities = append(facilities, *domain.SpaceToFacility(&spaces[i]))
	}
	return facilities, nil
}

func (r *FacilityRepository) Update(ctx context.Context, facility *domain.Facility) error {
	space := domain.SpaceFromFacility(facility)
	space.UpdatedAt = time.Now()
	_, err := r.collection.ReplaceOne(ctx, bson.M{"_id": space.ID}, space)
	return err
}

func (r *FacilityRepository) Delete(ctx context.Context, id primitive.ObjectID) error {
	_, err := r.collection.DeleteOne(ctx, bson.M{"_id": id})
	return err
}

func (r *FacilityRepository) CreateInquiry(ctx context.Context, inquiry *domain.FacilityInquiry) error {
	if inquiry.ID.IsZero() {
		inquiry.ID = primitive.NewObjectID()
	}
	inquiry.CreatedAt = time.Now()
	if inquiry.Status == "" {
		inquiry.Status = "pending"
	}
	_, err := r.inquiries.InsertOne(ctx, inquiry)
	return err
}
