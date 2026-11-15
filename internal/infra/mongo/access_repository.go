package mongo

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	mongodb "go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// CenterAccessRepository guarda las solicitudes, códigos y tokens del acceso
// privado a centros deportivos.
type CenterAccessRepository struct {
	requests      *mongodb.Collection
	verifications *mongodb.Collection
	tokens        *mongodb.Collection
}

func NewCenterAccessRepository(db *mongodb.Database) *CenterAccessRepository {
	return &CenterAccessRepository{
		requests:      db.Collection("center_access_requests"),
		verifications: db.Collection("center_access_verifications"),
		tokens:        db.Collection("center_access_tokens"),
	}
}

func (r *CenterAccessRepository) CreateRequest(ctx context.Context, request *domain.CenterAccessRequest) error {
	if request.ID.IsZero() {
		request.ID = primitive.NewObjectID()
	}
	if request.Status == "" {
		request.Status = domain.CenterAccessStatusPending
	}
	if request.Source == "" {
		request.Source = domain.CenterAccessSourceForm
	}
	if request.CreatedAt.IsZero() {
		request.CreatedAt = time.Now()
	}
	request.UpdatedAt = time.Now()
	_, err := r.requests.InsertOne(ctx, request)
	if mongodb.IsDuplicateKeyError(err) {
		return domain.NewConflictError("ya existe una solicitud con ese RUT en este centro")
	}
	return err
}

func (r *CenterAccessRepository) UpdateRequest(ctx context.Context, request *domain.CenterAccessRequest) error {
	request.UpdatedAt = time.Now()
	_, err := r.requests.ReplaceOne(ctx, bson.M{"_id": request.ID}, request)
	return err
}

func (r *CenterAccessRepository) FindRequestByCenterAndRUT(ctx context.Context, centerID primitive.ObjectID, rut string) (*domain.CenterAccessRequest, error) {
	var request domain.CenterAccessRequest
	filter := bson.M{"sport_center_id": centerID, "rut": rut}
	if err := r.requests.FindOne(ctx, filter).Decode(&request); err != nil {
		if errors.Is(err, mongodb.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return &request, nil
}

func (r *CenterAccessRepository) FindRequestByID(ctx context.Context, id primitive.ObjectID) (*domain.CenterAccessRequest, error) {
	var request domain.CenterAccessRequest
	if err := r.requests.FindOne(ctx, bson.M{"_id": id}).Decode(&request); err != nil {
		if errors.Is(err, mongodb.ErrNoDocuments) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return &request, nil
}

func (r *CenterAccessRepository) ListRequests(ctx context.Context, filter domain.CenterAccessRequestFilter) ([]domain.CenterAccessRequest, int64, error) {
	query := bson.M{"sport_center_id": filter.CenterID}
	if filter.Status != "" {
		query["status"] = filter.Status
	}
	if term := strings.TrimSpace(filter.Query); term != "" {
		rx := bson.M{"$regex": regexp.QuoteMeta(term), "$options": "i"}
		query["$or"] = bson.A{
			bson.M{"rut": rx},
			bson.M{"email": rx},
			bson.M{"name": rx},
		}
	}
	total, err := r.requests.CountDocuments(ctx, query)
	if err != nil {
		return nil, 0, err
	}
	page, limit := filter.Page, filter.Limit
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	cur, err := r.requests.Find(ctx, query, options.Find().
		SetSort(bson.D{{Key: "created_at", Value: -1}}).
		SetSkip(int64((page-1)*limit)).
		SetLimit(int64(limit)))
	if err != nil {
		return nil, 0, err
	}
	defer cur.Close(ctx)
	out := []domain.CenterAccessRequest{}
	if err := cur.All(ctx, &out); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

func (r *CenterAccessRepository) CountRequestsByStatus(ctx context.Context, centerID primitive.ObjectID) (map[string]int64, error) {
	counts := map[string]int64{}
	for _, status := range []string{
		domain.CenterAccessStatusPending,
		domain.CenterAccessStatusApproved,
		domain.CenterAccessStatusRejected,
		domain.CenterAccessStatusRevoked,
	} {
		n, err := r.requests.CountDocuments(ctx, bson.M{"sport_center_id": centerID, "status": status})
		if err != nil {
			return nil, err
		}
		counts[status] = n
	}
	return counts, nil
}

func (r *CenterAccessRepository) CreateVerification(ctx context.Context, verification *domain.CenterAccessVerification) error {
	if verification.ID.IsZero() {
		verification.ID = primitive.NewObjectID()
	}
	if verification.CreatedAt.IsZero() {
		verification.CreatedAt = time.Now()
	}
	_, err := r.verifications.InsertOne(ctx, verification)
	return err
}

func (r *CenterAccessRepository) FindVerificationByID(ctx context.Context, id primitive.ObjectID) (*domain.CenterAccessVerification, error) {
	var verification domain.CenterAccessVerification
	if err := r.verifications.FindOne(ctx, bson.M{"_id": id}).Decode(&verification); err != nil {
		if errors.Is(err, mongodb.ErrNoDocuments) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return &verification, nil
}

func (r *CenterAccessRepository) IncrementVerificationAttempts(ctx context.Context, id primitive.ObjectID) error {
	_, err := r.verifications.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$inc": bson.M{"attempts": 1}})
	return err
}

func (r *CenterAccessRepository) ConsumeVerification(ctx context.Context, id primitive.ObjectID, at time.Time) error {
	_, err := r.verifications.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": bson.M{"consumed_at": at}})
	return err
}

func (r *CenterAccessRepository) CreateToken(ctx context.Context, token *domain.CenterAccessToken) error {
	if token.ID.IsZero() {
		token.ID = primitive.NewObjectID()
	}
	if token.CreatedAt.IsZero() {
		token.CreatedAt = time.Now()
	}
	_, err := r.tokens.InsertOne(ctx, token)
	return err
}

func (r *CenterAccessRepository) FindTokenByHash(ctx context.Context, hash string) (*domain.CenterAccessToken, error) {
	var token domain.CenterAccessToken
	filter := bson.M{"token_hash": hash, "expires_at": bson.M{"$gt": time.Now()}}
	if err := r.tokens.FindOne(ctx, filter).Decode(&token); err != nil {
		if errors.Is(err, mongodb.ErrNoDocuments) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return &token, nil
}

func (r *CenterAccessRepository) DeleteTokensByRequest(ctx context.Context, requestID primitive.ObjectID) error {
	_, err := r.tokens.DeleteMany(ctx, bson.M{"request_id": requestID})
	return err
}
