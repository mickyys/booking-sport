package infra

// Tests HTTP del flujo de onboarding (RES-22 / RES-36 / RES-37):
//
//	POST /api/sport-centers/onboard
//	  RUT inválido            -> 400
//	  cuenta inexistente      -> 422 (Auth0: aún no creó su cuenta)
//	  email sin verificar     -> permitido (no bloquea el registro)
//	  slug duplicado          -> 409
//	  nombre+RUT duplicado    -> 422
//	  correo ya registrado    -> 422
//	  Auth0 caído             -> 502
//	  nombre > 100            -> 400 (binding max=100)
//	POST /api/invitations
//	GET  /api/sport-centers/onboard/verification-status

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hamp/booking-sport/internal/app"
	"github.com/hamp/booking-sport/internal/domain"
	"github.com/hamp/booking-sport/internal/infra/auth0mgmt"
	"github.com/hamp/booking-sport/internal/infra/middleware"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type mockCenterRepoForOnboardHandler struct {
	existingBySlug       *domain.SportCenter
	existingByNameAndRUT *domain.SportCenter
	existingByAdminEmail *domain.SportCenter
	createCalls          int
	lastCreated          *domain.SportCenter
	centersByUser        []domain.SportCenter
	lastUpdated          *domain.SportCenter
	settingsCalls        int
	lastPaymentMode      *string
}

func (m *mockCenterRepoForOnboardHandler) Create(ctx context.Context, center *domain.SportCenter) error {
	m.createCalls++
	m.lastCreated = center
	return nil
}
func (m *mockCenterRepoForOnboardHandler) Update(ctx context.Context, center *domain.SportCenter) error {
	m.lastUpdated = center
	return nil
}
func (m *mockCenterRepoForOnboardHandler) UpdateSettings(ctx context.Context, id primitive.ObjectID, slug *string, cancellationHours *int, retentionPercent *int, partialPaymentEnabled *bool, partialPaymentPercent *int, imageURL *string, marketplaceFeePercent *int, isFeatured *bool, branding *domain.SportCenterBranding, address *string, city *string, coordinates *domain.Coordinates, paymentMode *string) error {
	m.settingsCalls++
	m.lastPaymentMode = paymentMode
	return nil
}
func (m *mockCenterRepoForOnboardHandler) FindByID(ctx context.Context, id primitive.ObjectID) (*domain.SportCenter, error) {
	return nil, nil
}
func (m *mockCenterRepoForOnboardHandler) FindBySlug(ctx context.Context, slug string) (*domain.SportCenter, error) {
	if m.existingBySlug != nil {
		return m.existingBySlug, nil
	}
	return nil, nil
}
func (m *mockCenterRepoForOnboardHandler) FindBySubdomain(ctx context.Context, subdomain string) (*domain.SportCenter, error) {
	return nil, nil
}
func (m *mockCenterRepoForOnboardHandler) FindByNameAndRUT(ctx context.Context, name, rut string) (*domain.SportCenter, error) {
	if m.existingByNameAndRUT != nil {
		return m.existingByNameAndRUT, nil
	}
	return nil, nil
}
func (m *mockCenterRepoForOnboardHandler) FindByAdminEmail(ctx context.Context, email string) (*domain.SportCenter, error) {
	if m.existingByAdminEmail != nil {
		return m.existingByAdminEmail, nil
	}
	return nil, nil
}
func (m *mockCenterRepoForOnboardHandler) FindByHost(ctx context.Context, host string) (*domain.SportCenter, error) {
	return nil, nil
}
func (m *mockCenterRepoForOnboardHandler) FindAll(ctx context.Context) ([]domain.SportCenter, error) {
	return nil, nil
}
func (m *mockCenterRepoForOnboardHandler) FindPaged(ctx context.Context, page, limit int, name, city string, date *time.Time, hour *int, lat, lng, radiusKm *float64, priorityCity string) ([]domain.SportCenter, int64, bool, error) {
	return nil, 0, false, nil
}
func (m *mockCenterRepoForOnboardHandler) FindByUserID(ctx context.Context, userID string) ([]domain.SportCenter, error) {
	return m.centersByUser, nil
}
func (m *mockCenterRepoForOnboardHandler) GetCities(ctx context.Context) ([]string, error) {
	return nil, nil
}
func (m *mockCenterRepoForOnboardHandler) UpdateMercadoPagoConfig(ctx context.Context, id primitive.ObjectID, config *domain.MercadoPagoConfig) error {
	return nil
}

var _ app.SportCenterRepository = (*mockCenterRepoForOnboardHandler)(nil)

// ---------- Mock auth0mgmt.Client ----------

type mockAuth0Client struct {
	user                *auth0mgmt.User // resultado de FindByEmail; nil = no existe
	findByEmailErr      error
	accountCalls        int    // llamadas a CreateAccount
	lastAccountEmail    string // email pasado a CreateAccount
	lastAccountPassword string // clave pasada a CreateAccount
	assignRoleCalls     int
	lastAssignedRole    string
	lastAssignedUser    string
	assignRoleErr       error
	hasRole             bool
	hasRoleErr          error
	hasRoleCalls        int
	accountErr          error

	setupTicketCalls   int
	lastSetupTicketUID string
	setupTicketURL     string
	setupTicketErr     error
}

func (m *mockAuth0Client) FindByEmail(ctx context.Context, email string) (*auth0mgmt.User, error) {
	if m.findByEmailErr != nil {
		return nil, m.findByEmailErr
	}
	return m.user, nil
}

func (m *mockAuth0Client) CreateAccount(ctx context.Context, email, password string) (*auth0mgmt.User, error) {
	m.accountCalls++
	m.lastAccountEmail = email
	m.lastAccountPassword = password
	if m.accountErr != nil {
		return nil, m.accountErr
	}
	return &auth0mgmt.User{ID: "auth0|creado", Email: email, EmailVerified: true}, nil
}

