package infra

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/hamp/booking-sport/internal/app"
	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// La comisión y el primer mes gratis son cobros de la plataforma: los define el
// superadmin, nunca el centro.

func newSettingsRouter(t *testing.T, repo *mockCenterRepoForOnboardHandler) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	uc := app.NewSportCenterUseCase(repo, nil, nil, nil, nil, nil, nil, nil)
	h := NewSportCenterHandler(uc, &mockAuth0Client{}, &mockMailer{}, nil)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("user_id", "auth0|wizard-admin") })
	r.PATCH("/api/admin/sport-centers/:id/settings", h.UpdateSportCenterSettings)
	r.PATCH("/api/superadmin/sport-centers/:id/settings", h.SuperAdminUpdateSettings)
	r.PUT("/api/superadmin/sport-centers/:id/first-month-free", h.SetCenterFirstMonthFree)
	return r
}

func settingsRepo() (*mockCenterRepoForOnboardHandler, domain.SportCenter) {
	center := wizardCenter()
	repo := &mockCenterRepoForOnboardHandler{
		centersByUser: []domain.SportCenter{center},
		centerByID:    &center,
	}
	return repo, center
}

func TestCenterSettings_CentroNoPuedeFijarComision(t *testing.T) {
	repo, center := settingsRepo()
	r := newSettingsRouter(t, repo)

	w := doRequest(t, r, http.MethodPatch, "/api/admin/sport-centers/"+center.ID.Hex()+"/settings",
		`{"cancellation_hours":12,"marketplace_fee_percent":0}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body=%s", w.Code, w.Body.String())
	}
	if repo.settingsCalls != 1 {
		t.Fatalf("UpdateSettings calls = %d, want 1 (el resto del cuerpo sí se aplica)", repo.settingsCalls)
	}
	if repo.lastMarketplaceFee != nil {
		t.Errorf("marketplace_fee_percent = %d, want nil: el centro no puede fijar la comisión", *repo.lastMarketplaceFee)
	}
}

func TestCenterSettings_CentroNoPuedeEditarOtroCentro(t *testing.T) {
	repo, _ := settingsRepo()
	r := newSettingsRouter(t, repo)

	other := primitive.NewObjectID().Hex()
	w := doRequest(t, r, http.MethodPatch, "/api/admin/sport-centers/"+other+"/settings", `{"cancellation_hours":1}`)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403. body=%s", w.Code, w.Body.String())
	}
	if repo.settingsCalls != 0 {
		t.Errorf("UpdateSettings calls = %d, want 0", repo.settingsCalls)
	}
}

func TestCenterSettings_SinCentro_Retorna404(t *testing.T) {
	r := newSettingsRouter(t, &mockCenterRepoForOnboardHandler{})
	w := doRequest(t, r, http.MethodPatch, "/api/admin/sport-centers/"+primitive.NewObjectID().Hex()+"/settings", `{}`)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestCenterSettings_IdInvalido_Retorna400(t *testing.T) {
	repo, _ := settingsRepo()
	r := newSettingsRouter(t, repo)
	w := doRequest(t, r, http.MethodPatch, "/api/admin/sport-centers/no-es-un-id/settings", `{}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestSuperadminSettings_PuedeFijarComision(t *testing.T) {
	repo, center := settingsRepo()
	r := newSettingsRouter(t, repo)

	w := doRequest(t, r, http.MethodPatch, "/api/superadmin/sport-centers/"+center.ID.Hex()+"/settings",
		`{"marketplace_fee_percent":6}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body=%s", w.Code, w.Body.String())
	}
	if repo.lastMarketplaceFee == nil || *repo.lastMarketplaceFee != 6 {
		t.Errorf("marketplace_fee_percent = %v, want 6", repo.lastMarketplaceFee)
	}
	if repo.lastSettingsID != center.ID {
		t.Errorf("id = %s, want %s", repo.lastSettingsID.Hex(), center.ID.Hex())
	}
}

func TestSuperadminFirstMonthFree_ActivaYDesactiva(t *testing.T) {
	repo, center := settingsRepo()
	r := newSettingsRouter(t, repo)
	path := "/api/superadmin/sport-centers/" + center.ID.Hex() + "/first-month-free"

	if w := doRequest(t, r, http.MethodPut, path, `{"enabled":true}`); w.Code != http.StatusOK {
		t.Fatalf("activar: status = %d, want 200. body=%s", w.Code, w.Body.String())
	}
	if repo.lastUpdated == nil || !repo.lastUpdated.FirstMonthFree {
		t.Fatal("first_month_free debe quedar activo")
	}

	if w := doRequest(t, r, http.MethodPut, path, `{"enabled":false}`); w.Code != http.StatusOK {
		t.Fatalf("desactivar: status = %d, want 200", w.Code)
	}
	if repo.lastUpdated.FirstMonthFree {
		t.Error("first_month_free debe quedar desactivado")
	}
}

func TestSuperadminFirstMonthFree_ExigeEnabled(t *testing.T) {
	repo, center := settingsRepo()
	r := newSettingsRouter(t, repo)
	path := "/api/superadmin/sport-centers/" + center.ID.Hex() + "/first-month-free"

	for _, payload := range []string{`{}`, `{"enabled":"si"}`, ``} {
		if w := doRequest(t, r, http.MethodPut, path, payload); w.Code != http.StatusBadRequest {
			t.Errorf("payload %q: status = %d, want 400", payload, w.Code)
		}
	}
	if repo.lastUpdated != nil {
		t.Error("no debe actualizar con un cuerpo inválido")
	}
}

func TestSuperadminFirstMonthFree_CentroInexistente_Retorna404(t *testing.T) {
	r := newSettingsRouter(t, &mockCenterRepoForOnboardHandler{})
	w := doRequest(t, r, http.MethodPut, "/api/superadmin/sport-centers/"+primitive.NewObjectID().Hex()+"/first-month-free", `{"enabled":true}`)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}
