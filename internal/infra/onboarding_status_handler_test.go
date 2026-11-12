package infra

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/hamp/booking-sport/internal/app"
	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Repos de prueba que devuelven espacios y reglas reales para el estado del
// onboarding (los del wizard devuelven siempre vacío).
type statusSpaceRepo struct {
	mockWizardSpaceRepo
	spaces []domain.Space
}

func (m *statusSpaceRepo) FindByCenterID(ctx context.Context, centerID primitive.ObjectID) ([]domain.Space, error) {
	return m.spaces, nil
}

type statusRuleRepo struct {
	mockWizardRuleRepo
	rules []domain.ScheduleRule
}

func (m *statusRuleRepo) FindBySportCenterID(ctx context.Context, centerID primitive.ObjectID) ([]domain.ScheduleRule, error) {
	return m.rules, nil
}

func newOnboardingStatusRouter(t *testing.T, repo *mockCenterRepoForOnboardHandler, spaces *statusSpaceRepo, rules *statusRuleRepo) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	uc := app.NewSportCenterUseCase(repo, nil, nil, nil, nil, nil, spaces, rules)
	h := NewSportCenterHandler(uc, &mockAuth0Client{}, &mockMailer{}, nil)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("user_id", "auth0|wizard-admin") })
	r.GET("/api/admin/my-sport-center/onboarding/status", h.GetMyOnboardingStatus)
	r.POST("/api/admin/my-sport-center/onboarding/complete", h.CompleteMyOnboarding)
	r.POST("/api/superadmin/sport-centers/:id/onboarding/complete", h.ForceCompleteCenterOnboarding)
	return r
}

// completeCenterWithCourt arma un centro con teléfono, modalidad, una cancha y
// su regla de horario (lo que deja la app mobile).
func completeCenterWithCourt() (domain.SportCenter, *statusSpaceRepo, *statusRuleRepo) {
	center := wizardCenter()
	center.Contact.Phone = "+56 9 1234 5678"
	center.PaymentMode = "pay_at_center"
	space := domain.Space{ID: primitive.NewObjectID(), SportCenterID: center.ID, Category: domain.SpaceCategoryCancha, Name: "Cancha 1", Enabled: true}
	spaces := &statusSpaceRepo{spaces: []domain.Space{space}}
	rules := &statusRuleRepo{rules: []domain.ScheduleRule{{SpaceID: space.ID, SportCenterID: center.ID, Active: true}}}
	return center, spaces, rules
}

