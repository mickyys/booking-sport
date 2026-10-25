package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

type mockTenantRepo struct {
	centersByHost map[string]*domain.SportCenter
	centersBySub  map[string]*domain.SportCenter
}

func newMockTenantRepo() *mockTenantRepo {
	center := &domain.SportCenter{
		ID:        primitive.NewObjectID(),
		Slug:      "catolica",
		Name:      "Complejo Deportivo UC",
		Subdomain: "catolica",
		Domains: []string{
			"catolica.reservaloya.cl",
			"www.catolica.reservaloya.cl",
			"www.unioncatolica.cl",
			"unioncatolica.cl",
		},
		Branding: &domain.SportCenterBranding{
			Name:         "Unión Católica",
			LogoURL:      "https://example.com/logo.png",
			PrimaryColor: "#123456",
		},
	}
	return &mockTenantRepo{
		centersByHost: map[string]*domain.SportCenter{
			"catolica.reservaloya.cl": center,
			"www.unioncatolica.cl":    center,
			"unioncatolica.cl":        center,
		},
		centersBySub: map[string]*domain.SportCenter{
			"catolica": center,
		},
	}
}

func (m *mockTenantRepo) FindByHost(ctx context.Context, host string) (*domain.SportCenter, error) {
	if c, ok := m.centersByHost[host]; ok {
		return c, nil
	}
	return nil, mongo.ErrNoDocuments
}

func (m *mockTenantRepo) FindBySubdomain(ctx context.Context, subdomain string) (*domain.SportCenter, error) {
	if c, ok := m.centersBySub[subdomain]; ok {
		return c, nil
	}
	return nil, mongo.ErrNoDocuments
}

func (m *mockTenantRepo) FindByID(ctx context.Context, id primitive.ObjectID) (*domain.SportCenter, error) {
	return nil, nil
}
func (m *mockTenantRepo) FindBySlug(ctx context.Context, slug string) (*domain.SportCenter, error) {
	return nil, nil
}
func (m *mockTenantRepo) FindByNameAndRUT(ctx context.Context, name, rut string) (*domain.SportCenter, error) {
	return nil, nil
}
func (m *mockTenantRepo) FindByAdminEmail(ctx context.Context, email string) (*domain.SportCenter, error) {
	return nil, nil
}
func (m *mockTenantRepo) FindAll(ctx context.Context) ([]domain.SportCenter, error) {
	return nil, nil
}
func (m *mockTenantRepo) FindPaged(ctx context.Context, page, limit int, name, city string, date *time.Time, hour *int, lat, lng, radiusKm *float64, priorityCity string) ([]domain.SportCenter, int64, bool, error) {
	return nil, 0, false, nil
}
func (m *mockTenantRepo) FindByUserID(ctx context.Context, userID string) ([]domain.SportCenter, error) {
	return nil, nil
}
func (m *mockTenantRepo) GetCities(ctx context.Context) ([]string, error) {
	return nil, nil
}
func (m *mockTenantRepo) Update(ctx context.Context, center *domain.SportCenter) error {
	return nil
}
func (m *mockTenantRepo) UpdateSettings(ctx context.Context, id primitive.ObjectID, slug *string, cancellationHours *int, retentionPercent *int, partialPaymentEnabled *bool, partialPaymentPercent *int, imageURL *string, marketplaceFeePercent *int, branding *domain.SportCenterBranding, address *string, city *string, coordinates *domain.Coordinates, paymentMode *string) error {
	return nil
}
func (m *mockTenantRepo) Create(ctx context.Context, center *domain.SportCenter) error {
	return nil
}
func (m *mockTenantRepo) UpdateMercadoPagoConfig(ctx context.Context, id primitive.ObjectID, config *domain.MercadoPagoConfig) error {
	return nil
}

var _ SportCenterRepository = (*mockTenantRepo)(nil)

func TestNormalizeHost(t *testing.T) {
	cases := map[string]string{
		"Catolica.Reservaloya.cl":       "catolica.reservaloya.cl",
		"www.UNIONCATOLICA.CL":          "www.unioncatolica.cl",
		"catolica.reservaloya.cl.":      "catolica.reservaloya.cl",
		"catolica.reservaloya.cl:8080.": "catolica.reservaloya.cl",
		"  orellana.reservaloya.cl ":    "orellana.reservaloya.cl",
	}
	for in, want := range cases {
		if got := NormalizeHost(in); got != want {
			t.Errorf("NormalizeHost(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResolveByHost_ExactMatch(t *testing.T) {
	uc := NewTenantUseCase(newMockTenantRepo())
	tenant, err := uc.ResolveByHost(context.Background(), "www.unioncatolica.cl")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tenant.Subdomain != "catolica" {
		t.Errorf("Subdomain = %q, want catolica", tenant.Subdomain)
	}
	if tenant.Slug != "catolica" {
		t.Errorf("Slug = %q, want catolica", tenant.Slug)
	}
	if tenant.Name != "Unión Católica" {
		t.Errorf("Name = %q, want branding name", tenant.Name)
	}
	if tenant.CanonicalHost != "catolica.reservaloya.cl" {
		t.Errorf("CanonicalHost = %q, want catolica.reservaloya.cl", tenant.CanonicalHost)
	}
	if tenant.Branding == nil || tenant.Branding.PrimaryColor != "#123456" {
		t.Errorf("Branding not propagated correctly: %+v", tenant.Branding)
	}
	if tenant.CenterID.IsZero() {
		t.Error("CenterID should not be zero")
	}
}

func TestResolveByHost_WWWFallback(t *testing.T) {
	uc := NewTenantUseCase(newMockTenantRepo())
	// "www.catolica.reservaloya.cl" no está registrado exacto en el mock,
	// pero "catolica.reservaloya.cl" sí -> debe resolver por fallback www.
	tenant, err := uc.ResolveByHost(context.Background(), "WWW.CATOLICA.RESERVALOYA.CL")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tenant.Slug != "catolica" {
		t.Errorf("Slug = %q, want catolica", tenant.Slug)
	}
}

func TestResolveByHost_CaseInsensitiveWithPort(t *testing.T) {
	uc := NewTenantUseCase(newMockTenantRepo())
	tenant, err := uc.ResolveByHost(context.Background(), "UnionCatolica.CL:443.")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tenant.Slug != "catolica" {
		t.Errorf("Slug = %q, want catolica", tenant.Slug)
	}
}

func TestResolveByHost_NotFound(t *testing.T) {
	uc := NewTenantUseCase(newMockTenantRepo())
	_, err := uc.ResolveByHost(context.Background(), "otro.reservaloya.cl")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, mongo.ErrNoDocuments) && !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected not-found error, got %v", err)
	}
}

func TestResolveBySubdomain(t *testing.T) {
	uc := NewTenantUseCase(newMockTenantRepo())
	tenant, err := uc.ResolveBySubdomain(context.Background(), "catolica")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tenant.Subdomain != "catolica" {
		t.Errorf("Subdomain = %q, want catolica", tenant.Subdomain)
	}

	if _, err := uc.ResolveBySubdomain(context.Background(), "orellana"); err == nil {
		t.Error("expected error for unknown subdomain")
	}
}
