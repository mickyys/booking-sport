package app

import (
	"context"
	"errors"
	"testing"

	"github.com/hamp/booking-sport/internal/domain"
	"github.com/hamp/booking-sport/pkg/mercadopago"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type fakeCommercialRepo struct {
	center  *domain.SportCenter
	updates []map[string]interface{}
}

func (f *fakeCommercialRepo) FindByID(ctx context.Context, id primitive.ObjectID) (*domain.SportCenter, error) {
	c := *f.center
	if f.center.Commercial != nil {
		cc := *f.center.Commercial
		c.Commercial = &cc
	}
	return &c, nil
}
func (f *fakeCommercialRepo) FindByUserID(ctx context.Context, userID string) ([]domain.SportCenter, error) {
	return []domain.SportCenter{*f.center}, nil
}
func (f *fakeCommercialRepo) UpdateCommercialFields(ctx context.Context, id primitive.ObjectID, fields map[string]interface{}) error {
	f.updates = append(f.updates, fields)
	if f.center.Commercial == nil {
		f.center.Commercial = &domain.Commercial{}
	}
	c := f.center.Commercial
	for k, v := range fields {
		switch k {
		case "commercial.payment_mode":
			c.PaymentMode = v.(string)
		case "commercial.published":
			b := v.(bool)
			c.Published = &b
		case "commercial.subscription":
			s := v.(domain.Subscription)
			c.Subscription = &s
		case "commercial.subscription.status":
			c.Subscription.Status = v.(string)
		case "commercial.online_payment.commission":
			cm := v.(domain.CommissionConfig)
			if c.OnlinePayment == nil {
				c.OnlinePayment = &domain.OnlinePayment{}
			}
			c.OnlinePayment.Commission = &cm
		}
	}
	return nil
}

type fakeSpaces struct {
	SpaceRepository
	spaces []domain.Space
}

func (f *fakeSpaces) FindByCenterID(ctx context.Context, id primitive.ObjectID) ([]domain.Space, error) {
	return f.spaces, nil
}

type fakeModes struct{ got string }

func (f *fakeModes) SetPaymentMode(ctx context.Context, id primitive.ObjectID, mode string) error {
	f.got = mode
	return nil
}

type fakeProvider struct {
	created mercadopago.SubscriptionInput
	result  *mercadopago.SubscriptionResult
	err     error
}

func (f *fakeProvider) CreateSubscription(ctx context.Context, in mercadopago.SubscriptionInput) (*mercadopago.SubscriptionResult, error) {
	f.created = in
	return &mercadopago.SubscriptionResult{ID: "pre-1", InitPoint: "https://mp/checkout"}, nil
}
func (f *fakeProvider) GetSubscription(ctx context.Context, id string) (*mercadopago.SubscriptionResult, error) {
	return f.result, f.err
}

func newCommercialFixture(center *domain.SportCenter) (*CommercialUseCase, *fakeCommercialRepo, *fakeModes, *fakeProvider) {
	if center.ID.IsZero() {
		center.ID = primitive.NewObjectID()
	}
	repo := &fakeCommercialRepo{center: center}
	modes := &fakeModes{}
	prov := &fakeProvider{}
	spaces := &fakeSpaces{spaces: []domain.Space{{Enabled: true, Schedule: []domain.CourtSchedule{{}}}}}
	uc := NewCommercialUseCase(repo, spaces, nil, modes, prov, CommercialConfig{SubscriptionAmount: 30000, SubscriptionBack: "https://front/back"})
	return uc, repo, modes, prov
}

func readyCenter() *domain.SportCenter {
	return &domain.SportCenter{Name: "C", Address: "A", City: "S", OnboardingStatus: "completed"}
}

func TestCommercialPublishRejectsMissingRequirements(t *testing.T) {
	c := readyCenter()
	c.Commercial = &domain.Commercial{PaymentMode: domain.CommercialModePayAtCenter, Published: boolPtrApp(false)}
	uc, repo, _, _ := newCommercialFixture(c)

	_, err := uc.Publish(context.Background(), c.ID)
	var appErr *domain.AppError
	if !errors.As(err, &appErr) || appErr.StatusCode != 400 {
		t.Fatalf("esperaba 400 sin suscripción, got %v", err)
	}
	if len(repo.updates) != 0 {
		t.Fatal("no debe persistir nada si no cumple los requisitos")
	}

	c.Commercial.Subscription = &domain.Subscription{Status: domain.SubscriptionActive}
	v, err := uc.Publish(context.Background(), c.ID)
	if err != nil || !v.Published {
		t.Fatalf("debía publicar: v=%+v err=%v", v, err)
	}
}

func TestCommercialSetModeKeepsDataAndSetsDefaults(t *testing.T) {
	c := readyCenter()
	c.PaymentMode = "pay_at_center"
	uc, repo, modes, _ := newCommercialFixture(c)

	v, err := uc.SetMode(context.Background(), c.ID, domain.CommercialModePayOnline)
	if err != nil {
		t.Fatal(err)
	}
	if modes.got != PaymentModePayOnline {
		t.Errorf("modalidad legacy = %q, want pay_online", modes.got)
	}
	if v.PaymentMode != domain.CommercialModePayOnline || v.Commission == nil || v.Commission.Source != domain.CommissionSourceDefault {
		t.Errorf("vista inesperada: %+v", v)
	}
	if _, ok := repo.updates[0]["commercial.subscription"]; ok {
		t.Error("no debe tocar la suscripción al pasar a PAY_ONLINE")
	}

	if _, err := uc.SetMode(context.Background(), c.ID, "OTRA"); err == nil {
		t.Error("modalidad inválida debe fallar")
	}
}

func TestCommercialSetCommission(t *testing.T) {
	c := readyCenter()
	c.Commercial = &domain.Commercial{PaymentMode: domain.CommercialModePayOnline}
	uc, _, _, _ := newCommercialFixture(c)
	defer SetCommercialModelEnforced(false)
	SetCommercialModelEnforced(true)

	zero := 0
	v, err := uc.SetCommission(context.Background(), c.ID, &zero)
	if err != nil || v.Commission.Effective != 0 || v.Commission.Source != domain.CommissionSourceCenter {
		t.Fatalf("0%% explícito: v=%+v err=%v", v, err)
	}
	if v, err = uc.SetCommission(context.Background(), c.ID, nil); err != nil || v.Commission.Effective != defaultMarketplaceFeePercent {
		t.Fatalf("reset al default: v=%+v err=%v", v, err)
	}
	bad := 101
	if _, err = uc.SetCommission(context.Background(), c.ID, &bad); err == nil {
		t.Error("101%% debe fallar")
	}

	atCenter := readyCenter()
	atCenter.Commercial = &domain.Commercial{PaymentMode: domain.CommercialModePayAtCenter}
	uc2, _, _, _ := newCommercialFixture(atCenter)
	if _, err = uc2.SetCommission(context.Background(), atCenter.ID, &zero); err == nil {
		t.Error("PAY_AT_CENTER no admite comisión")
	}
}

func TestCommercialSubscriptionFlow(t *testing.T) {
	c := readyCenter()
	c.AdminEmail = "admin@centro.cl"
	c.Commercial = &domain.Commercial{PaymentMode: domain.CommercialModePayAtCenter, Published: boolPtrApp(false)}
	uc, repo, _, prov := newCommercialFixture(c)

	start, err := uc.StartSubscription(context.Background(), c.ID, "")
	if err != nil || start.InitPoint == "" {
		t.Fatalf("start: %+v %v", start, err)
	}
	if prov.created.ExternalRef != c.ID.Hex() || prov.created.PayerEmail != "admin@centro.cl" || prov.created.Amount != 30000 {
		t.Errorf("input a MP inesperado: %+v", prov.created)
	}
	if repo.center.Commercial.Subscription.Status != domain.SubscriptionPending {
		t.Fatal("hasta el webhook la suscripción debe quedar PENDING")
	}

	// Webhook de otra suscripción: se ignora.
	prov.result = &mercadopago.SubscriptionResult{ID: "otra", Status: "authorized", ExternalRef: c.ID.Hex()}
	if err := uc.HandleSubscriptionWebhook(context.Background(), "otra"); !errors.Is(err, ErrSubscriptionIgnored) {
		t.Fatalf("debía ignorar preapproval ajeno, got %v", err)
	}

	prov.result = &mercadopago.SubscriptionResult{ID: "pre-1", Status: "authorized", ExternalRef: c.ID.Hex()}
	if err := uc.HandleSubscriptionWebhook(context.Background(), "pre-1"); err != nil {
		t.Fatal(err)
	}
	if repo.center.Commercial.Subscription.Status != domain.SubscriptionActive {
		t.Fatal("authorized debe activar la suscripción")
	}

	if _, err := uc.StartSubscription(context.Background(), c.ID, ""); err == nil {
		t.Error("no debe iniciar otra suscripción si ya está activa")
	}
}

func TestCommercialSubscriptionRequiresConfiguredPlan(t *testing.T) {
	c := readyCenter()
	c.Commercial = &domain.Commercial{PaymentMode: domain.CommercialModePayAtCenter}
	uc, _, _, _ := newCommercialFixture(c)
	uc.cfg.SubscriptionAmount = 0
	if _, err := uc.StartSubscription(context.Background(), c.ID, "a@b.cl"); err == nil {
		t.Error("sin monto configurado no debe crear suscripción")
	}
}

func boolPtrApp(b bool) *bool { return &b }

type updateOnlyRepo struct {
	SportCenterRepository
	existing *domain.SportCenter
	saved    *domain.SportCenter
}

func (r *updateOnlyRepo) FindByID(ctx context.Context, id primitive.ObjectID) (*domain.SportCenter, error) {
	return r.existing, nil
}
func (r *updateOnlyRepo) Update(ctx context.Context, c *domain.SportCenter) error {
	r.saved = c
	return nil
}

func TestUpdateSportCenterPreservesCommercial(t *testing.T) {
	existing := &domain.SportCenter{ID: primitive.NewObjectID(), Commercial: &domain.Commercial{PaymentMode: domain.CommercialModePayOnline}}
	repo := &updateOnlyRepo{existing: existing}
	uc := &SportCenterUseCase{repo: repo}

	// El cliente intenta enviar su propia comisión.
	incoming := &domain.SportCenter{Name: "Nuevo", Commercial: &domain.Commercial{PaymentMode: domain.CommercialModePayAtCenter}}
	if err := uc.UpdateSportCenter(context.Background(), existing.ID, incoming); err != nil {
		t.Fatal(err)
	}
	if repo.saved.Commercial == nil || repo.saved.Commercial.PaymentMode != domain.CommercialModePayOnline {
		t.Fatalf("commercial debe conservarse del documento existente, got %+v", repo.saved.Commercial)
	}
}
