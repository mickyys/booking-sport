package app

// Tests unitarios del flujo de onboarding (RES-22 / subtarea RES-36).
//
// Cobertura:
//   - validateRUT / cleanRUT (validación RUT chileno, módulo 11)
//   - generateSlug / replaceAccents
//   - TipoEspacio.IsValid
//   - CreateSportCenterOnboard (con mock local de SportCenterRepository)
//
// NOTA QA: al momento de escribir estos tests, validateRUT rechaza TODOS los
// RUT (ver BUG-1 en el reporte): cleanRUT elimina el guión y luego
// strings.Split(rut, "-") exige exactamente 2 partes, lo que es imposible de
// satisfacer. Los casos marcados como válidos expresan el comportamiento
// correcto según el criterio de aceptación y fallarán mientras BUG-1 exista.

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// ---------- Mock SportCenterRepository para onboarding ----------

type mockCenterRepoForOnboarding struct {
	findBySlugFn       func(ctx context.Context, slug string) (*domain.SportCenter, error)
	findByNameAndRUTFn func(ctx context.Context, name, rut string) (*domain.SportCenter, error)
	findByAdminEmailFn func(ctx context.Context, email string) (*domain.SportCenter, error)
	createFn           func(ctx context.Context, center *domain.SportCenter) error

	createCalls int
	lastCreated *domain.SportCenter
	slugLookups []string
}

func (m *mockCenterRepoForOnboarding) Create(ctx context.Context, center *domain.SportCenter) error {
	m.createCalls++
	m.lastCreated = center
	if m.createFn != nil {
		return m.createFn(ctx, center)
	}
	return nil
}

func (m *mockCenterRepoForOnboarding) Update(ctx context.Context, center *domain.SportCenter) error {
	return nil
}

func (m *mockCenterRepoForOnboarding) UpdateSettings(ctx context.Context, id primitive.ObjectID, slug *string, cancellationHours *int, retentionPercent *int, partialPaymentEnabled *bool, partialPaymentPercent *int, imageURL *string, marketplaceFeePercent *int, isFeatured *bool, branding *domain.SportCenterBranding, address *string, city *string, coordinates *domain.Coordinates) error {
	return nil
}

func (m *mockCenterRepoForOnboarding) FindByID(ctx context.Context, id primitive.ObjectID) (*domain.SportCenter, error) {
	return nil, nil
}

func (m *mockCenterRepoForOnboarding) FindBySlug(ctx context.Context, slug string) (*domain.SportCenter, error) {
	m.slugLookups = append(m.slugLookups, slug)
	if m.findBySlugFn != nil {
		return m.findBySlugFn(ctx, slug)
	}
	return nil, nil
}

func (m *mockCenterRepoForOnboarding) FindBySubdomain(ctx context.Context, subdomain string) (*domain.SportCenter, error) {
	return nil, nil
}

func (m *mockCenterRepoForOnboarding) FindByNameAndRUT(ctx context.Context, name, rut string) (*domain.SportCenter, error) {
	if m.findByNameAndRUTFn != nil {
		return m.findByNameAndRUTFn(ctx, name, rut)
	}
	return nil, nil
}

func (m *mockCenterRepoForOnboarding) FindByAdminEmail(ctx context.Context, email string) (*domain.SportCenter, error) {
	if m.findByAdminEmailFn != nil {
		return m.findByAdminEmailFn(ctx, email)
	}
	return nil, nil
}

func (m *mockCenterRepoForOnboarding) FindByHost(ctx context.Context, host string) (*domain.SportCenter, error) {
	return nil, nil
}

func (m *mockCenterRepoForOnboarding) FindAll(ctx context.Context) ([]domain.SportCenter, error) {
	return nil, nil
}

func (m *mockCenterRepoForOnboarding) FindPaged(ctx context.Context, page, limit int, name, city string, date *time.Time, hour *int, lat, lng *float64, priorityCity string) ([]domain.SportCenter, int64, error) {
	return nil, 0, nil
}

func (m *mockCenterRepoForOnboarding) FindByUserID(ctx context.Context, userID string) ([]domain.SportCenter, error) {
	return nil, nil
}

func (m *mockCenterRepoForOnboarding) GetCities(ctx context.Context) ([]string, error) {
	return nil, nil
}

func (m *mockCenterRepoForOnboarding) UpdateMercadoPagoConfig(ctx context.Context, id primitive.ObjectID, config *domain.MercadoPagoConfig) error {
	return nil
}

