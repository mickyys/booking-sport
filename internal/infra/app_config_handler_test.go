package infra

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestAppConfigHandlerGet(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cases := map[string]string{
		"":       `{"debug_menu_enabled":false}`,
		"false":  `{"debug_menu_enabled":false}`,
		"basura": `{"debug_menu_enabled":false}`,
		"true":   `{"debug_menu_enabled":true}`,
		" TRUE ": `{"debug_menu_enabled":true}`,
		"1":      `{"debug_menu_enabled":true}`,
	}
	for env, want := range cases {
		t.Setenv("APP_DEBUG_MENU", env)
		r := gin.New()
		r.GET("/api/app-config", NewAppConfigHandler().Get)

		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/app-config", nil))

		if w.Code != http.StatusOK {
			t.Fatalf("env=%q: status %d", env, w.Code)
		}
		if got := w.Body.String(); got != want {
			t.Fatalf("env=%q: got %s want %s", env, got, want)
		}
	}
}
