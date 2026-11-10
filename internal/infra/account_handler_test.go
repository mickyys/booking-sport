package infra

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// fakeCenterFinder implementa centerFinder para los tests del AccountHandler.
type fakeCenterFinder struct {
	centers []domain.SportCenter
	err     error
}

func (f *fakeCenterFinder) FindByUserID(ctx context.Context, userID string) ([]domain.SportCenter, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.centers, nil
}

func newAccountRouter(t *testing.T, auth0 *mockAuth0Client, centers centerFinder, userID string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	h := NewAccountHandler(auth0, centers)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if userID != "" {
			c.Set("user_id", userID)
		}
		c.Next()
	})
	r.DELETE("/users/me/account", h.DeleteMyAccount)
	return r
}

func TestDeleteMyAccount(t *testing.T) {
	t.Run("elimina la cuenta en Auth0 sin tocar Mongo", func(t *testing.T) {
		auth0 := &mockAuth0Client{}
		r := newAccountRouter(t, auth0, &fakeCenterFinder{}, "auth0|cliente")

		w := doRequest(t, r, http.MethodDelete, "/users/me/account", "")

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %s)", w.Code, w.Body.String())
		}
		if auth0.deleteUserCalls != 1 || auth0.lastDeletedUser != "auth0|cliente" {
			t.Errorf("DeleteUser calls=%d user=%q", auth0.deleteUserCalls, auth0.lastDeletedUser)
		}
	})

	t.Run("sin user_id responde 401 y no llama a Auth0", func(t *testing.T) {
		auth0 := &mockAuth0Client{}
		r := newAccountRouter(t, auth0, &fakeCenterFinder{}, "")

		w := doRequest(t, r, http.MethodDelete, "/users/me/account", "")

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", w.Code)
		}
		if auth0.deleteUserCalls != 0 {
			t.Errorf("no debía llamar a Auth0: %d", auth0.deleteUserCalls)
		}
	})

	t.Run("último administrador del centro responde 409", func(t *testing.T) {
		auth0 := &mockAuth0Client{}
		centers := &fakeCenterFinder{centers: []domain.SportCenter{{
			ID:    primitive.NewObjectID(),
			Users: []string{"auth0|admin"},
		}}}
		r := newAccountRouter(t, auth0, centers, "auth0|admin")

		w := doRequest(t, r, http.MethodDelete, "/users/me/account", "")

		if w.Code != http.StatusConflict {
			t.Fatalf("status = %d, want 409 (body %s)", w.Code, w.Body.String())
		}
		if auth0.deleteUserCalls != 0 {
			t.Errorf("no debía llamar a Auth0: %d", auth0.deleteUserCalls)
		}
	})

	t.Run("administrador con colegas puede eliminarse", func(t *testing.T) {
		auth0 := &mockAuth0Client{}
		centers := &fakeCenterFinder{centers: []domain.SportCenter{{
			ID:    primitive.NewObjectID(),
			Users: []string{"auth0|admin", "auth0|colega"},
		}}}
		r := newAccountRouter(t, auth0, centers, "auth0|admin")

		w := doRequest(t, r, http.MethodDelete, "/users/me/account", "")

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %s)", w.Code, w.Body.String())
		}
	})

	t.Run("error de Auth0 responde 502", func(t *testing.T) {
		auth0 := &mockAuth0Client{deleteUserErr: errors.New("auth0 caído")}
		r := newAccountRouter(t, auth0, &fakeCenterFinder{}, "auth0|cliente")

		w := doRequest(t, r, http.MethodDelete, "/users/me/account", "")

		if w.Code != http.StatusBadGateway {
			t.Fatalf("status = %d, want 502", w.Code)
		}
	})

	t.Run("error al leer centros responde 500", func(t *testing.T) {
		auth0 := &mockAuth0Client{}
		centers := &fakeCenterFinder{err: errors.New("mongo caído")}
		r := newAccountRouter(t, auth0, centers, "auth0|cliente")

		w := doRequest(t, r, http.MethodDelete, "/users/me/account", "")

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500", w.Code)
		}
		if auth0.deleteUserCalls != 0 {
			t.Errorf("no debía llamar a Auth0: %d", auth0.deleteUserCalls)
		}
	})
}
