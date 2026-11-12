package infra

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/hamp/booking-sport/internal/app"
	"github.com/hamp/booking-sport/internal/domain"
	"github.com/hamp/booking-sport/pkg/mercadopago"
)

func newMPCallbackRouter(t *testing.T) (*gin.Engine, *app.MPConnectUseCase) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	repo := &mockCenterRepoForOnboardHandler{centersByUser: []domain.SportCenter{wizardCenter()}}
	uc := app.NewMPConnectUseCase(
		repo,
		mercadopago.NewConnectClient("cid", "secreto-de-prueba", "https://api.test/cb", "https://auth.test", "pk"),
		"secreto-de-prueba",
	)
	h := NewMPConnectHandler(uc)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("user_id", "auth0|wizard-admin") })
	r.GET("/api/admin/mercadopago/connect", h.GetAuthorizationURL)
	r.GET("/api/admin/mercadopago/callback", h.Callback)
	return r, uc
}

func stateFromConnect(t *testing.T, r *gin.Engine, query string) string {
	t.Helper()
	w := doRequest(t, r, http.MethodGet, "/api/admin/mercadopago/connect"+query, "")
	if w.Code != http.StatusOK {
		t.Fatalf("connect status = %d. body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	i := strings.Index(body, "state=")
	if i < 0 {
		t.Fatalf("sin state en %s", body)
	}
	rest := body[i+len("state="):]
	if j := strings.IndexAny(rest, "&\""); j >= 0 {
		rest = rest[:j]
	}
	state, _ := url.QueryUnescape(rest)
	return state
}

func TestMPCallback_OrigenApp_DevuelveDeepLink(t *testing.T) {
	t.Setenv("MOBILE_ADMIN_SCHEME", "")
	r, _ := newMPCallbackRouter(t)
	state := stateFromConnect(t, r, "?source=app")

	// Sin `code` (el usuario canceló en Mercado Pago): vuelve a la app con error.
	req := httptest.NewRequest(http.MethodGet, "/api/admin/mercadopago/callback?state="+url.QueryEscape(state), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (página con deep link). body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "reservaloyaadmin://mp-connect?") || !strings.Contains(body, "status=error") {
		t.Errorf("la página no trae el deep link de error: %s", body)
	}
	if !strings.Contains(w.Header().Get("Content-Type"), "text/html") {
		t.Errorf("content-type = %q, want text/html", w.Header().Get("Content-Type"))
	}
}

func TestMPCallback_OrigenApp_EsquemaConfigurable(t *testing.T) {
	t.Setenv("MOBILE_ADMIN_SCHEME", "miappadmin")
	r, _ := newMPCallbackRouter(t)
	state := stateFromConnect(t, r, "?source=app")

	req := httptest.NewRequest(http.MethodGet, "/api/admin/mercadopago/callback?state="+url.QueryEscape(state), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if !strings.Contains(w.Body.String(), "miappadmin://mp-connect?") {
		t.Errorf("no usa el esquema configurado: %s", w.Body.String())
	}
}

func TestMPCallback_OrigenWeb_RedirigeALaWeb(t *testing.T) {
	t.Setenv("URL_FRONTEND", "https://app.reservaloya.cl")
	r, _ := newMPCallbackRouter(t)
	state := stateFromConnect(t, r, "")

	req := httptest.NewRequest(http.MethodGet, "/api/admin/mercadopago/callback?state="+url.QueryEscape(state), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", w.Code)
	}
	if loc := w.Header().Get("Location"); !strings.HasPrefix(loc, "https://app.reservaloya.cl/admin/onboarding?status=error") {
		t.Errorf("location = %q, want redirect a la web", loc)
	}
}

func TestMPCallback_StateInvalido_NoUsaDeepLink(t *testing.T) {
	t.Setenv("URL_FRONTEND", "https://app.reservaloya.cl")
	r, _ := newMPCallbackRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/mercadopago/callback?state=basura", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Errorf("status = %d, want 302 a la web con state inválido", w.Code)
	}
}
