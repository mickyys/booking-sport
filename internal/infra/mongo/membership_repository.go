package mongo

import (
	"context"
	"errors"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// MembershipRepository implementa la persistencia de planes, socios y los
// artefactos de verificación por correo.
type MembershipRepository struct {
	plans         *mongo.Collection
	members       *mongo.Collection
	verifications *mongo.Collection
	tokens        *mongo.Collection
}

func NewMembershipRepository(db *mongo.Database) *MembershipRepository {
	return &MembershipRepository{
		plans:         db.Collection("membership_plans"),
		members:       db.Collection("members"),
		verifications: db.Collection("member_verifications"),
		tokens:        db.Collection("member_tokens"),
	}
}

// --- Planes ---

func (r *MembershipRepository) CreatePlan(ctx context.Context, plan *domain.MembershipPlan) error {
	res, err := r.plans.InsertOne(ctx, plan)
	if err != nil {
		return err
	}
	plan.ID = res.InsertedID.(primitive.ObjectID)
	return nil
}

func (r *MembershipRepository) UpdatePlan(ctx context.Context, plan *domain.MembershipPlan) error {
	_, err := r.plans.UpdateOne(ctx, bson.M{"_id": plan.ID}, bson.M{"$set": plan})
	return err
}

func (r *MembershipRepository) FindPlanByID(ctx context.Context, id primitive.ObjectID) (*domain.MembershipPlan, error) {
	var plan domain.MembershipPlan
	if err := r.plans.FindOne(ctx, bson.M{"_id": id}).Decode(&plan); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return &plan, nil
}

func (r *MembershipRepository) FindPlansByCenter(ctx context.Context, centerID primitive.ObjectID) ([]domain.MembershipPlan, error) {
	cursor, err := r.plans.Find(ctx, bson.M{"sport_center_id": centerID}, options.Find().SetSort(bson.D{{Key: "created_at", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	plans := []domain.MembershipPlan{}
	if err := cursor.All(ctx, &plans); err != nil {
		return nil, err
	}
	return plans, nil
}

// DeletePlan desactiva el plan en lugar de borrarlo para no romper socios
// existentes que lo referencian.
func (r *MembershipRepository) DeletePlan(ctx context.Context, id primitive.ObjectID) error {
	_, err := r.plans.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": bson.M{"active": false, "updated_at": time.Now()}})
	return err
}

// --- Socios ---

func (r *MembershipRepository) CreateMember(ctx context.Context, member *domain.Member) error {
	res, err := r.members.InsertOne(ctx, member)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return domain.NewConflictError("ya existe un socio con ese documento en este centro")
		}
		return err
	}
	member.ID = res.InsertedID.(primitive.ObjectID)
	return nil
}

func (r *MembershipRepository) UpdateMember(ctx context.Context, member *domain.Member) error {
	member.UpdatedAt = time.Now()
	_, err := r.members.UpdateOne(ctx, bson.M{"_id": member.ID}, bson.M{"$set": member})
	if err != nil && mongo.IsDuplicateKeyError(err) {
		return domain.NewConflictError("ya existe un socio con ese documento en este centro")
	}
	return err
}

func (r *MembershipRepository) FindMemberByID(ctx context.Context, id primitive.ObjectID) (*domain.Member, error) {
	var member domain.Member
	if err := r.members.FindOne(ctx, bson.M{"_id": id}).Decode(&member); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return &member, nil
}

func (r *MembershipRepository) FindMemberByDocument(ctx context.Context, centerID primitive.ObjectID, document string) (*domain.Member, error) {
	var member domain.Member
	filter := bson.M{"sport_center_id": centerID, "document_number": document}
	if err := r.members.FindOne(ctx, filter).Decode(&member); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return &member, nil
}

func (r *MembershipRepository) FindMembersByCenter(ctx context.Context, centerID primitive.ObjectID) ([]domain.Member, error) {
	cursor, err := r.members.Find(ctx, bson.M{"sport_center_id": centerID}, options.Find().SetSort(bson.D{{Key: "name", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	members := []domain.Member{}
	if err := cursor.All(ctx, &members); err != nil {
		return nil, err
	}
	return members, nil
}

func (r *MembershipRepository) DeleteMember(ctx context.Context, id primitive.ObjectID) error {
	_, err := r.members.DeleteOne(ctx, bson.M{"_id": id})
	return err
}

// --- Verificación por correo ---

func (r *MembershipRepository) CreateVerification(ctx context.Context, verification *domain.MemberVerification) error {
	res, err := r.verifications.InsertOne(ctx, verification)
	if err != nil {
		return err
	}
	verification.ID = res.InsertedID.(primitive.ObjectID)
	return nil
}

func (r *MembershipRepository) FindVerificationByID(ctx context.Context, id primitive.ObjectID) (*domain.MemberVerification, error) {
	var verification domain.MemberVerification
	if err := r.verifications.FindOne(ctx, bson.M{"_id": id}).Decode(&verification); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return &verification, nil
}

func (r *MembershipRepository) IncrementVerificationAttempts(ctx context.Context, id primitive.ObjectID) error {
	_, err := r.verifications.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$inc": bson.M{"attempts": 1}})
	return err
}

func (r *MembershipRepository) ConsumeVerification(ctx context.Context, id primitive.ObjectID, at time.Time) error {
	_, err := r.verifications.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": bson.M{"consumed_at": at}})
	return err
}

// --- Token de socio ---

func (r *MembershipRepository) CreateMemberToken(ctx context.Context, token *domain.MemberToken) error {
	res, err := r.tokens.InsertOne(ctx, token)
	if err != nil {
		return err
	}
	token.ID = res.InsertedID.(primitive.ObjectID)
	return nil
}

func (r *MembershipRepository) FindMemberTokenByHash(ctx context.Context, hash string) (*domain.MemberToken, error) {
	var token domain.MemberToken
	filter := bson.M{"token_hash": hash, "expires_at": bson.M{"$gt": time.Now()}}
	if err := r.tokens.FindOne(ctx, filter).Decode(&token); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return &token, nil
}
