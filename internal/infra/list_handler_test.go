package infra

// Tests del listado público de centros (GET /api/sport-centers) con foco en
// el filtro por radio:
//	  radius válido (con lat/lng)     -> se propaga al repositorio (km)
//	  radius ausente                 -> radiusKm = nil
//	  radius sin lat/lng             -> 400
//	  radius no numérico             -> 400
//	  radius <= 0 o > 500            -> 400
//	  limit ausente                  -> default 10
//	  repo con fallback de radio     -> used_radius_fallback en la respuesta

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hamp/booking-sport/internal/app"
	"github.com/hamp/booking-sport/internal/domain"
	"github.com/hamp/booking-sport/internal/infra/middleware"
)

type listMockRepo struct {
	*mockCenterRepoForOnboardHandler

	called       bool
	lastPage     int
	lastLimit    int
	lastName     string
	lastCity     string
	lastLat      *float64
	lastLng      *float64
	lastRadiusKm *float64

	centers  []domain.SportCenter
	total    int64
	fallback bool
}

func (m *listMockRepo) FindPaged(_ context.Context, page, limit int, name, city string, _ *time.Time, _ *int, lat, lng, radiusKm *float64, _ string) ([]domain.SportCenter, int64, bool, error) {
	m.called = true
	m.lastPage = page
	m.lastLimit = limit
	m.lastName = name
	m.lastCity = city
	m.lastLat = lat
	m.lastLng = lng
	m.lastRadiusKm = radiusKm
	return m.centers, m.total, m.fallback, nil
}

var _ app.SportCenterRepository = (*listMockRepo)(nil)

func newListRouter(t *testing.T, repo *listMockRepo) *gin.Engine {
	t.Helper()

	gin.SetMode(gin.TestMode)
	uc := app.NewSportCenterUseCase(repo, nil, nil, nil, nil, nil, nil, nil)
	h := NewSportCenterHandler(uc, nil, nil, nil)
	r := gin.New()
	r.Use(middleware.Recovery())
	r.GET("/api/sport-centers", h.List)
	return r
}

func doListRequest(t *testing.T, r *gin.Engine, path string) {
	t.Helper()
	w := doRequest(t, r, http.MethodGet, path, "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET %s: status = %d, want 200. body=%s", path, w.Code, w.Body.String())
	}
}

func TestListHandler_RadiusValido_SePropagaAlRepositorio(t *testing.T) {
	repo := &listMockRepo{mockCenterRepoForOnboardHandler: &mockCenterRepoForOnboardHandler{}}
	r := newListRouter(t, repo)

	doListRequest(t, r, "/api/sport-centers?lat=-33.45&lng=-70.66&radius=20")

	if !repo.called {
		t.Fatal("el repositorio no fue invocado")
	}
	if repo.lastRadiusKm == nil {
		t.Fatal("radiusKm = nil, se esperaba 20")
	}
	if *repo.lastRadiusKm != 20 {
		t.Errorf("radiusKm = %v, want 20", *repo.lastRadiusKm)
	}
	if repo.lastLat == nil || *repo.lastLat != -33.45 {
		t.Errorf("lat = %v, want -33.45", repo.lastLat)
	}
	if repo.lastLng == nil || *repo.lastLng != -70.66 {
		t.Errorf("lng = %v, want -70.66", repo.lastLng)
	}
}

func TestListHandler_SinRadius_RadiusKmNil(t *testing.T) {
	repo := &listMockRepo{mockCenterRepoForOnboardHandler: &mockCenterRepoForOnboardHandler{}}
	r := newListRouter(t, repo)

	doListRequest(t, r, "/api/sport-centers?lat=-33.45&lng=-70.66")

	if !repo.called {
		t.Fatal("el repositorio no fue invocado")
	}
	if repo.lastRadiusKm != nil {
		t.Errorf("radiusKm = %v, want nil", *repo.lastRadiusKm)
	}
}

func TestListHandler_SinLimit_Default10(t *testing.T) {
	repo := &listMockRepo{
		mockCenterRepoForOnboardHandler: &mockCenterRepoForOnboardHandler{},
		centers:                         []domain.SportCenter{},
	}
	r := newListRouter(t, repo)

	doListRequest(t, r, "/api/sport-centers")

	if repo.lastLimit != 10 {
		t.Errorf("limit = %d, want 10 (default)", repo.lastLimit)
	}
	if repo.lastPage != 1 {
		t.Errorf("page = %d, want 1 (default)", repo.lastPage)
	}
}

func TestListHandler_RadiusInvalido_Retorna400(t *testing.T) {
	cases := []struct {
		name string
		path string
	}{
		{"sin lat/lng", "/api/sport-centers?radius=20"},
		{"no numérico", "/api/sport-centers?lat=-33.45&lng=-70.66&radius=abc"},
		{"cero", "/api/sport-centers?lat=-33.45&lng=-70.66&radius=0"},
		{"negativo", "/api/sport-centers?lat=-33.45&lng=-70.66&radius=-5"},
		{"mayor a 500", "/api/sport-centers?lat=-33.45&lng=-70.66&radius=501"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &listMockRepo{mockCenterRepoForOnboardHandler: &mockCenterRepoForOnboardHandler{}}
			r := newListRouter(t, repo)

			w := doRequest(t, r, http.MethodGet, tc.path, "")
			if w.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400. body=%s", w.Code, w.Body.String())
			}
			if repo.called {
				t.Error("el repositorio no debe ser invocado con radius inválido")
			}
		})
	}
}

func TestListHandler_UsedRadiusFallback_SaleEnLaRespuesta(t *testing.T) {
	repo := &listMockRepo{
		mockCenterRepoForOnboardHandler: &mockCenterRepoForOnboardHandler{},
		centers: []domain.SportCenter{
			{Name: "Club Lejano", City: "Valparaíso"},
		},
		total:    1,
		fallback: true,
	}
	r := newListRouter(t, repo)

	w := doRequest(t, r, http.MethodGet, "/api/sport-centers?lat=-33.45&lng=-70.66&radius=1", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body=%s", w.Code, w.Body.String())
	}

	var body struct {
		Data               []domain.SportCenter `json:"data"`
		Total              int64                `json:"total"`
		UsedRadiusFallback bool                 `json:"used_radius_fallback"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("json inválido: %v", err)
	}
	if !body.UsedRadiusFallback {
		t.Error("used_radius_fallback = false, se esperaba true")
	}
	if body.Total != 1 || len(body.Data) != 1 {
		t.Errorf("total/data = %d/%d, want 1/1", body.Total, len(body.Data))
	}
}

func TestListHandler_SinFallback_NoMarcaElFlag(t *testing.T) {
	repo := &listMockRepo{
		mockCenterRepoForOnboardHandler: &mockCenterRepoForOnboardHandler{},
		centers:                         []domain.SportCenter{},
	}
	r := newListRouter(t, repo)

	w := doRequest(t, r, http.MethodGet, "/api/sport-centers?lat=-33.45&lng=-70.66&radius=20", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body=%s", w.Code, w.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("json inválido: %v", err)
	}
	if _, ok := body["used_radius_fallback"]; ok {
		t.Error("used_radius_fallback no debe aparecer cuando no hay fallback")
	}
}