var _ SportCenterRepository = (*mockCenterRepoForOnboarding)(nil)

// newOnboardTestUseCase construye el usecase con el mock.
func newOnboardTestUseCase(t *testing.T, m *mockCenterRepoForOnboarding) *SportCenterUseCase {
	t.Helper()
	return NewSportCenterUseCase(m, nil, nil, nil, nil, nil)
}

// ---------- validateRUT ----------

// TestValidateRUT_Casos valida el dígito verificador módulo 11 estándar chileno.
// Valores de DV esperados verificados contra implementación de referencia
// (derecha→izquierda, factores 2..7 cíclicos), misma que usa el frontend
// (booking-sport-ui/src/utils/rut.ts).
func TestValidateRUT_Casos(t *testing.T) {
	cases := []struct {
		name string
		rut  string
		want bool
		nota string
	}{
		{"dv valido basico", "11.111.111-1", true, ""},
		{"dv valido 8 digitos", "12.345.678-5", true, ""},
		{"dv real de 76214473 es 5", "76214473-5", true, "el ejemplo del ticket dice -6, pero el DV módulo 11 real es 5"},
		{"dv real de 14271480 es 9", "14.271.480-9", true, ""},
		{"dv K valido", "6000000-K", true, "cuerpo 6000000 produce DV=K"},
		{"dv k minuscula se normaliza", "6000000-k", true, ""},
		{"normalizacion espacios", "11 111 111-1", true, "debe aceptar tras limpiar espacios"},
		{"normalizacion completa", "11.111.111 - 1", true, "puntos, guion y espacios"},
		{"dv incorrecto", "11.111.111-2", false, ""},
		{"ejemplo ticket dv equivocado", "76214473-6", false, "DV real es 5; discrepancia de especificacion reportada"},
		{"ejemplo ticket K equivocado", "14.271.480-K", false, "DV real es 9"},
		{"vacío", "", false, ""},
		{"basura", "abc", false, ""},
		{"sin dv", "11111111-", false, ""},
		{"letra en cuerpo", "12.345.67A-8", false, ""},
		{"cuerpo demasiado corto", "6-K", false, "un RUT real tiene >=7 dígitos de cuerpo; el frontend exige 7-8 (rut.ts regex \\d{7,8})"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := validateRUT(tc.rut)
			if got != tc.want {
				t.Errorf("validateRUT(%q) = %v, want %v %s", tc.rut, got, tc.want, tc.nota)
			}
		})
	}
}

// ---------- cleanRUT ----------