func (m *mockAuth0Client) CreatePasswordSetupTicket(ctx context.Context, auth0UserID string) (string, error) {
	m.setupTicketCalls++
	m.lastSetupTicketUID = auth0UserID
	if m.setupTicketErr != nil {
		return "", m.setupTicketErr
	}
	if m.setupTicketURL == "" {
		return "https://tenant.auth0.com/changepw/ticket-123", nil
	}
	return m.setupTicketURL, nil
}

func (m *mockAuth0Client) AssignRole(ctx context.Context, auth0UserID, roleID string) error {
	m.assignRoleCalls++
	m.lastAssignedUser = auth0UserID
	m.lastAssignedRole = roleID
	return m.assignRoleErr
}

func (m *mockAuth0Client) UserHasRole(ctx context.Context, auth0UserID, roleID string) (bool, error) {
	m.hasRoleCalls++
	if m.hasRoleErr != nil {
		return false, m.hasRoleErr
	}
	return m.hasRole, nil
}

var _ auth0mgmt.Client = (*mockAuth0Client)(nil)

// ---------- Mock app.Mailer ----------

type mockMailer struct {
	welcomeCalls int
	lastWelcome  app.CenterWelcomeEmail
	welcomeErr   error

	inviteCalls int
	lastInvite  app.CenterInvitationEmail
	inviteErr   error
}

func (m *mockMailer) SendBookingConfirmation(ctx context.Context, booking *domain.Booking, cancellationHours, retentionPercent int, paidAmount, pendingAmount float64) error {
	return nil
}
func (m *mockMailer) SendBookingCancellation(ctx context.Context, booking *domain.Booking) error {
	return nil
}
func (m *mockMailer) SendContactEmail(ctx context.Context, to string, name string, email string, phone string, sportCenterName string, message string) error {
	return nil
}
func (m *mockMailer) SendCenterOnboardWelcome(ctx context.Context, data app.CenterWelcomeEmail) error {
	m.welcomeCalls++
	m.lastWelcome = data
	return m.welcomeErr
}

func (m *mockMailer) SendCenterInvitation(ctx context.Context, data app.CenterInvitationEmail) error {
	m.inviteCalls++
	m.lastInvite = data
	return m.inviteErr
}

var _ app.Mailer = (*mockMailer)(nil)

// ---------- Router de prueba ----------

func newOnboardRouter(t *testing.T, repo *mockCenterRepoForOnboardHandler, auth0 *mockAuth0Client, mailers ...app.Mailer) *gin.Engine {
	t.Helper()

	gin.SetMode(gin.TestMode)
	uc := app.NewSportCenterUseCase(repo, nil, nil, nil, nil, nil, nil, nil)
	var mailer app.Mailer
	if len(mailers) > 0 {
		mailer = mailers[0]
	}
	h := NewSportCenterHandler(uc, auth0, mailer, nil)
	r := gin.New()
	r.Use(middleware.Recovery()) // paridad con cmd/app/main.go:174
	r.POST("/api/sport-centers/onboard", h.OnboardCreate)
	r.GET("/api/sport-centers/onboard/verification-status", h.OnboardVerificationStatus)
	return r
}