func TestOnboardingStatus_DatosCompletos_PuedeCompletar(t *testing.T) {
	center, spaces, rules := completeCenterWithCourt()
	repo := &mockCenterRepoForOnboardHandler{centersByUser: []domain.SportCenter{center}}
	r := newOnboardingStatusRouter(t, repo, spaces, rules)

	w := doRequest(t, r, http.MethodGet, "/api/admin/my-sport-center/onboarding/status", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body=%s", w.Code, w.Body.String())
	}
	var st app.OnboardingStatus
	if err := json.Unmarshal(w.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if !st.CanComplete || len(st.Missing) != 0 {
		t.Errorf("can_complete=%v missing=%v, want true y vacío", st.CanComplete, st.Missing)
	}
	if st.SpacesCount != 1 || st.CourtsCount != 1 || st.CourtsWithSchedule != 1 {
		t.Errorf("spaces=%d courts=%d withSchedule=%d, want 1/1/1", st.SpacesCount, st.CourtsCount, st.CourtsWithSchedule)
	}
	if st.SubscriptionStatus != domain.SubscriptionNotConfigured {
		t.Errorf("subscription = %q, want NOT_CONFIGURED", st.SubscriptionStatus)
	}
	for _, s := range st.Steps {
		if s.Key == app.OnboardingKeyBilling && (s.Done || !s.Optional) {
			t.Errorf("billing done=%v optional=%v, want pendiente y opcional", s.Done, s.Optional)
		}
	}
}

func TestOnboardingStatus_SinEspacios_ListaPendientes(t *testing.T) {
	center := wizardCenter()
	repo := &mockCenterRepoForOnboardHandler{centersByUser: []domain.SportCenter{center}}
	r := newOnboardingStatusRouter(t, repo, &statusSpaceRepo{}, &statusRuleRepo{})

	w := doRequest(t, r, http.MethodGet, "/api/admin/my-sport-center/onboarding/status", "")
	var st app.OnboardingStatus
	if err := json.Unmarshal(w.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if st.CanComplete {
		t.Error("can_complete = true, want false")
	}
	want := map[string]bool{"contact": true, "spaces": true, "schedules": true, "payment_mode": true}
	for _, m := range st.Missing {
		delete(want, m)
	}
	if len(want) != 0 {
		t.Errorf("missing = %v, faltan %v", st.Missing, want)
	}
}

func TestOnboardingStatus_CanchaSinHorario_Pendiente(t *testing.T) {
	center, spaces, _ := completeCenterWithCourt()
	repo := &mockCenterRepoForOnboardHandler{centersByUser: []domain.SportCenter{center}}
	r := newOnboardingStatusRouter(t, repo, spaces, &statusRuleRepo{})

	w := doRequest(t, r, http.MethodGet, "/api/admin/my-sport-center/onboarding/status", "")
	var st app.OnboardingStatus
	_ = json.Unmarshal(w.Body.Bytes(), &st)
	if st.CanComplete {
		t.Error("can_complete = true, want false (cancha sin horario)")
	}
	found := false
	for _, m := range st.Missing {
		if m == "schedules" {
			found = true
		}
	}
	if !found {
		t.Errorf("missing = %v, want incluir schedules", st.Missing)
	}
}

func TestOnboardingStatus_SinCentro_Retorna404(t *testing.T) {
	r := newOnboardingStatusRouter(t, &mockCenterRepoForOnboardHandler{}, &statusSpaceRepo{}, &statusRuleRepo{})
	w := doRequest(t, r, http.MethodGet, "/api/admin/my-sport-center/onboarding/status", "")
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestOnboardingComplete_DatosExistentes_MarcaCompletoSinDuplicar(t *testing.T) {
	center, spaces, rules := completeCenterWithCourt()
	center.SpaceTemplates = []domain.SpaceTemplate{{Tipo: "cancha", Cantidad: 2}}
	repo := &mockCenterRepoForOnboardHandler{centersByUser: []domain.SportCenter{center}}
	r := newOnboardingStatusRouter(t, repo, spaces, rules)

	w := doRequest(t, r, http.MethodPost, "/api/admin/my-sport-center/onboarding/complete", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body=%s", w.Code, w.Body.String())
	}
	if repo.lastUpdated == nil || repo.lastUpdated.OnboardingStatus != "completed" || repo.lastUpdated.OnboardingStep != 5 {
		t.Fatalf("lastUpdated = %+v, want completed/5", repo.lastUpdated)
	}
	if repo.lastUpdated.SpaceTemplates != nil {
		t.Error("el borrador de plantillas debe limpiarse")
	}
	if len(spaces.created) != 0 || len(rules.created) != 0 {
		t.Errorf("creó %d espacios y %d reglas, want 0 (no debe duplicar)", len(spaces.created), len(rules.created))
	}
}

func TestOnboardingComplete_Incompleto_Retorna400ConPendientes(t *testing.T) {
	center := wizardCenter()
	repo := &mockCenterRepoForOnboardHandler{centersByUser: []domain.SportCenter{center}}
	r := newOnboardingStatusRouter(t, repo, &statusSpaceRepo{}, &statusRuleRepo{})

	w := doRequest(t, r, http.MethodPost, "/api/admin/my-sport-center/onboarding/complete", "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400. body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Missing []string `json:"missing"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Missing) == 0 {
		t.Error("missing vacío, want lista de pendientes")
	}
	if repo.lastUpdated != nil {
		t.Error("no debe actualizar el centro si faltan datos")
	}
}

func TestOnboardingComplete_YaCompleto_EsIdempotente(t *testing.T) {
	center := wizardCenter()
	center.OnboardingStatus = "completed"
	repo := &mockCenterRepoForOnboardHandler{centersByUser: []domain.SportCenter{center}}
	r := newOnboardingStatusRouter(t, repo, &statusSpaceRepo{}, &statusRuleRepo{})

	w := doRequest(t, r, http.MethodPost, "/api/admin/my-sport-center/onboarding/complete", "")
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (idempotente). body=%s", w.Code, w.Body.String())
	}
}

func TestSuperadminForceComplete_ExigeMotivo(t *testing.T) {
	center := wizardCenter()
	repo := &mockCenterRepoForOnboardHandler{centerByID: &center}
	r := newOnboardingStatusRouter(t, repo, &statusSpaceRepo{}, &statusRuleRepo{})
	path := "/api/superadmin/sport-centers/" + center.ID.Hex() + "/onboarding/complete"

	if w := doRequest(t, r, http.MethodPost, path, `{}`); w.Code != http.StatusBadRequest {
		t.Errorf("sin motivo: status = %d, want 400", w.Code)
	}
	if repo.lastUpdated != nil {
		t.Fatal("no debe actualizar sin motivo")
	}

	w := doRequest(t, r, http.MethodPost, path, `{"reason":"configurado desde la app mobile"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("con motivo: status = %d, want 200. body=%s", w.Code, w.Body.String())
	}
	if repo.lastUpdated == nil || repo.lastUpdated.OnboardingStatus != "completed" {
		t.Errorf("lastUpdated = %+v, want completed", repo.lastUpdated)
	}
}