func TestCleanRUT_Casos(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"12.345.678-5", "123456785"},
		{"11 111 111-1", "111111111"},
		{"  7.654.321-K  ", "7654321K"},
		{"abc", "abc"}, // solo remueve '.', '-' y ' '
		{"", ""},
	}
	for _, tc := range cases {
		if got := cleanRUT(tc.in); got != tc.want {
			t.Errorf("cleanRUT(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// ---------- generateSlug ----------

func TestGenerateSlug_Casos(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"acentos y enie", "Centro Deportivo Ñuñoa", "centro-deportivo-nunoa"},
		{"nombre compuesto", "Complejo Deportivo Los Andes", "complejo-deportivo-los-andes"},
		{"mayusculas acentuadas", "ÁÉÍÓÚ áéíóú", "aeiou-aeiou"},
		{"enie y tilde", "Ñandú Park Ñ", "nandu-park-n"},
		{"espacios multiples y trim", "  Hola   Mundo  ", "hola-mundo"},
		{"guiones colapsados", "Foo--Bar__Baz", "foo-bar-baz"},
		{"trim de guiones", "---x---", "x"},
		{"signos de puntuacion", "¡Hola! ¿Qué tal?", "hola-que-tal"},
		{"nombre vacío", "", ""},
		{"solo espacios", "   ", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := generateSlug(tc.in); got != tc.want {
				t.Errorf("generateSlug(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// ---------- replaceAccents ----------

func TestReplaceAccents_Casos(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"ñuñoa", "nunoa"},
		{"ÁRBOL", "ARBOL"},
		{"murciélago", "murcielago"},
		{"camión-ultramar", "camion-ultramar"},
		{"xyz-123", "xyz-123"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := replaceAccents(tc.in); got != tc.want {
			t.Errorf("replaceAccents(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// ---------- TipoEspacio.IsValid ----------

func TestTipoEspacio_IsValid(t *testing.T) {
	cases := []struct {
		name string
		in   domain.TipoEspacio
		want bool
	}{
		{"cancha", domain.TipoEspacioCancha, true},
		{"quincho", domain.TipoEspacioQuincho, true},
		{"sede", domain.TipoEspacioSede, true},
		{"piscina", domain.TipoEspacioPiscina, true},
		{"vacío", domain.TipoEspacio(""), false},
		{"mayusculas no permitidas", domain.TipoEspacio("CANCHA"), false},
		{"valor desconocido", domain.TipoEspacio("gimnasio"), false},
		{"plural no permitido", domain.TipoEspacio("canchas"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.in.IsValid(); got != tc.want {
				t.Errorf("TipoEspacio(%q).IsValid() = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// ---------- CreateSportCenterOnboard ----------

func TestCreateSportCenterOnboard_HappyPath_CreaCentroConSlugAutogenerado(t *testing.T) {
	m := &mockCenterRepoForOnboarding{}
	uc := newOnboardTestUseCase(t, m)

	center, err := uc.CreateSportCenterOnboard(context.Background(),
		"Club Andino Ñuñoa", "12.345.678-5", "Av. Ossa 123, La Reina", []string{"cancha"},
		"", "admin@club.cl", "auth0|123",
		"La Reina", -33.4383, -70.7511)

	if err != nil {
		t.Fatalf("CreateSportCenterOnboard retornó error = %v, want nil", err)
	}
	if center == nil {
		t.Fatal("CreateSportCenterOnboard retornó centro nil, want no-nil")
	}
	if center.Slug != "club-andino-nunoa" {
		t.Errorf("slug autogenerado = %q, want %q", center.Slug, "club-andino-nunoa")
	}
	if center.Rut != "12.345.678-5" {
		t.Errorf("rut = %q, want %q (passthrough)", center.Rut, "12.345.678-5")
	}
	if len(center.TiposEspacio) != 1 || center.TiposEspacio[0] != "cancha" {
		t.Errorf("tipos_espacios = %v, want [cancha]", center.TiposEspacio)
	}
	if center.AdminEmail != "admin@club.cl" {
		t.Errorf("admin_email = %q, want %q", center.AdminEmail, "admin@club.cl")
	}
	if len(center.Users) != 1 || center.Users[0] != "auth0|123" {
		t.Errorf("users = %v, want [auth0|123]", center.Users)
	}
	if center.Name != "Club Andino Ñuñoa" {
		t.Errorf("name = %q, want %q", center.Name, "Club Andino Ñuñoa")
	}
	if center.CancellationHours != 24 {
		t.Errorf("CancellationHours default = %d, want 24", center.CancellationHours)
	}
	if center.RetentionPercent != 10 {
		t.Errorf("RetentionPercent default = %d, want 10", center.RetentionPercent)
	}
	if m.createCalls != 1 {
		t.Errorf("repo.Create llamado %d veces, want 1", m.createCalls)
	}
	// Ciudad y coordenadas llegan resueltas desde el frontend: passthrough.
	if center.City != "La Reina" {
		t.Errorf("City = %q, want %q", center.City, "La Reina")
	}
	if center.Coordinates.Lat != -33.4383 || center.Coordinates.Lng != -70.7511 {
		t.Errorf("Coordinates = %+v, want {lat:-33.4383 lng:-70.7511}", center.Coordinates)
	}
	if center.Location == nil {
		t.Fatal("Location = nil, want punto GeoJSON")
	}
	if center.Location.Type != "Point" {
		t.Errorf("Location.Type = %q, want Point", center.Location.Type)
	}
	if center.Location.Coordinates[0] != -70.7511 || center.Location.Coordinates[1] != -33.4383 {
		t.Errorf("Location.Coordinates = %v, want [lng lat] = [-70.7511 -33.4383]", center.Location.Coordinates)
	}
}

// TestCreateSportCenterOnboard_SinCoordenadas_GuardaSinUbicacion verifica que
// si el frontend no envía coordenadas, el centro se crea con la ciudad pero
// sin Coordinates ni Location (0/0 significa "sin ubicación").
func TestCreateSportCenterOnboard_SinCoordenadas_GuardaSinUbicacion(t *testing.T) {
	m := &mockCenterRepoForOnboarding{}
	uc := newOnboardTestUseCase(t, m)

	center, err := uc.CreateSportCenterOnboard(context.Background(),
		"Club Sin Mapa", "12.345.678-5", "Dirección 1", []string{"cancha"},
		"", "admin@club.cl", "",
		"Providencia", 0, 0)

	if err != nil {
		t.Fatalf("CreateSportCenterOnboard retornó error = %v, want nil", err)
	}
	if center.City != "Providencia" {
		t.Errorf("City = %q, want %q", center.City, "Providencia")
	}
	if center.Coordinates.Lat != 0 || center.Coordinates.Lng != 0 {
		t.Errorf("Coordinates = %+v, want cero", center.Coordinates)
	}
	if center.Location != nil {
		t.Errorf("Location = %+v, want nil", center.Location)
	}
}

func TestCreateSportCenterOnboard_SlugDuplicado_RetornaErrorSlugEnUso(t *testing.T) {
	existing := &domain.SportCenter{ID: primitive.NewObjectID(), Slug: "club-andino-nunoa"}
	m := &mockCenterRepoForOnboarding{
		findBySlugFn: func(ctx context.Context, slug string) (*domain.SportCenter, error) {
			return existing, nil // slug ya ocupado
		},
	}
	uc := newOnboardTestUseCase(t, m)

	center, err := uc.CreateSportCenterOnboard(context.Background(),
		"Otro Club", "12.345.678-5", "Dirección 1", []string{"quincho"},
		"", "otro@club.cl", "", "", 0, 0)

	if err == nil {
		t.Fatal("CreateSportCenterOnboard con slug duplicado retornó nil, want error")
	}
	if !strings.Contains(err.Error(), "ya está en uso") || !strings.Contains(err.Error(), "slug") {
		t.Errorf("error = %q, want mensaje de slug en uso (handler lo mapea a 409)", err.Error())
	}
	if center != nil {
		t.Errorf("centro retornado = %+v, want nil en error", center)
	}
	if m.createCalls != 0 {
		t.Errorf("repo.Create llamado %d veces, want 0 (no debe crear)", m.createCalls)
	}
}

func TestCreateSportCenterOnboard_NombreRUTDuplicado_RetornaErrorDuplicado(t *testing.T) {
	existing := &domain.SportCenter{ID: primitive.NewObjectID(), Name: "Club Andino Ñuñoa", Rut: "12.345.678-5"}
	m := &mockCenterRepoForOnboarding{
		findByNameAndRUTFn: func(ctx context.Context, name, rut string) (*domain.SportCenter, error) {
			return existing, nil // combinación nombre+RUT ya existe
		},
	}
	uc := newOnboardTestUseCase(t, m)

	center, err := uc.CreateSportCenterOnboard(context.Background(),
		"Club Andino Ñuñoa", "12.345.678-5", "Dirección 1", []string{"sede"},
		"", "admin@club.cl", "", "", 0, 0)

	if err == nil {
		t.Fatal("CreateSportCenterOnboard con nombre+RUT duplicados retornó nil, want error")
	}
	if !strings.Contains(err.Error(), "ya existe un centro deportivo") {
		t.Errorf("error = %q, want mensaje de duplicado nombre+RUT (handler lo mapea a 422)", err.Error())
	}
	if center != nil {
		t.Errorf("centro retornado = %+v, want nil", center)
	}
	if m.createCalls != 0 {
		t.Errorf("repo.Create llamado %d veces, want 0", m.createCalls)
	}
}

func TestCreateSportCenterOnboard_RUTInvalido_Rechazado(t *testing.T) {
	cases := []struct {
		name string
		rut  string
	}{
		{"dv incorrecto", "12.345.678-9"},
		{"basura", "no-es-un-rut"},
		{"vacío", ""},
		{"solo puntos", "..."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := &mockCenterRepoForOnboarding{}
			uc := newOnboardTestUseCase(t, m)

			center, err := uc.CreateSportCenterOnboard(context.Background(),
				"Club X", tc.rut, "Dirección 1", []string{"cancha"},
				"", "admin@club.cl", "", "", 0, 0)

			if err == nil {
				t.Fatalf("CreateSportCenterOnboard con RUT %q retornó nil, want error", tc.rut)
			}
			if !strings.Contains(err.Error(), "RUT inválido") {
				t.Errorf("error = %q, want mensaje 'RUT inválido' claro", err.Error())
			}
			if center != nil {
				t.Errorf("centro retornado = %+v, want nil", center)
			}
			if m.createCalls != 0 {
				t.Errorf("repo.Create llamado %d veces, want 0", m.createCalls)
			}
			if len(m.slugLookups) != 0 {
				t.Errorf("se consultó FindBySlug %d veces con RUT inválido, want 0 (rechazo temprano)", len(m.slugLookups))
			}
		})
	}
}

func TestCreateSportCenterOnboard_TiposEspacioInvalidos_Rechazados(t *testing.T) {
	cases := []struct {
		name          string
		tiposEspacios []string
	}{
		{"elemento desconocido", []string{"cancha", "gimnasio"}},
		{"solo desconocido", []string{"gimnasio"}},
		{"array vacío", []string{}},
		{"array nil", nil},
		{"mayusculas", []string{"CANCHA"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := &mockCenterRepoForOnboarding{}
			uc := newOnboardTestUseCase(t, m)

			center, err := uc.CreateSportCenterOnboard(context.Background(),
				"Club Y", "12.345.678-5", "Dirección 1", tc.tiposEspacios,
				"", "admin@club.cl", "", "", 0, 0)

			if err == nil {
				t.Fatalf("CreateSportCenterOnboard con tipo_espacios %v retornó nil, want error", tc.tiposEspacios)
			}
			if !strings.Contains(err.Error(), "tipo_espacios inválido") {
				t.Errorf("error = %q, want mensaje 'tipo_espacios inválido'", err.Error())
			}
			if center != nil {
				t.Errorf("centro retornado = %+v, want nil", center)
			}
			if m.createCalls != 0 {
				t.Errorf("repo.Create llamado %d veces, want 0", m.createCalls)
			}
		})
	}
}

func TestCreateSportCenterOnboard_MultiplesTiposValidos_DedupePreservaOrden(t *testing.T) {
	m := &mockCenterRepoForOnboarding{}
	uc := newOnboardTestUseCase(t, m)

	center, err := uc.CreateSportCenterOnboard(context.Background(),
		"Club Multi", "12.345.678-5", "Dirección 1", []string{"cancha", "cancha", "quincho"},
		"", "admin@club.cl", "", "", 0, 0)

	if err != nil {
		t.Fatalf("CreateSportCenterOnboard retornó error = %v, want nil", err)
	}
	got := center.TiposEspacio
	want := []string{"cancha", "quincho"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("tipos_espacios persistidos = %v, want %v (dedupeado preservando orden)", got, want)
	}
}

func TestCreateSportCenterOnboard_EmailNormalizadoYGuardado(t *testing.T) {
	m := &mockCenterRepoForOnboarding{}
	uc := newOnboardTestUseCase(t, m)

	var queriedEmail string
	m.findByAdminEmailFn = func(ctx context.Context, email string) (*domain.SportCenter, error) {
		queriedEmail = email
		return nil, mongo.ErrNoDocuments // disponible
	}

	center, err := uc.CreateSportCenterOnboard(context.Background(),
		"Club Email", "12.345.678-5", "Dirección 1", []string{"piscina"},
		"", "  Admin@CLUB.CL ", "", "", 0, 0)

	if err != nil {
		t.Fatalf("CreateSportCenterOnboard retornó error = %v, want nil", err)
	}
	if center.AdminEmail != "admin@club.cl" {
		t.Errorf("admin_email guardado = %q, want normalizado %q", center.AdminEmail, "admin@club.cl")
	}
	if queriedEmail != "" && queriedEmail != "admin@club.cl" {
		t.Errorf("lookup de unicidad usó email %q, want normalizado %q", queriedEmail, "admin@club.cl")
	}
}

func TestCreateSportCenterOnboard_EmailDuplicado_RetornaErrorCorreoYaRegistrado(t *testing.T) {
	existing := &domain.SportCenter{ID: primitive.NewObjectID(), AdminEmail: "admin@club.cl"}
	m := &mockCenterRepoForOnboarding{
		findByAdminEmailFn: func(ctx context.Context, email string) (*domain.SportCenter, error) {
			return existing, nil
		},
	}
	uc := newOnboardTestUseCase(t, m)

	center, err := uc.CreateSportCenterOnboard(context.Background(),
		"Otro Club", "11.111.111-1", "Dirección 2", []string{"cancha"},
		"", "admin@club.cl", "", "", 0, 0)

	if err == nil {
		t.Fatal("CreateSportCenterOnboard con correo duplicado retornó nil, want error")
	}
	if !strings.Contains(err.Error(), "correo ya registrado") {
		t.Errorf("error = %q, want mensaje 'correo ya registrado' (handler lo mapea a 422)", err.Error())
	}
	if center != nil {
		t.Errorf("centro retornado = %+v, want nil", center)
	}
	if m.createCalls != 0 {
		t.Errorf("repo.Create llamado %d veces, want 0", m.createCalls)
	}
}

func TestCreateSportCenterOnboard_EmailInvalido_Rechazado(t *testing.T) {
	cases := []struct {
		name  string
		email string
	}{
		{"sin arroba", "admin.club.cl"},
		{"sin tld", "admin@club"},
		{"con espacio", "ad min@club.cl"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := &mockCenterRepoForOnboarding{}
			uc := newOnboardTestUseCase(t, m)

			center, err := uc.CreateSportCenterOnboard(context.Background(),
				"Club Mail", "12.345.678-5", "Dirección 1", []string{"cancha"},
				"", tc.email, "", "", 0, 0)

			if err == nil {
				t.Fatalf("CreateSportCenterOnboard con email %q retornó nil, want error", tc.email)
			}
			if !strings.Contains(err.Error(), "correo inválido") {
				t.Errorf("error = %q, want mensaje 'correo inválido'", err.Error())
			}
			if center != nil {
				t.Errorf("centro retornado = %+v, want nil", center)
			}
			if m.createCalls != 0 {
				t.Errorf("repo.Create llamado %d veces, want 0", m.createCalls)
			}
		})
	}
}

func TestCreateSportCenterOnboard_SlugManual_SeRespeta(t *testing.T) {
	m := &mockCenterRepoForOnboarding{}
	uc := newOnboardTestUseCase(t, m)

	center, err := uc.CreateSportCenterOnboard(context.Background(),
		"Club Con Nombre Largo Para Slug", "12.345.678-5", "Dirección 1", []string{"piscina"},
		"mi-club-custom", "admin@club.cl", "", "", 0, 0)

	if err != nil {
		t.Fatalf("CreateSportCenterOnboard retornó error = %v, want nil", err)
	}
	if center.Slug != "mi-club-custom" {
		t.Errorf("slug = %q, want slug manual %q (no autogenerado)", center.Slug, "mi-club-custom")
	}
	if len(m.slugLookups) != 1 || m.slugLookups[0] != "mi-club-custom" {
		t.Errorf("lookups de slug = %v, want [%q]", m.slugLookups, "mi-club-custom")
	}
}

func TestCreateSportCenterOnboard_ErroresDeRepositorio(t *testing.T) {
	errBoom := fmt.Errorf("db down") // error real de infraestructura, distinto de ErrNoDocuments

	t.Run("error lookup slug", func(t *testing.T) {
		m := &mockCenterRepoForOnboarding{
			findBySlugFn: func(ctx context.Context, slug string) (*domain.SportCenter, error) {
				return nil, errBoom
			},
		}
		uc := newOnboardTestUseCase(t, m)
		center, err := uc.CreateSportCenterOnboard(context.Background(),
			"Club Z", "12.345.678-5", "Dir", []string{"cancha"},
			"", "admin@club.cl", "", "", 0, 0)
		if err == nil || !strings.Contains(err.Error(), "error al verificar slug único") {
			t.Errorf("error = %v, want contener 'error al verificar slug único'", err)
		}
		if center != nil {
			t.Errorf("centro = %+v, want nil", center)
		}
		if m.createCalls != 0 {
			t.Errorf("repo.Create llamado %d veces, want 0", m.createCalls)
		}
	})

	t.Run("error lookup nombre+rut", func(t *testing.T) {
		m := &mockCenterRepoForOnboarding{
			findByNameAndRUTFn: func(ctx context.Context, name, rut string) (*domain.SportCenter, error) {
				return nil, errBoom
			},
		}
		uc := newOnboardTestUseCase(t, m)
		center, err := uc.CreateSportCenterOnboard(context.Background(),
			"Club Z", "12.345.678-5", "Dir", []string{"cancha"},
			"", "admin@club.cl", "", "", 0, 0)
		if err == nil || !strings.Contains(err.Error(), "error al verificar nombre+RUT") {
			t.Errorf("error = %v, want contener 'error al verificar nombre+RUT'", err)
		}
		if center != nil {
			t.Errorf("centro = %+v, want nil", center)
		}
	})

	t.Run("error lookup admin_email", func(t *testing.T) {
		m := &mockCenterRepoForOnboarding{
			findByAdminEmailFn: func(ctx context.Context, email string) (*domain.SportCenter, error) {
				return nil, errBoom
			},
		}
		uc := newOnboardTestUseCase(t, m)
		center, err := uc.CreateSportCenterOnboard(context.Background(),
			"Club Z", "12.345.678-5", "Dir", []string{"cancha"},
			"", "admin@club.cl", "", "", 0, 0)
		if err == nil || !strings.Contains(err.Error(), "error al verificar correo único") {
			t.Errorf("error = %v, want contener 'error al verificar correo único'", err)
		}
		if center != nil {
			t.Errorf("centro = %+v, want nil", center)
		}
	})

	t.Run("error create", func(t *testing.T) {
		m := &mockCenterRepoForOnboarding{
			createFn: func(ctx context.Context, center *domain.SportCenter) error {
				return errBoom
			},
		}
		uc := newOnboardTestUseCase(t, m)
		center, err := uc.CreateSportCenterOnboard(context.Background(),
			"Club Z", "12.345.678-5", "Dir", []string{"cancha"},
			"", "admin@club.cl", "", "", 0, 0)
		if err == nil || !strings.Contains(err.Error(), "error al crear centro deportivo") {
			t.Errorf("error = %v, want contener 'error al crear centro deportivo'", err)
		}
		if center != nil {
			t.Errorf("centro = %+v, want nil", center)
		}
	})
}

// TestCreateSportCenterOnboard_ErrNoDocuments_SeTrataComoDisponible es la
// regresión del fix crítico RES-37: con Mongo real, slug/email libres devuelven
// mongo.ErrNoDocuments y NO deben producir un error falso.
func TestCreateSportCenterOnboard_ErrNoDocuments_SeTrataComoDisponible(t *testing.T) {
	m := &mockCenterRepoForOnboarding{
		findBySlugFn: func(ctx context.Context, slug string) (*domain.SportCenter, error) {
			return nil, mongo.ErrNoDocuments // slug libre en Mongo real
		},
		findByNameAndRUTFn: func(ctx context.Context, name, rut string) (*domain.SportCenter, error) {
			return nil, mongo.ErrNoDocuments // combinación libre
		},
		findByAdminEmailFn: func(ctx context.Context, email string) (*domain.SportCenter, error) {
			return nil, mongo.ErrNoDocuments // correo disponible
		},
	}
	uc := newOnboardTestUseCase(t, m)

	center, err := uc.CreateSportCenterOnboard(context.Background(),
		"Club Nuevo", "12.345.678-5", "Dirección 1", []string{"cancha"},
		"", "nuevo@club.cl", "", "", 0, 0)

	if err != nil {
		t.Fatalf("CreateSportCenterOnboard con ErrNoDocuments retornó error = %v, want nil (not-found ≠ fallo)", err)
	}
	if center == nil {
		t.Fatal("CreateSportCenterOnboard retornó centro nil, want no-nil")
	}
	if m.createCalls != 1 {
		t.Errorf("repo.Create llamado %d veces, want 1", m.createCalls)
	}
}

// ---------- TipoEspaciosValidos ----------

func TestTipoEspaciosValidos_Casos(t *testing.T) {
	cases := []struct {
		name      string
		in        []string
		want      []string
		wantValid bool
	}{
		{"multiples validos", []string{"cancha", "quincho"}, []string{"cancha", "quincho"}, true},
		{"duplicados preservan orden", []string{"quincho", "cancha", "quincho"}, []string{"quincho", "cancha"}, true},
		{"un solo valor", []string{"sede"}, []string{"sede"}, true},
		{"elemento invalido", []string{"cancha", "gimnasio"}, nil, false},
		{"array vacio", []string{}, []string{}, true},
		{"nil", nil, []string{}, true},
		{"mayusculas invalidas", []string{"Piscina"}, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := domain.TipoEspaciosValidos(tc.in)
			if ok != tc.wantValid {
				t.Fatalf("TipoEspaciosValidos(%v) ok = %v, want %v", tc.in, ok, tc.wantValid)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("TipoEspaciosValidos(%v) = %v, want %v", tc.in, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("TipoEspaciosValidos(%v)[%d] = %q, want %q", tc.in, i, got[i], tc.want[i])
				}
			}
		})
	}
}
