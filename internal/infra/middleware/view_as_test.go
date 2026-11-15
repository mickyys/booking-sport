package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type fakeCenters struct{ center *domain.SportCenter }

func (f fakeCenters) FindByID(_ context.Context, id primitive.ObjectID) (*domain.SportCenter, error) {
	if f.center != nil && f.center.ID == id {
		return f.center, nil
	}
	return nil, nil
}

func TestSuperAdminViewAs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	center := &domain.SportCenter{ID: primitive.NewObjectID(), Users: []string{"auth0|owner"}}
	other := primitive.NewObjectID().Hex()

	cases := []struct {
		name     string
		roles    []string
		method   string
		header   string
		want     int
		wantUser string
	}{
		{"sin header no cambia usuario", []string{"administrator"}, "GET", "", 200, "auth0|me"},
		{"superadmin lee el centro", []string{"superadmin"}, "GET", center.ID.Hex(), 200, "auth0|owner"},
		{"superadmin no escribe", []string{"superadmin"}, "POST", center.ID.Hex(), 403, ""},
		{"admin con header recibe 403", []string{"administrator"}, "GET", center.ID.Hex(), 403, ""},
		{"centro inexistente", []string{"superadmin"}, "GET", other, 404, ""},
		{"id inválido", []string{"superadmin"}, "GET", "xyz", 400, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := gin.New()
			r.Use(func(c *gin.Context) {
				c.Set("user_id", "auth0|me")
				c.Set("user_roles", tc.roles)
			})
			r.Use(SuperAdminViewAs(nil, fakeCenters{center}, nil))
			var got string
			h := func(c *gin.Context) { got = c.GetString("user_id"); c.Status(http.StatusOK) }
			r.GET("/api/admin/x", h)
			r.POST("/api/admin/x", h)
			req := httptest.NewRequest(tc.method, "/api/admin/x", nil)
			if tc.header != "" {
				req.Header.Set(ViewCenterHeader, tc.header)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Fatalf("got %d want %d", w.Code, tc.want)
			}
			if tc.want == 200 && got != tc.wantUser {
				t.Fatalf("user_id %q want %q", got, tc.wantUser)
			}
		})
	}
}
