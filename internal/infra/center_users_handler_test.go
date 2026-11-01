package infra

import (
	"context"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/hamp/booking-sport/internal/app"
	"github.com/hamp/booking-sport/internal/domain"
	"github.com/hamp/booking-sport/internal/infra/auth0mgmt"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// mockCenterRepoForUsers resuelve FindByUserID por usuario y FindByID para que
// UpdateSportCenter pueda persistir cambios en center.Users.
type mockCenterRepoForUsers struct {
	mockCenterRepoForOnboardHandler
	byUser map[string][]domain.SportCenter
	center domain.SportCenter
}

func (m *mockCenterRepoForUsers) FindByUserID(ctx context.Context, userID string) ([]domain.SportCenter, error) {
	return m.byUser[userID], nil
}

func (m *mockCenterRepoForUsers) FindByID(ctx context.Context, id primitive.ObjectID) (*domain.SportCenter, error) {
	c := m.center
	return &c, nil
}

const currentAdminID = "auth0|actual"

func newUsersRouter(t *testing.T, repo *mockCenterRepoForUsers, auth0 *mockAuth0Client, mailer app.Mailer) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	uc := app.NewSportCenterUseCase(repo, nil, nil, nil, nil, nil, nil, nil)
	h := NewSportCenterHandler(uc, auth0, mailer, nil)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", currentAdminID)
		c.Next()
	})
	r.POST("/admin/users", h.InviteCenterUser)
	r.DELETE("/admin/users/:userId", h.RemoveCenterUser)
	return r
}

func newUsersRepo(users ...string) *mockCenterRepoForUsers {
	center := domain.SportCenter{ID: primitive.NewObjectID(), Name: "Club Test", Users: users}
	return &mockCenterRepoForUsers{
		center: center,
		byUser: map[string][]domain.SportCenter{currentAdminID: {center}},
	}
}

func TestInviteCenterUser_CuentaNueva_CreaRolYEnviaCorreo(t *testing.T) {
	t.Setenv("AUTH0_ADMIN_ROLE_ID", "rol_test")
	repo := newUsersRepo(currentAdminID)
	auth0 := &mockAuth0Client{hasRole: true}
	mailer := &mockMailer{}
	r := newUsersRouter(t, repo, auth0, mailer)

	w := doRequest(t, r, http.MethodPost, "/admin/users", `{"email":"Nuevo@Club.cl","nombre":"Nuevo"}`)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %s)", w.Code, w.Body.String())
	}
	if auth0.accountCalls != 1 || auth0.lastAccountEmail != "nuevo@club.cl" {
		t.Errorf("CreateAccount calls=%d email=%q", auth0.accountCalls, auth0.lastAccountEmail)
	}
	if auth0.assignRoleCalls != 1 || auth0.lastAssignedRole != "rol_test" || auth0.lastAssignedUser != "auth0|creado" {
		t.Errorf("AssignRole calls=%d role=%q user=%q", auth0.assignRoleCalls, auth0.lastAssignedRole, auth0.lastAssignedUser)
	}
	if auth0.setupTicketCalls != 1 {
		t.Errorf("ticket calls = %d, want 1", auth0.setupTicketCalls)
	}
	if repo.lastUpdated == nil || len(repo.lastUpdated.Users) != 2 || repo.lastUpdated.Users[1] != "auth0|creado" {
		t.Errorf("center.Users no se actualizó: %+v", repo.lastUpdated)
	}
	if mailer.inviteCalls != 1 || mailer.lastInvite.SetupURL == "" || mailer.lastInvite.To != "nuevo@club.cl" {
		t.Errorf("correo = %+v (calls %d)", mailer.lastInvite, mailer.inviteCalls)
	}
	if mailer.welcomeCalls != 0 {
		t.Errorf("no debía enviarse el correo de bienvenida de centro")
	}
}

func TestInviteCenterUser_CuentaExistente_NoCreaCuenta(t *testing.T) {
	t.Setenv("AUTH0_ADMIN_ROLE_ID", "rol_test")
	repo := newUsersRepo(currentAdminID)
	auth0 := &mockAuth0Client{hasRole: true, user: &auth0mgmt.User{ID: "auth0|existente", Email: "e@club.cl"}}
	mailer := &mockMailer{}
	r := newUsersRouter(t, repo, auth0, mailer)

	w := doRequest(t, r, http.MethodPost, "/admin/users", `{"email":"e@club.cl"}`)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %s)", w.Code, w.Body.String())
	}
	if auth0.accountCalls != 0 || auth0.setupTicketCalls != 0 {
		t.Errorf("no debía crear cuenta ni ticket: account=%d ticket=%d", auth0.accountCalls, auth0.setupTicketCalls)
	}
	if mailer.inviteCalls != 1 || mailer.lastInvite.SetupURL != "" {
		t.Errorf("SetupURL debía estar vacío para cuenta existente")
	}
}

func TestInviteCenterUser_YaEnElCentro_409(t *testing.T) {
	t.Setenv("AUTH0_ADMIN_ROLE_ID", "rol_test")
	repo := newUsersRepo(currentAdminID, "auth0|existente")
	auth0 := &mockAuth0Client{user: &auth0mgmt.User{ID: "auth0|existente"}}
	r := newUsersRouter(t, repo, auth0, &mockMailer{})

	w := doRequest(t, r, http.MethodPost, "/admin/users", `{"email":"e@club.cl"}`)

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", w.Code)
	}
	if auth0.assignRoleCalls != 0 {
		t.Errorf("no debía asignar rol")
	}
}