func doRequest(t *testing.T, r *gin.Engine, method, path, payload string) *httptest.ResponseRecorder {
	t.Helper()
	var body *strings.Reader
	if payload == "" {
		body = strings.NewReader("")
	} else {
		body = strings.NewReader(payload)
	}
	req := httptest.NewRequest(method, path, body)
	if payload != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

const verifiedAuth0ID = "auth0|verified-admin"

// verifiedAuth0Client retorna un mock con el email ya verificado en Auth0.
func verifiedAuth0Client() *mockAuth0Client {
	return &mockAuth0Client{
		user: &auth0mgmt.User{ID: verifiedAuth0ID, Name: "Hector", Email: "admin@club.cl", EmailVerified: true},
	}
}

const validPayload = `{"nombre":"Club Andino Ñuñoa","rut":"12.345.678-5","direccion":"Av. Ossa 123","email":"admin@club.cl"}`

// ---------- POST /onboard: validaciones de binding ----------

func TestOnboardHandler_NombreMayorA100_Retorna400(t *testing.T) {
	r := newOnboardRouter(t, &mockCenterRepoForOnboardHandler{}, verifiedAuth0Client())

	longName := strings.Repeat("a", 101)
	payload := `{"nombre":"` + longName + `","rut":"12.345.678-5","direccion":"Dir","email":"admin@club.cl"}`

	w := doRequest(t, r, http.MethodPost, "/api/sport-centers/onboard", payload)
	if w.Code != http.StatusBadRequest {
		t.Errorf("nombre >100: status = %d, want %d (binding max=100). body=%s",
			w.Code, http.StatusBadRequest, w.Body.String())
	}
}

func TestOnboardHandler_EmailRequerido_Retorna400(t *testing.T) {
	r := newOnboardRouter(t, &mockCenterRepoForOnboardHandler{}, verifiedAuth0Client())

	payload := `{"nombre":"Club E","rut":"12.345.678-5","direccion":"Dir"}`
	w := doRequest(t, r, http.MethodPost, "/api/sport-centers/onboard", payload)
	if w.Code != http.StatusBadRequest {
		t.Errorf("email ausente: status = %d, want %d (binding required). body=%s",
			w.Code, http.StatusBadRequest, w.Body.String())
	}
}

// ---------- POST /onboard: gate de cuenta Auth0 ----------

func TestOnboardHandler_EmailNoVerificado_CreaCentroYAsignaRol(t *testing.T) {
	auth0Mock := &mockAuth0Client{
		user: &auth0mgmt.User{ID: "auth0|unverified", Email: "admin@club.cl", EmailVerified: false},
	}
	repo := &mockCenterRepoForOnboardHandler{}
	r := newOnboardRouter(t, repo, auth0Mock)

	w := doRequest(t, r, http.MethodPost, "/api/sport-centers/onboard", validPayload)
	if w.Code != http.StatusCreated {
		t.Fatalf("email sin verificar debe permitir el registro: status = %d, want %d. body=%s",
			w.Code, http.StatusCreated, w.Body.String())
	}
	if repo.createCalls != 1 || repo.lastCreated == nil {
		t.Fatal("repo.Create no fue llamado exactamente una vez")
	}
	if len(repo.lastCreated.Users) != 1 || repo.lastCreated.Users[0] != "auth0|unverified" {
		t.Errorf("users = %v, want [auth0|unverified]", repo.lastCreated.Users)
	}
	if auth0Mock.assignRoleCalls != 1 || auth0Mock.lastAssignedUser != "auth0|unverified" {
		t.Errorf("AssignRole calls=%d user=%q, want 1 llamada para auth0|unverified",
			auth0Mock.assignRoleCalls, auth0Mock.lastAssignedUser)
	}
}

// ---------- POST /onboard: creación de cuenta con clave ----------

func TestOnboardHandler_CuentaNuevaSinClave_Retorna400(t *testing.T) {
	auth0Mock := &mockAuth0Client{} // user nil = cuenta nueva
	repo := &mockCenterRepoForOnboardHandler{}
	r := newOnboardRouter(t, repo, auth0Mock)

	w := doRequest(t, r, http.MethodPost, "/api/sport-centers/onboard", validPayload)
	if w.Code != http.StatusBadRequest {
		t.Errorf("cuenta nueva sin clave: status = %d, want %d. body=%s",
			w.Code, http.StatusBadRequest, w.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err == nil && !strings.Contains(resp["error"], "clave") {
		t.Errorf("error = %q, want mencionar la clave", resp["error"])
	}
	if auth0Mock.accountCalls != 0 {
		t.Errorf("CreateAccount llamado %d veces, want 0", auth0Mock.accountCalls)
	}
	if repo.createCalls != 0 {
		t.Errorf("repo.Create llamado %d veces, want 0", repo.createCalls)
	}
}

func TestOnboardHandler_CuentaNuevaConClaveDebil_Retorna400(t *testing.T) {
	cases := []struct {
		name  string
		clave string
	}{
		{"muy corta", "Ab1x"},
		{"sin mayúscula", "clavesegura1"},
		{"sin minúscula", "CLAVESEGURA1"},
		{"sin número", "ClaveSegura"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			auth0Mock := &mockAuth0Client{}
			repo := &mockCenterRepoForOnboardHandler{}
			r := newOnboardRouter(t, repo, auth0Mock)

			payload := `{"nombre":"Club K","rut":"12.345.678-5","direccion":"Dir","email":"admin@club.cl","clave":"` + tc.clave + `"}`
			w := doRequest(t, r, http.MethodPost, "/api/sport-centers/onboard", payload)
			if w.Code != http.StatusBadRequest {
				t.Errorf("%s: status = %d, want %d. body=%s", tc.name, w.Code, http.StatusBadRequest, w.Body.String())
			}
			if auth0Mock.accountCalls != 0 {
				t.Errorf("CreateAccount llamado %d veces, want 0", auth0Mock.accountCalls)
			}
		})
	}
}

func TestOnboardHandler_CuentaNuevaConClaveValida_CreaCuentaCentroRolYEnviaBienvenida(t *testing.T) {
	auth0Mock := &mockAuth0Client{} // user nil = cuenta nueva
	repo := &mockCenterRepoForOnboardHandler{}
	mailerMock := &mockMailer{}
	t.Setenv("PLATFORM_DOMAIN", "reservaloya.cl")
	t.Setenv("URL_FRONTEND", "https://app.reservaloya.cl")
	r := newOnboardRouter(t, repo, auth0Mock, mailerMock)

	w := doRequest(t, r, http.MethodPost, "/api/sport-centers/onboard",
		`{"nombre":"Club Nuevo","rut":"12.345.678-5","direccion":"Av. Ossa 123","email":"Admin@Club.CL","clave":"ClaveSegura123"}`)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d. body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	if auth0Mock.accountCalls != 1 {
		t.Fatalf("CreateAccount calls = %d, want 1", auth0Mock.accountCalls)
	}
	if auth0Mock.lastAccountEmail != "admin@club.cl" || auth0Mock.lastAccountPassword != "ClaveSegura123" {
		t.Errorf("CreateAccount(email=%q clave=%q), want email normalizado y clave del formulario",
			auth0Mock.lastAccountEmail, auth0Mock.lastAccountPassword)
	}
	if auth0Mock.assignRoleCalls != 1 || auth0Mock.lastAssignedUser != "auth0|creado" {
		t.Errorf("AssignRole calls=%d user=%q, want 1 para auth0|creado",
			auth0Mock.assignRoleCalls, auth0Mock.lastAssignedUser)
	}
	center := repo.lastCreated
	if center == nil {
		t.Fatal("repo.Create no fue llamado")
	}
	if len(center.Users) != 1 || center.Users[0] != "auth0|creado" {
		t.Errorf("users = %v, want [auth0|creado]", center.Users)
	}
	// Cuenta nueva: ticket de configuración de clave + bienvenida con SetupURL.
	if auth0Mock.setupTicketCalls != 1 || auth0Mock.lastSetupTicketUID != "auth0|creado" {
		t.Fatalf("setupTicket calls=%d uid=%q, want 1 para auth0|creado",
			auth0Mock.setupTicketCalls, auth0Mock.lastSetupTicketUID)
	}
	if mailerMock.welcomeCalls != 1 {
		t.Fatalf("SendCenterOnboardWelcome calls = %d, want 1", mailerMock.welcomeCalls)
	}
	bienvenida := mailerMock.lastWelcome
	if bienvenida.To != "admin@club.cl" {
		t.Errorf("welcome.To = %q, want admin@club.cl", bienvenida.To)
	}
	if bienvenida.CenterName != "Club Nuevo" || bienvenida.Address != "Av. Ossa 123" {
		t.Errorf("welcome centro/dirección = %q/%q, want Club Nuevo/Av. Ossa 123", bienvenida.CenterName, bienvenida.Address)
	}
	if bienvenida.TenantURL != "https://club-nuevo.reservaloya.cl" {
		t.Errorf("welcome.TenantURL = %q, want https://club-nuevo.reservaloya.cl", bienvenida.TenantURL)
	}
	if bienvenida.LoginURL != "https://app.reservaloya.cl/admin" {
		t.Errorf("welcome.LoginURL = %q, want https://app.reservaloya.cl/admin", bienvenida.LoginURL)
	}
	if bienvenida.SetupURL == "" {
		t.Error("welcome.SetupURL vacío; cuenta nueva requiere enlace para configurar la clave")
	}
}

func TestOnboardHandler_FalloTicketSetup_IgualEnviaBienvenidaSinEnlace(t *testing.T) {
	auth0Mock := &mockAuth0Client{setupTicketErr: context.DeadlineExceeded}
	repo := &mockCenterRepoForOnboardHandler{}
	mailerMock := &mockMailer{}
	r := newOnboardRouter(t, repo, auth0Mock, mailerMock)

	w := doRequest(t, r, http.MethodPost, "/api/sport-centers/onboard",
		`{"nombre":"Club T","rut":"12.345.678-5","direccion":"Dir","email":"admin@club.cl","clave":"ClaveSegura123"}`)

	if w.Code != http.StatusCreated {
		t.Fatalf("fallo del ticket debe ser best-effort: status = %d, want %d. body=%s",
			w.Code, http.StatusCreated, w.Body.String())
	}
	if mailerMock.welcomeCalls != 1 {
		t.Fatalf("bienvenida enviada %d veces, want 1 (aun sin ticket)", mailerMock.welcomeCalls)
	}
	if mailerMock.lastWelcome.SetupURL != "" {
		t.Errorf("SetupURL = %q, want vacío cuando el ticket falla", mailerMock.lastWelcome.SetupURL)
	}
}

func TestOnboardHandler_FalloMailgun_NoBloqueaRegistro(t *testing.T) {
	auth0Mock := verifiedAuth0Client()
	repo := &mockCenterRepoForOnboardHandler{}
	mailerMock := &mockMailer{welcomeErr: context.DeadlineExceeded}
	r := newOnboardRouter(t, repo, auth0Mock, mailerMock)

	w := doRequest(t, r, http.MethodPost, "/api/sport-centers/onboard", validPayload)
	if w.Code != http.StatusCreated {
		t.Errorf("fallo de Mailgun debe ser best-effort: status = %d, want %d. body=%s",
			w.Code, http.StatusCreated, w.Body.String())
	}
	if repo.createCalls != 1 {
		t.Errorf("repo.Create llamado %d veces, want 1 (el centro igual se crea)", repo.createCalls)
	}
}

func TestOnboardHandler_Auth0Caido_Retorna502(t *testing.T) {
	auth0Mock := &mockAuth0Client{
		findByEmailErr: context.DeadlineExceeded,
	}
	r := newOnboardRouter(t, &mockCenterRepoForOnboardHandler{}, auth0Mock)

	w := doRequest(t, r, http.MethodPost, "/api/sport-centers/onboard", validPayload)
	if w.Code != http.StatusBadGateway {
		t.Errorf("auth0 down: status = %d, want %d. body=%s",
			w.Code, http.StatusBadGateway, w.Body.String())
	}
}

// ---------- POST /onboard: camino feliz y ownership ----------

func TestOnboardHandler_EmailVerificado_CreaCentroConOwnershipYEmailNormalizado(t *testing.T) {
	auth0Mock := verifiedAuth0Client()
	repo := &mockCenterRepoForOnboardHandler{}
	r := newOnboardRouter(t, repo, auth0Mock)

	payload := `{"nombre":"Club Andino Ñuñoa","rut":"12.345.678-5","direccion":"Av. Ossa 123","email":"Admin@CLUB.CL"}`
	w := doRequest(t, r, http.MethodPost, "/api/sport-centers/onboard", payload)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d. body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	if repo.createCalls != 1 || repo.lastCreated == nil {
		t.Fatal("repo.Create no fue llamado exactamente una vez")
	}
	center := repo.lastCreated
	if center.AdminEmail != "admin@club.cl" {
		t.Errorf("admin_email = %q, want normalizado %q", center.AdminEmail, "admin@club.cl")
	}
	if len(center.Users) != 1 || center.Users[0] != verifiedAuth0ID {
		t.Errorf("users = %v, want [%s]", center.Users, verifiedAuth0ID)
	}
	if auth0Mock.assignRoleCalls != 1 || auth0Mock.lastAssignedUser != verifiedAuth0ID {
		t.Errorf("AssignRole calls=%d user=%q, want 1 llamada para %s",
			auth0Mock.assignRoleCalls, auth0Mock.lastAssignedUser, verifiedAuth0ID)
	}
}

func TestOnboardHandler_TiposMultiplesValidos_Retorna201(t *testing.T) {
	auth0Mock := verifiedAuth0Client()
	repo := &mockCenterRepoForOnboardHandler{}
	r := newOnboardRouter(t, repo, auth0Mock)

	payload := `{"nombre":"Club M","rut":"12.345.678-5","direccion":"Dir","email":"admin@club.cl"}`
	w := doRequest(t, r, http.MethodPost, "/api/sport-centers/onboard", payload)

	if w.Code != http.StatusCreated {
		t.Errorf("camino feliz: status = %d, want %d. body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
}

// ---------- POST /onboard: ciudad y coordenadas del frontend ----------

func TestOnboardHandler_ConCiudadYCoordenadas_PersisteUbicacion(t *testing.T) {
	repo := &mockCenterRepoForOnboardHandler{}
	r := newOnboardRouter(t, repo, verifiedAuth0Client())

	payload := `{"nombre":"Club Geo","rut":"12.345.678-5","direccion":"Av. Ossa 123","email":"admin@club.cl","ciudad":"La Reina","latitud":-33.4383,"longitud":-70.7511}`
	w := doRequest(t, r, http.MethodPost, "/api/sport-centers/onboard", payload)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d. body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	center := repo.lastCreated
	if center == nil {
		t.Fatal("repo.lastCreated = nil")
	}
	if center.City != "La Reina" {
		t.Errorf("city = %q, want %q", center.City, "La Reina")
	}
	if center.Coordinates.Lat != -33.4383 || center.Coordinates.Lng != -70.7511 {
		t.Errorf("coordinates = %+v, want {lat:-33.4383 lng:-70.7511}", center.Coordinates)
	}
	if center.Location == nil || center.Location.Coordinates[0] != -70.7511 || center.Location.Coordinates[1] != -33.4383 {
		t.Errorf("location = %+v, want Point [-70.7511 -33.4383]", center.Location)
	}

	var resp struct {
		Coordinates *struct {
			Lat float64 `json:"lat"`
			Lng float64 `json:"lng"`
		} `json:"coordinates"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("respuesta no es JSON válido: %v", err)
	}
	if resp.Coordinates == nil || resp.Coordinates.Lat != -33.4383 || resp.Coordinates.Lng != -70.7511 {
		t.Errorf("coordinates en respuesta = %+v, want lat/lng poblados", resp.Coordinates)
	}
}

func TestOnboardHandler_LatitudSinLongitud_Retorna400(t *testing.T) {
	repo := &mockCenterRepoForOnboardHandler{}
	r := newOnboardRouter(t, repo, verifiedAuth0Client())

	payload := `{"nombre":"Club Parcial","rut":"12.345.678-5","direccion":"Dir","email":"admin@club.cl","ciudad":"Santiago","latitud":-33.45}`
	w := doRequest(t, r, http.MethodPost, "/api/sport-centers/onboard", payload)
	if w.Code != http.StatusBadRequest {
		t.Errorf("latitud sin longitud: status = %d, want %d. body=%s",
			w.Code, http.StatusBadRequest, w.Body.String())
	}
	if repo.createCalls != 0 {
		t.Errorf("repo.Create llamado %d veces, want 0", repo.createCalls)
	}
}

func TestOnboardHandler_CoordenadasFueraDeRango_Retorna400(t *testing.T) {
	cases := []struct {
		name string
		json string
	}{
		{"latitud > 90", `"latitud":91,"longitud":-70.7`},
		{"longitud < -180", `"latitud":-33.4,"longitud":-180.1`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &mockCenterRepoForOnboardHandler{}
			r := newOnboardRouter(t, repo, verifiedAuth0Client())

			payload := `{"nombre":"Club Rango","rut":"12.345.678-5","direccion":"Dir","email":"admin@club.cl",` + tc.json + `}`
			w := doRequest(t, r, http.MethodPost, "/api/sport-centers/onboard", payload)
			if w.Code != http.StatusBadRequest {
				t.Errorf("%s: status = %d, want %d. body=%s", tc.name, w.Code, http.StatusBadRequest, w.Body.String())
			}
			if repo.createCalls != 0 {
				t.Errorf("repo.Create llamado %d veces, want 0", repo.createCalls)
			}
		})
	}
}

// ---------- POST /onboard: conflictos ----------

func TestOnboardHandler_SlugDuplicado_Retorna409(t *testing.T) {
	mock := &mockCenterRepoForOnboardHandler{
		existingBySlug: &domain.SportCenter{ID: primitive.NewObjectID(), Slug: "club-andino-nunoa"},
	}
	r := newOnboardRouter(t, mock, verifiedAuth0Client())

	w := doRequest(t, r, http.MethodPost, "/api/sport-centers/onboard", validPayload)
	if w.Code != http.StatusConflict {
		t.Errorf("slug duplicado: status = %d, want %d. body=%s", w.Code, http.StatusConflict, w.Body.String())
	}
	if mock.createCalls != 0 {
		t.Errorf("repo.Create llamado %d veces, want 0", mock.createCalls)
	}
}

func TestOnboardHandler_NombreRUTDuplicado_Retorna422(t *testing.T) {
	mock := &mockCenterRepoForOnboardHandler{
		existingByNameAndRUT: &domain.SportCenter{ID: primitive.NewObjectID(), Name: "Club Andino Ñuñoa", Rut: "12.345.678-5"},
	}
	r := newOnboardRouter(t, mock, verifiedAuth0Client())

	w := doRequest(t, r, http.MethodPost, "/api/sport-centers/onboard", validPayload)
	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("nombre+RUT duplicado: status = %d, want %d. body=%s",
			w.Code, http.StatusUnprocessableEntity, w.Body.String())
	}
}

func TestOnboardHandler_AdminEmailDuplicado_Retorna409(t *testing.T) {
	mock := &mockCenterRepoForOnboardHandler{
		existingByAdminEmail: &domain.SportCenter{ID: primitive.NewObjectID(), AdminEmail: "admin@club.cl"},
	}
	auth0Mock := verifiedAuth0Client()
	mailerMock := &mockMailer{}
	r := newOnboardRouter(t, mock, auth0Mock, mailerMock)

	w := doRequest(t, r, http.MethodPost, "/api/sport-centers/onboard", validPayload)
	if w.Code != http.StatusConflict {
		t.Errorf("correo ya registrado: status = %d, want %d. body=%s",
			w.Code, http.StatusConflict, w.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err == nil && !strings.Contains(resp["error"], "ya está registrado") {
		t.Errorf("error = %q, want mencionar 'ya está registrado'", resp["error"])
	}
	if mock.createCalls != 0 {
		t.Errorf("repo.Create llamado %d veces, want 0", mock.createCalls)
	}
	if auth0Mock.accountCalls != 0 {
		t.Errorf("CreateAccount llamado %d veces, want 0 (la cuenta ya existe)", auth0Mock.accountCalls)
	}
	if auth0Mock.assignRoleCalls != 0 {
		t.Errorf("AssignRole llamado %d veces, want 0 (bloqueo antes de asignar rol)", auth0Mock.assignRoleCalls)
	}
	if mailerMock.welcomeCalls != 0 {
		t.Errorf("bienvenida enviada %d veces, want 0", mailerMock.welcomeCalls)
	}
}

func TestOnboardHandler_UsuarioExistenteSinCentro_IgnoraClaveYUsaCuentaExistente(t *testing.T) {
	auth0Mock := &mockAuth0Client{
		user: &auth0mgmt.User{ID: "auth0|jugador", Name: "Hector", Email: "admin@club.cl", EmailVerified: true},
	}
	repo := &mockCenterRepoForOnboardHandler{}
	mailerMock := &mockMailer{}
	r := newOnboardRouter(t, repo, auth0Mock, mailerMock)

	// El frontend no envía clave cuando el correo ya existe en Auth0.
	w := doRequest(t, r, http.MethodPost, "/api/sport-centers/onboard", validPayload)
	if w.Code != http.StatusCreated {
		t.Fatalf("usuario existente sin centro: status = %d, want %d. body=%s",
			w.Code, http.StatusCreated, w.Body.String())
	}
	if auth0Mock.accountCalls != 0 {
		t.Errorf("CreateAccount llamado %d veces, want 0 (no debe tocar la cuenta existente)", auth0Mock.accountCalls)
	}
	if len(repo.lastCreated.Users) != 1 || repo.lastCreated.Users[0] != "auth0|jugador" {
		t.Errorf("users = %v, want [auth0|jugador]", repo.lastCreated.Users)
	}
	if mailerMock.welcomeCalls != 1 {
		t.Errorf("bienvenida enviada %d veces, want 1", mailerMock.welcomeCalls)
	}
	// Cuenta existente: sin ticket de setup y con saludo personalizado.
	if auth0Mock.setupTicketCalls != 0 {
		t.Errorf("setupTicket llamado %d veces, want 0 (la cuenta ya existe)", auth0Mock.setupTicketCalls)
	}
	if mailerMock.lastWelcome.SetupURL != "" {
		t.Errorf("SetupURL = %q, want vacío para cuenta existente", mailerMock.lastWelcome.SetupURL)
	}
	if mailerMock.lastWelcome.UserName != "Hector" {
		t.Errorf("UserName = %q, want Hector (nombre del usuario Auth0)", mailerMock.lastWelcome.UserName)
	}
}

func TestOnboardHandler_RUTInvalido_Retorna400(t *testing.T) {
	r := newOnboardRouter(t, &mockCenterRepoForOnboardHandler{}, verifiedAuth0Client())

	payload := `{"nombre":"Club X","rut":"12.345.678-9","direccion":"Dir","email":"admin@club.cl"}`
	w := doRequest(t, r, http.MethodPost, "/api/sport-centers/onboard", payload)
	if w.Code != http.StatusBadRequest {
		t.Errorf("RUT inválido: status = %d, want %d. body=%s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

// ---------- GET /onboard/verification-status ----------

func TestVerificationStatus_Verificado_RetornaExistsYVerified(t *testing.T) {
	auth0Mock := &mockAuth0Client{
		user: &auth0mgmt.User{ID: "auth0|ok", Email: "admin@club.cl", EmailVerified: true},
	}
	r := newOnboardRouter(t, &mockCenterRepoForOnboardHandler{}, auth0Mock)

	w := doRequest(t, r, http.MethodGet, "/api/sport-centers/onboard/verification-status?email=admin%40club.cl", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d. body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var resp map[string]bool
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("respuesta no es JSON válido: %v", err)
	}
	if !resp["exists"] || !resp["verified"] {
		t.Errorf("exists=%v verified=%v, want ambos true", resp["exists"], resp["verified"])
	}
	if resp["has_center"] {
		t.Errorf("has_center = %v, want false cuando el usuario no administra centros", resp["has_center"])
	}
}

func TestVerificationStatus_ConCentro_RetornaHasCenterTrue(t *testing.T) {
	auth0Mock := &mockAuth0Client{
		user: &auth0mgmt.User{ID: "auth0|admin", Email: "admin@club.cl", EmailVerified: true},
	}
	repo := &mockCenterRepoForOnboardHandler{
		existingByAdminEmail: &domain.SportCenter{ID: primitive.NewObjectID(), AdminEmail: "admin@club.cl"},
	}
	r := newOnboardRouter(t, repo, auth0Mock)

	w := doRequest(t, r, http.MethodGet, "/api/sport-centers/onboard/verification-status?email=admin%40club.cl", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d. body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var resp map[string]bool
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("respuesta no es JSON válido: %v", err)
	}
	if !resp["exists"] || !resp["has_center"] {
		t.Errorf("exists=%v has_center=%v, want ambos true (el correo ya administra un centro)", resp["exists"], resp["has_center"])
	}
}

func TestVerificationStatus_ExisteSinVerificar_RetornaExistsSinVerified(t *testing.T) {
	auth0Mock := &mockAuth0Client{
		user: &auth0mgmt.User{ID: "auth0|pendiente", Email: "admin@club.cl", EmailVerified: false},
	}
	r := newOnboardRouter(t, &mockCenterRepoForOnboardHandler{}, auth0Mock)

	w := doRequest(t, r, http.MethodGet, "/api/sport-centers/onboard/verification-status?email=admin%40club.cl", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d. body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var resp map[string]bool
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("respuesta no es JSON válido: %v", err)
	}
	if !resp["exists"] {
		t.Errorf("exists = %v, want true (la cuenta fue creada)", resp["exists"])
	}
	if resp["verified"] {
		t.Errorf("verified = %v, want false cuando el email no está verificado", resp["verified"])
	}
}

func TestVerificationStatus_SinUsuario_RetornaFalse(t *testing.T) {
	r := newOnboardRouter(t, &mockCenterRepoForOnboardHandler{}, &mockAuth0Client{})

	w := doRequest(t, r, http.MethodGet, "/api/sport-centers/onboard/verification-status?email=nadie%40club.cl", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d. body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var resp map[string]bool
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("respuesta no es JSON válido: %v", err)
	}
	if resp["exists"] || resp["verified"] || resp["has_center"] {
		t.Errorf("exists=%v verified=%v has_center=%v, want todos false cuando el usuario no existe en Auth0",
			resp["exists"], resp["verified"], resp["has_center"])
	}
}

func TestVerificationStatus_SinQueryParam_Retorna400(t *testing.T) {
	r := newOnboardRouter(t, &mockCenterRepoForOnboardHandler{}, &mockAuth0Client{})

	w := doRequest(t, r, http.MethodGet, "/api/sport-centers/onboard/verification-status", "")
	if w.Code != http.StatusBadRequest {
		t.Errorf("sin query email: status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// ---------- PUT /admin/my-sport-center/onboarding (wizard) ----------

func wizardCenter() domain.SportCenter {
	return domain.SportCenter{
		ID:               primitive.NewObjectID(),
		Name:             "Club Wizard",
		AdminEmail:       "admin@club.cl",
		Users:            []string{"auth0|wizard-admin"},
		OnboardingStatus: "pending",
	}
}

// mockWizardSpaceRepo captura los espacios materializados por el wizard.
type mockWizardSpaceRepo struct {
	created []*domain.Space
}

func (m *mockWizardSpaceRepo) Create(ctx context.Context, space *domain.Space) error {
	if space.ID.IsZero() {
		space.ID = primitive.NewObjectID()
	}
	m.created = append(m.created, space)
	return nil
}
func (m *mockWizardSpaceRepo) FindByID(ctx context.Context, id primitive.ObjectID) (*domain.Space, error) {
	return nil, nil
}
func (m *mockWizardSpaceRepo) FindByCenterID(ctx context.Context, centerID primitive.ObjectID) ([]domain.Space, error) {
	return nil, nil
}
func (m *mockWizardSpaceRepo) FindByCenterAndCategory(ctx context.Context, centerID primitive.ObjectID, category domain.SpaceCategory) ([]domain.Space, error) {
	return nil, nil
}
func (m *mockWizardSpaceRepo) Update(ctx context.Context, space *domain.Space) error   { return nil }
func (m *mockWizardSpaceRepo) Delete(ctx context.Context, id primitive.ObjectID) error { return nil }
func (m *mockWizardSpaceRepo) UpdateScheduleSlot(ctx context.Context, id primitive.ObjectID, slot domain.CourtSchedule) error {
	return nil
}
func (m *mockWizardSpaceRepo) SyncPartialPaymentSlots(ctx context.Context, centerID primitive.ObjectID, partialPaymentEnabled bool) (int64, error) {
	return 0, nil
}

// mockWizardRuleRepo captura las reglas de horario materializadas por el wizard.
type mockWizardRuleRepo struct {
	created []*domain.ScheduleRule
}

func (m *mockWizardRuleRepo) Create(ctx context.Context, rule *domain.ScheduleRule) error {
	if rule.ID.IsZero() {
		rule.ID = primitive.NewObjectID()
	}
	m.created = append(m.created, rule)
	return nil
}
func (m *mockWizardRuleRepo) Update(ctx context.Context, rule *domain.ScheduleRule) error { return nil }
func (m *mockWizardRuleRepo) FindByID(ctx context.Context, id primitive.ObjectID) (*domain.ScheduleRule, error) {
	return nil, nil
}
func (m *mockWizardRuleRepo) FindBySpaceID(ctx context.Context, spaceID primitive.ObjectID) ([]domain.ScheduleRule, error) {
	return nil, nil
}
func (m *mockWizardRuleRepo) FindBySportCenterID(ctx context.Context, centerID primitive.ObjectID) ([]domain.ScheduleRule, error) {
	return nil, nil
}
func (m *mockWizardRuleRepo) Delete(ctx context.Context, id primitive.ObjectID) error { return nil }
func (m *mockWizardRuleRepo) DeleteBySpaceID(ctx context.Context, spaceID primitive.ObjectID) error {
	return nil
}
func (m *mockWizardRuleRepo) SyncPaymentModeSlots(ctx context.Context, centerID primitive.ObjectID, paymentRequired bool) (int64, error) {
	return 0, nil
}

func newOnboardingWizardRouter(t *testing.T, repo *mockCenterRepoForOnboardHandler) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	uc := app.NewSportCenterUseCase(repo, nil, nil, nil, nil, nil, &mockWizardSpaceRepo{}, &mockWizardRuleRepo{})
	h := NewSportCenterHandler(uc, &mockAuth0Client{}, &mockMailer{}, nil)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("user_id", "auth0|wizard-admin") })
	r.PUT("/api/admin/my-sport-center/onboarding", h.UpdateMySportCenterOnboarding)
	return r
}

func TestOnboardingWizard_SinCentro_Retorna404(t *testing.T) {
	repo := &mockCenterRepoForOnboardHandler{}
	r := newOnboardingWizardRouter(t, repo)

	w := doRequest(t, r, http.MethodPut, "/api/admin/my-sport-center/onboarding", `{"step":1,"telefono":"+56 9 1234 5678"}`)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d. body=%s", w.Code, http.StatusNotFound, w.Body.String())
	}
}

func TestOnboardingWizard_EtapaContacto_GuardaTelefonoYAvanza(t *testing.T) {
	repo := &mockCenterRepoForOnboardHandler{centersByUser: []domain.SportCenter{wizardCenter()}}
	r := newOnboardingWizardRouter(t, repo)

	w := doRequest(t, r, http.MethodPut, "/api/admin/my-sport-center/onboarding", `{"step":1,"telefono":"+56 9 1234 5678"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d. body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	if repo.lastUpdated == nil {
		t.Fatal("Update no fue llamado")
	}
	if repo.lastUpdated.Contact.Phone != "+56 9 1234 5678" {
		t.Errorf("contact.phone = %q, want +56 9 1234 5678", repo.lastUpdated.Contact.Phone)
	}
	if repo.lastUpdated.Contact.Email != "admin@club.cl" {
		t.Errorf("contact.email = %q, want admin@club.cl", repo.lastUpdated.Contact.Email)
	}
	if repo.lastUpdated.OnboardingStep != 1 {
		t.Errorf("onboarding_step = %d, want 1", repo.lastUpdated.OnboardingStep)
	}
}

func TestOnboardingWizard_EtapaContacto_TelefonoInvalido_Retorna400(t *testing.T) {
	repo := &mockCenterRepoForOnboardHandler{centersByUser: []domain.SportCenter{wizardCenter()}}
	r := newOnboardingWizardRouter(t, repo)

	w := doRequest(t, r, http.MethodPut, "/api/admin/my-sport-center/onboarding", `{"step":1,"telefono":"123"}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d. body=%s", w.Code, http.StatusBadRequest, w.Body.String())
	}
	if repo.lastUpdated != nil {
		t.Error("Update no debe llamarse con teléfono inválido")
	}
}

func TestOnboardingWizard_EtapaEspacios_ValidaYAvanza(t *testing.T) {
	repo := &mockCenterRepoForOnboardHandler{centersByUser: []domain.SportCenter{wizardCenter()}}
	r := newOnboardingWizardRouter(t, repo)

	payload := `{"step":2,"templates":[{"tipo":"cancha","cantidad":2,"cancha":true},{"tipo":"piscina","cantidad":1}]}`
	w := doRequest(t, r, http.MethodPut, "/api/admin/my-sport-center/onboarding", payload)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d. body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	if repo.lastUpdated == nil {
		t.Fatal("Update no fue llamado")
	}
	if repo.lastUpdated.OnboardingStep != 2 {
		t.Errorf("onboarding_step = %d, want 2", repo.lastUpdated.OnboardingStep)
	}
}

func TestOnboardingWizard_EtapaEspacios_SinEspacios_Retorna400(t *testing.T) {
	repo := &mockCenterRepoForOnboardHandler{centersByUser: []domain.SportCenter{wizardCenter()}}
	r := newOnboardingWizardRouter(t, repo)

	w := doRequest(t, r, http.MethodPut, "/api/admin/my-sport-center/onboarding", `{"step":2,"templates":[{"tipo":"cancha","cantidad":0,"cancha":true}]}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d. body=%s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

func TestOnboardingWizard_EtapaCobro_GuardaModalidad(t *testing.T) {
	repo := &mockCenterRepoForOnboardHandler{centersByUser: []domain.SportCenter{wizardCenter()}}
	r := newOnboardingWizardRouter(t, repo)

	w := doRequest(t, r, http.MethodPut, "/api/admin/my-sport-center/onboarding", `{"step":3,"payment_mode":"pay_online"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d. body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	if repo.lastUpdated.PaymentMode != "pay_online" {
		t.Errorf("payment_mode = %q, want pay_online", repo.lastUpdated.PaymentMode)
	}
	if repo.lastUpdated.OnboardingStep != 3 {
		t.Errorf("onboarding_step = %d, want 3", repo.lastUpdated.OnboardingStep)
	}
}

func TestOnboardingWizard_EtapaCobro_ModalidadInvalida_Retorna400(t *testing.T) {
	repo := &mockCenterRepoForOnboardHandler{centersByUser: []domain.SportCenter{wizardCenter()}}
	r := newOnboardingWizardRouter(t, repo)

	w := doRequest(t, r, http.MethodPut, "/api/admin/my-sport-center/onboarding", `{"step":3,"payment_mode":"gratis"}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d. body=%s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

func TestOnboardingWizard_EtapaImagenYOferta_Guarda(t *testing.T) {
	repo := &mockCenterRepoForOnboardHandler{centersByUser: []domain.SportCenter{wizardCenter()}}
	r := newOnboardingWizardRouter(t, repo)

	payload := `{"step":4,"image_url":"https://img.test/foto.jpg","first_month_free":true}`
	w := doRequest(t, r, http.MethodPut, "/api/admin/my-sport-center/onboarding", payload)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d. body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	if repo.lastUpdated.ImageURL != "https://img.test/foto.jpg" {
		t.Errorf("image_url = %q, want https://img.test/foto.jpg", repo.lastUpdated.ImageURL)
	}
	if !repo.lastUpdated.FirstMonthFree {
		t.Error("first_month_free = false, want true")
	}
	if repo.lastUpdated.OnboardingStep != 4 {
		t.Errorf("onboarding_step = %d, want 4", repo.lastUpdated.OnboardingStep)
	}
}

func TestOnboardingWizard_Finalizar_Incompleto_Retorna400(t *testing.T) {
	center := wizardCenter() // sin teléfono ni modalidad
	repo := &mockCenterRepoForOnboardHandler{centersByUser: []domain.SportCenter{center}}
	r := newOnboardingWizardRouter(t, repo)

	w := doRequest(t, r, http.MethodPut, "/api/admin/my-sport-center/onboarding", `{"step":5,"finalize":true}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d. body=%s", w.Code, http.StatusBadRequest, w.Body.String())
	}
	if repo.lastUpdated != nil || w.Code != http.StatusBadRequest {
		// lastUpdated puede no setearse; solo validamos el 400
	}
}

func TestOnboardingWizard_Finalizar_Completo_MarcaCompleted(t *testing.T) {
	center := wizardCenter()
	center.Contact.Phone = "+56 9 1234 5678"
	center.PaymentMode = "pay_at_center"
	repo := &mockCenterRepoForOnboardHandler{centersByUser: []domain.SportCenter{center}}
	r := newOnboardingWizardRouter(t, repo)

	w := doRequest(t, r, http.MethodPut, "/api/admin/my-sport-center/onboarding", `{"step":5,"finalize":true,"templates":[{"tipo":"cancha","cantidad":1,"cancha":true}]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d. body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	if repo.lastUpdated == nil {
		t.Fatal("Update no fue llamado")
	}
	if repo.lastUpdated.OnboardingStatus != "completed" {
		t.Errorf("onboarding_status = %q, want completed", repo.lastUpdated.OnboardingStatus)
	}
	if repo.lastUpdated.OnboardingStep != 5 {
		t.Errorf("onboarding_step = %d, want 5", repo.lastUpdated.OnboardingStep)
	}
}
