package app

import (
	"context"
	"testing"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type accessCenterRepo struct {
	mockCenterRepoForRecurring
	center *domain.SportCenter
}

func (r *accessCenterRepo) FindByID(_ context.Context, _ primitive.ObjectID) (*domain.SportCenter, error) {
	c := *r.center
	return &c, nil
}

type memAccessRepo struct {
	requests map[primitive.ObjectID]*domain.CenterAccessRequest
	verifs   map[primitive.ObjectID]*domain.CenterAccessVerification
	tokens   map[string]*domain.CenterAccessToken
}

func newMemAccessRepo() *memAccessRepo {
	return &memAccessRepo{
		requests: map[primitive.ObjectID]*domain.CenterAccessRequest{},
		verifs:   map[primitive.ObjectID]*domain.CenterAccessVerification{},
		tokens:   map[string]*domain.CenterAccessToken{},
	}
}

func (r *memAccessRepo) CreateRequest(_ context.Context, q *domain.CenterAccessRequest) error {
	q.ID = primitive.NewObjectID()
	c := *q
	r.requests[q.ID] = &c
	return nil
}
func (r *memAccessRepo) UpdateRequest(_ context.Context, q *domain.CenterAccessRequest) error {
	c := *q
	r.requests[q.ID] = &c
	return nil
}
func (r *memAccessRepo) FindRequestByCenterAndRUT(_ context.Context, centerID primitive.ObjectID, rut string) (*domain.CenterAccessRequest, error) {
	for _, q := range r.requests {
		if q.SportCenterID == centerID && q.RUT == rut {
			c := *q
			return &c, nil
		}
	}
	return nil, nil
}
func (r *memAccessRepo) FindRequestByID(_ context.Context, id primitive.ObjectID) (*domain.CenterAccessRequest, error) {
	if q, ok := r.requests[id]; ok {
		c := *q
		return &c, nil
	}
	return nil, domain.ErrNotFound
}
func (r *memAccessRepo) ListRequests(context.Context, CenterAccessRequestFilter) ([]domain.CenterAccessRequest, int64, error) {
	return nil, 0, nil
}
func (r *memAccessRepo) CountRequestsByStatus(context.Context, primitive.ObjectID) (map[string]int64, error) {
	return nil, nil
}
func (r *memAccessRepo) CreateVerification(_ context.Context, v *domain.CenterAccessVerification) error {
	v.ID = primitive.NewObjectID()
	c := *v
	r.verifs[v.ID] = &c
	return nil
}
func (r *memAccessRepo) FindVerificationByID(_ context.Context, id primitive.ObjectID) (*domain.CenterAccessVerification, error) {
	if v, ok := r.verifs[id]; ok {
		c := *v
		return &c, nil
	}
	return nil, domain.ErrNotFound
}
func (r *memAccessRepo) IncrementVerificationAttempts(_ context.Context, id primitive.ObjectID) error {
	r.verifs[id].Attempts++
	return nil
}
func (r *memAccessRepo) ConsumeVerification(_ context.Context, id primitive.ObjectID, at time.Time) error {
	r.verifs[id].ConsumedAt = &at
	return nil
}
func (r *memAccessRepo) CreateToken(_ context.Context, t *domain.CenterAccessToken) error {
	c := *t
	r.tokens[t.TokenHash] = &c
	return nil
}
func (r *memAccessRepo) FindTokenByHash(_ context.Context, h string) (*domain.CenterAccessToken, error) {
	if t, ok := r.tokens[h]; ok {
		return t, nil
	}
	return nil, domain.ErrNotFound
}
func (r *memAccessRepo) DeleteTokensByRequest(_ context.Context, id primitive.ObjectID) error {
	for h, t := range r.tokens {
		if t.RequestID == id {
			delete(r.tokens, h)
		}
	}
	return nil
}

type accessMailerCapture struct{ codes chan string }

func (m *accessMailerCapture) SendAccessRequestReceived(context.Context, CenterAccessEmail) error {
	return nil
}
func (m *accessMailerCapture) SendAccessRequestApproved(context.Context, CenterAccessDecisionEmail) error {
	return nil
}
func (m *accessMailerCapture) SendAccessRequestRejected(context.Context, CenterAccessDecisionEmail) error {
	return nil
}
func (m *accessMailerCapture) SendAccessVerificationCode(_ context.Context, _, _, _, code string, _ int) error {
	m.codes <- code
	return nil
}

func newAccessFixture(t *testing.T) (*CenterAccessUseCase, *domain.SportCenter, *accessMailerCapture) {
	t.Helper()
	center := &domain.SportCenter{
		ID:        primitive.NewObjectID(),
		Name:      "Club Privado",
		Slug:      "club",
		IsPrivate: true,
		AccessForm: &domain.CenterAccessForm{
			Enabled: true,
			Fields: []domain.CenterAccessField{
				{Key: "rama", Label: "Rama", Type: domain.CenterAccessFieldSelect, Required: true, Options: []string{"A", "B"}},
			},
		},
	}
	mailer := &accessMailerCapture{codes: make(chan string, 4)}
	uc := NewCenterAccessUseCase(newMemAccessRepo(), &accessCenterRepo{center: center}, mailer)
	return uc, center, mailer
}

func TestAccessFullFlow(t *testing.T) {
	uc, center, mailer := newAccessFixture(t)
	ctx := context.Background()

	// Sin token, el centro privado bloquea la reserva.
	if err := uc.EnsureBookingAccess(ctx, center, "", false); err == nil {
		t.Fatal("se esperaba ACCESS_REQUIRED")
	}
	// Admin y centros públicos pasan.
	if err := uc.EnsureBookingAccess(ctx, center, "", true); err != nil {
		t.Fatalf("admin debe pasar: %v", err)
	}

	// Campo obligatorio faltante.
	_, err := uc.SubmitRequest(ctx, center.ID, CenterAccessRequestInput{RUT: "12.345.678-5", Email: "a@b.cl"}, "1.1.1.1")
	if err == nil {
		t.Fatal("se esperaba error por campo obligatorio")
	}
	req, err := uc.SubmitRequest(ctx, center.ID, CenterAccessRequestInput{
		RUT: "12.345.678-5", Email: "A@B.cl", Answers: map[string]string{"rama": "A"},
	}, "1.1.1.1")
	if err != nil {
		t.Fatalf("SubmitRequest: %v", err)
	}
	if req.Status != domain.CenterAccessStatusPending || req.Email != "a@b.cl" {
		t.Fatalf("request = %+v", req)
	}

	// Pendiente: no puede verificar.
	if _, err := uc.RequestVerification(ctx, center.ID, "12345678-5", "2.2.2.2"); err == nil {
		t.Fatal("pendiente no debe verificar")
	}

	// Rechazo requiere motivo.
	if _, err := uc.SetRequestStatus(ctx, center.ID, req.ID, domain.CenterAccessStatusRejected, "", "u", "u@x"); err == nil {
		t.Fatal("rechazo sin motivo debe fallar")
	}
	if _, err := uc.SetRequestStatus(ctx, center.ID, req.ID, domain.CenterAccessStatusApproved, "", "u", "u@x"); err != nil {
		t.Fatalf("approve: %v", err)
	}

	res, err := uc.RequestVerification(ctx, center.ID, "12345678-5", "3.3.3.3")
	if err != nil {
		t.Fatalf("RequestVerification: %v", err)
	}
	code := <-mailer.codes

	if _, err := uc.ConfirmVerification(ctx, res.VerificationID, "000000"); err == nil && code != "000000" {
		t.Fatal("código incorrecto debe fallar")
	}
	conf, err := uc.ConfirmVerification(ctx, res.VerificationID, code)
	if err != nil {
		t.Fatalf("ConfirmVerification: %v", err)
	}
	if err := uc.EnsureBookingAccess(ctx, center, conf.AccessToken, false); err != nil {
		t.Fatalf("token válido debe pasar: %v", err)
	}
	// Token de otro centro no sirve.
	other := *center
	other.ID = primitive.NewObjectID()
	if err := uc.EnsureBookingAccess(ctx, &other, conf.AccessToken, false); err == nil {
		t.Fatal("token de otro centro debe fallar")
	}

	// Revocar invalida el token.
	if _, err := uc.SetRequestStatus(ctx, center.ID, req.ID, domain.CenterAccessStatusRevoked, "baja del club", "u", "u@x"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if err := uc.EnsureBookingAccess(ctx, center, conf.AccessToken, false); err == nil {
		t.Fatal("token revocado debe fallar")
	}
}

func TestAccessFormNormalize(t *testing.T) {
	form, err := normalizeAccessForm(&domain.CenterAccessForm{
		Enabled: true,
		Fields: []domain.CenterAccessField{
			{Key: "rut", Label: "x", Type: "text"},
			{Label: "Lista", Type: domain.CenterAccessFieldSelect, Options: []string{"a", "a", "b"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(form.Fields) != 3 || form.Fields[0].Key != "rut" || !form.Fields[0].Required || form.Fields[1].Key != "email" {
		t.Fatalf("fields = %+v", form.Fields)
	}
	if len(form.Fields[2].Options) != 2 {
		t.Fatalf("options = %v", form.Fields[2].Options)
	}
	if _, err := normalizeAccessForm(&domain.CenterAccessForm{Fields: []domain.CenterAccessField{{Label: "L", Type: "select"}}}); err == nil {
		t.Fatal("select sin opciones debe fallar")
	}
}
