package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRequireSuperAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		name  string
		roles interface{}
		want  int
	}{
		{"superadmin", []string{"SuperAdmin"}, http.StatusOK},
		{"admin only", []string{"administrator"}, http.StatusForbidden},
		{"no roles", nil, http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := gin.New()
			r.Use(func(c *gin.Context) {
				if tc.roles != nil {
					c.Set("user_roles", tc.roles)
				}
			})
			r.GET("/x", RequireSuperAdmin(nil), func(c *gin.Context) { c.Status(http.StatusOK) })
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest("GET", "/x", nil))
			if w.Code != tc.want {
				t.Fatalf("got %d want %d", w.Code, tc.want)
			}
		})
	}
}