func TestInviteCenterUser_AdministraOtroCentro_409(t *testing.T) {
	t.Setenv("AUTH0_ADMIN_ROLE_ID", "rol_test")
	repo := newUsersRepo(currentAdminID)
	repo.byUser["auth0|otro"] = []domain.SportCenter{{ID: primitive.NewObjectID(), Name: "Otro"}}
	auth0 := &mockAuth0Client{user: &auth0mgmt.User{ID: "auth0|otro"}}
	r := newUsersRouter(t, repo, auth0, &mockMailer{})

	w := doRequest(t, r, http.MethodPost, "/admin/users", `{"email":"o@club.cl"}`)

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", w.Code)
	}
}

func TestInviteCenterUser_EmailInvalido_400(t *testing.T) {
	t.Setenv("AUTH0_ADMIN_ROLE_ID", "rol_test")
	r := newUsersRouter(t, newUsersRepo(currentAdminID), &mockAuth0Client{}, &mockMailer{})

	w := doRequest(t, r, http.MethodPost, "/admin/users", `{"email":"no-es-correo"}`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestInviteCenterUser_SinRolConfigurado_500(t *testing.T) {
	t.Setenv("AUTH0_ADMIN_ROLE_ID", "")
	r := newUsersRouter(t, newUsersRepo(currentAdminID), &mockAuth0Client{}, &mockMailer{})

	w := doRequest(t, r, http.MethodPost, "/admin/users", `{"email":"a@club.cl"}`)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

func TestRemoveCenterUser_ValidaPropioYUltimoAdmin(t *testing.T) {
	t.Run("no puede quitarse a sí mismo", func(t *testing.T) {
		r := newUsersRouter(t, newUsersRepo(currentAdminID, "auth0|otro"), &mockAuth0Client{}, nil)
		w := doRequest(t, r, http.MethodDelete, "/admin/users/"+currentAdminID, "")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", w.Code)
		}
	})
	t.Run("no puede quitar al último administrador", func(t *testing.T) {
		r := newUsersRouter(t, newUsersRepo("auth0|otro"), &mockAuth0Client{}, nil)
		w := doRequest(t, r, http.MethodDelete, "/admin/users/auth0|otro", "")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", w.Code)
		}
	})
	t.Run("quita a otro administrador", func(t *testing.T) {
		repo := newUsersRepo(currentAdminID, "auth0|otro")
		r := newUsersRouter(t, repo, &mockAuth0Client{}, nil)
		w := doRequest(t, r, http.MethodDelete, "/admin/users/auth0|otro", "")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %s)", w.Code, w.Body.String())
		}
		if repo.lastUpdated == nil || len(repo.lastUpdated.Users) != 1 {
			t.Errorf("users = %+v", repo.lastUpdated)
		}
	})
}

func TestInviteCenterUser_RolNoConfirmado_502(t *testing.T) {
	t.Setenv("AUTH0_ADMIN_ROLE_ID", "rol_test")
	repo := newUsersRepo(currentAdminID)
	auth0 := &mockAuth0Client{hasRole: false}
	r := newUsersRouter(t, repo, auth0, &mockMailer{})

	w := doRequest(t, r, http.MethodPost, "/admin/users", `{"email":"nuevo@club.cl"}`)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", w.Code)
	}
	if repo.lastUpdated != nil {
		t.Errorf("no debía agregarse al centro si el rol no se confirmó")
	}
}

func TestInviteCenterUser_RolesNoLegibles_Continua(t *testing.T) {
	t.Setenv("AUTH0_ADMIN_ROLE_ID", "rol_test")
	auth0 := &mockAuth0Client{hasRoleErr: auth0mgmt.ErrRolesUnreadable}
	r := newUsersRouter(t, newUsersRepo(currentAdminID), auth0, &mockMailer{})

	w := doRequest(t, r, http.MethodPost, "/admin/users", `{"email":"nuevo@club.cl"}`)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %s)", w.Code, w.Body.String())
	}
}

func TestInviteCenterUser_Reenviar_ReasignaRolYEnviaCorreo(t *testing.T) {
	t.Setenv("AUTH0_ADMIN_ROLE_ID", "rol_test")
	repo := newUsersRepo(currentAdminID, "auth0|existente")
	auth0 := &mockAuth0Client{hasRole: true, user: &auth0mgmt.User{ID: "auth0|existente"}}
	mailer := &mockMailer{}
	r := newUsersRouter(t, repo, auth0, mailer)

	w := doRequest(t, r, http.MethodPost, "/admin/users", `{"email":"e@club.cl","resend":true}`)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}
	if auth0.assignRoleCalls != 1 || mailer.inviteCalls != 1 {
		t.Errorf("assign=%d invite=%d, want 1 y 1", auth0.assignRoleCalls, mailer.inviteCalls)
	}
	if repo.lastUpdated != nil {
		t.Errorf("no debía modificarse center.Users al reenviar")
	}
}
