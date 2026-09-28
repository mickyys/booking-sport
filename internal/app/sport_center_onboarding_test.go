package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// ---------- Mock para el wizard ----------

type mockOnboardingRepo struct {
	SportCenterRepository // hereda los métodos no usados del wizard
	centers    []domain.SportCenter
	lastUpdate *domain.SportCenter
	updateErr  error
}

func (m *mockOnboardingRepo) FindByUserID(ctx context.Context, userID string) ([]domain.SportCenter, error) {
	return m.centers, nil
}

func (m *mockOnboardingRepo) Update(ctx context.Context, center *domain.SportCenter) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	m.lastUpdate = center
	return nil
}

// mockSpaceRepo captura los espacios materializados por el wizard.
type mockSpaceRepo struct {
	created []*domain.Space
}

func (m *mockSpaceRepo) Create(ctx context.Context, space *domain.Space) error {
	if space.ID.IsZero() {
		space.ID = primitive.NewObjectID()
	}
	m.created = append(m.created, space)
	return nil
}
func (m *mockSpaceRepo) FindByID(ctx context.Context, id primitive.ObjectID) (*domain.Space, error) { return nil, nil }
func (m *mockSpaceRepo) FindByCenterID(ctx context.Context, centerID primitive.ObjectID) ([]domain.Space, error) {
	out := make([]domain.Space, 0, len(m.created))
	for _, s := range m.created {
		out = append(out, *s)
	}
	return out, nil
}
func (m *mockSpaceRepo) FindByCenterAndCategory(ctx context.Context, centerID primitive.ObjectID, category domain.SpaceCategory) ([]domain.Space, error) {
	return nil, nil
}
func (m *mockSpaceRepo) Update(ctx context.Context, space *domain.Space) error { return nil }
func (m *mockSpaceRepo) Delete(ctx context.Context, id primitive.ObjectID) error { return nil }
func (m *mockSpaceRepo) UpdateScheduleSlot(ctx context.Context, id primitive.ObjectID, slot domain.CourtSchedule) error {
	return nil
}
func (m *mockSpaceRepo) SyncPartialPaymentSlots(ctx context.Context, centerID primitive.ObjectID, partialPaymentEnabled bool) (int64, error) {
	return 0, nil
}

// mockRuleRepo captura las reglas de horario materializadas por el wizard.
type mockRuleRepo struct {
	created []*domain.ScheduleRule
}

func (m *mockRuleRepo) Create(ctx context.Context, rule *domain.ScheduleRule) error {
	if rule.ID.IsZero() {
		rule.ID = primitive.NewObjectID()
	}
	m.created = append(m.created, rule)
	return nil
}
func (m *mockRuleRepo) Update(ctx context.Context, rule *domain.ScheduleRule) error { return nil }
func (m *mockRuleRepo) FindByID(ctx context.Context, id primitive.ObjectID) (*domain.ScheduleRule, error) {
	return nil, nil
}
func (m *mockRuleRepo) FindBySpaceID(ctx context.Context, spaceID primitive.ObjectID) ([]domain.ScheduleRule, error) {
	return nil, nil
}
func (m *mockRuleRepo) FindBySportCenterID(ctx context.Context, centerID primitive.ObjectID) ([]domain.ScheduleRule, error) {
	return nil, nil
}
func (m *mockRuleRepo) Delete(ctx context.Context, id primitive.ObjectID) error { return nil }
func (m *mockRuleRepo) DeleteBySpaceID(ctx context.Context, spaceID primitive.ObjectID) error { return nil }

func onboardingCenter() domain.SportCenter {
	return domain.SportCenter{
		ID:               primitive.NewObjectID(),
		Name:             "Club Wizard",
		AdminEmail:       "admin@club.cl",
		Users:            []string{"auth0|wizard"},
		OnboardingStatus: "pending",
	}
}

// onboardingUCWithRepo devuelve un usecase cuyo repo de centros es el mock
// especializado del wizard (FindByUserID + Update) con repos de espacios y
// reglas en memoria para la materialización.
func onboardingUCWithRepo(repo *mockOnboardingRepo) *SportCenterUseCase {
	return &SportCenterUseCase{repo: repo, spaceRepo: &mockSpaceRepo{}, ruleRepo: &mockRuleRepo{}}
}

func onboardingUCWithRepos(repo *mockOnboardingRepo, spaces *mockSpaceRepo, rules *mockRuleRepo) *SportCenterUseCase {
	return &SportCenterUseCase{repo: repo, spaceRepo: spaces, ruleRepo: rules}
}

// ---------- UpdateSportCenterOnboarding ----------

func TestUpdateOnboarding_Contacto_GuardaTelefonoYAvanza(t *testing.T) {
	repo := &mockOnboardingRepo{centers: []domain.SportCenter{onboardingCenter()}}
	uc := onboardingUCWithRepo(repo)

	phone := "+56 9 1234 5678"
	err := uc.UpdateSportCenterOnboarding(context.Background(), "auth0|wizard", UpdateOnboardingInput{
		Step:     OnboardingStepContacto,
		Telefono: &phone,
	})
	if err != nil {
		t.Fatalf("UpdateSportCenterOnboarding retornó error = %v, want nil", err)
	}
	if repo.lastUpdate == nil {
		t.Fatal("repo.Update no fue llamado")
	}
	if repo.lastUpdate.Contact.Phone != phone {
		t.Errorf("contact.phone = %q, want %q", repo.lastUpdate.Contact.Phone, phone)
	}
	if repo.lastUpdate.Contact.Email != "admin@club.cl" {
		t.Errorf("contact.email = %q, want admin@club.cl", repo.lastUpdate.Contact.Email)
	}
	if repo.lastUpdate.OnboardingStep != OnboardingStepContacto {
		t.Errorf("onboarding_step = %d, want %d", repo.lastUpdate.OnboardingStep, OnboardingStepContacto)
	}
}

func TestUpdateOnboarding_Contacto_TelefonoInvalido_Rechaza(t *testing.T) {
	repo := &mockOnboardingRepo{centers: []domain.SportCenter{onboardingCenter()}}
	uc := onboardingUCWithRepo(repo)

	phone := "123"
	err := uc.UpdateSportCenterOnboarding(context.Background(), "auth0|wizard", UpdateOnboardingInput{
		Step:     OnboardingStepContacto,
		Telefono: &phone,
	})
	if err == nil || !strings.Contains(err.Error(), "teléfono inválido") {
		t.Errorf("error = %v, want 'teléfono inválido'", err)
	}
	if repo.lastUpdate != nil {
		t.Error("repo.Update no debe llamarse con teléfono inválido")
	}
}

func TestUpdateOnboarding_Contacto_SinTelefono_Rechaza(t *testing.T) {
	repo := &mockOnboardingRepo{centers: []domain.SportCenter{onboardingCenter()}}
	uc := onboardingUCWithRepo(repo)

	phone := ""
	err := uc.UpdateSportCenterOnboarding(context.Background(), "auth0|wizard", UpdateOnboardingInput{
		Step:     OnboardingStepContacto,
		Telefono: &phone,
	})
	if err == nil {
		t.Error("teléfono vacío debe ser rechazado")
	}
}

func TestUpdateOnboarding_Espacios_PersisteBorradorYAvanza(t *testing.T) {
	repo := &mockOnboardingRepo{centers: []domain.SportCenter{onboardingCenter()}}
	uc := onboardingUCWithRepo(repo)

	err := uc.UpdateSportCenterOnboarding(context.Background(), "auth0|wizard", UpdateOnboardingInput{
		Step: OnboardingStepEspacios,
		Templates: []domain.SpaceTemplate{
			{Tipo: "cancha", Cantidad: 2, Cancha: true},
			{Tipo: "piscina", Cantidad: 1},
		},
	})
	if err != nil {
		t.Fatalf("UpdateSportCenterOnboarding retornó error = %v, want nil", err)
	}
	if repo.lastUpdate == nil {
		t.Fatal("repo.Update no fue llamado")
	}
	if repo.lastUpdate.OnboardingStep != OnboardingStepEspacios {
		t.Errorf("onboarding_step = %d, want %d", repo.lastUpdate.OnboardingStep, OnboardingStepEspacios)
	}
	// El borrador se persiste para retomar el wizard tras una recarga.
	if len(repo.lastUpdate.SpaceTemplates) != 2 || repo.lastUpdate.SpaceTemplates[0].Cantidad != 2 {
		t.Errorf("space_templates = %v, want cancha:2 y piscina", repo.lastUpdate.SpaceTemplates)
	}
}

func TestUpdateOnboarding_Espacios_Invalidos_Rechaza(t *testing.T) {
	cases := [][]domain.SpaceTemplate{
		{{Tipo: "cancha", Cantidad: 0, Cancha: true}},
		{{Tipo: "gimnasio", Cantidad: 1}},
		{},
	}
	for i, templates := range cases {
		repo := &mockOnboardingRepo{centers: []domain.SportCenter{onboardingCenter()}}
		uc := onboardingUCWithRepo(repo)
		err := uc.UpdateSportCenterOnboarding(context.Background(), "auth0|wizard", UpdateOnboardingInput{
			Step:       OnboardingStepEspacios,
			Templates: templates,
		})
		if err == nil {
			t.Errorf("caso %d: templates %v deben ser rechazadas", i, templates)
		}
		if repo.lastUpdate != nil {
			t.Errorf("caso %d: repo.Update no debe llamarse", i)
		}
	}
}

func TestUpdateOnboarding_Cobro_GuardaModalidad(t *testing.T) {
	for _, mode := range []string{PaymentModePayAtCenter, PaymentModePayOnline} {
		repo := &mockOnboardingRepo{centers: []domain.SportCenter{onboardingCenter()}}
		uc := onboardingUCWithRepo(repo)
		m := mode
		err := uc.UpdateSportCenterOnboarding(context.Background(), "auth0|wizard", UpdateOnboardingInput{
			Step:        OnboardingStepCobro,
			PaymentMode: &m,
		})
		if err != nil {
			t.Fatalf("modalidad %q retornó error = %v", mode, err)
		}
		if repo.lastUpdate.PaymentMode != mode {
			t.Errorf("payment_mode = %q, want %q", repo.lastUpdate.PaymentMode, mode)
		}
	}
}

func TestUpdateOnboarding_Cobro_ModalidadInvalida_Rechaza(t *testing.T) {
	repo := &mockOnboardingRepo{centers: []domain.SportCenter{onboardingCenter()}}
	uc := onboardingUCWithRepo(repo)
	mode := "gratis"
	err := uc.UpdateSportCenterOnboarding(context.Background(), "auth0|wizard", UpdateOnboardingInput{
		Step:        OnboardingStepCobro,
		PaymentMode: &mode,
	})
	if err == nil {
		t.Error("modalidad inválida debe ser rechazada")
	}
}

func TestUpdateOnboarding_ImagenYOferta_Guarda(t *testing.T) {
	repo := &mockOnboardingRepo{centers: []domain.SportCenter{onboardingCenter()}}
	uc := onboardingUCWithRepo(repo)
	image := "https://img.test/foto.jpg"
	offer := true
	err := uc.UpdateSportCenterOnboarding(context.Background(), "auth0|wizard", UpdateOnboardingInput{
		Step:           OnboardingStepImagen,
		ImageURL:       &image,
		FirstMonthFree: &offer,
	})
	if err != nil {
		t.Fatalf("UpdateSportCenterOnboarding retornó error = %v", err)
	}
	if repo.lastUpdate.ImageURL != image {
		t.Errorf("image_url = %q, want %q", repo.lastUpdate.ImageURL, image)
	}
	if !repo.lastUpdate.FirstMonthFree {
		t.Error("first_month_free = false, want true")
	}
}

func TestUpdateOnboarding_SinCentro_Rechaza(t *testing.T) {
	repo := &mockOnboardingRepo{centers: nil}
	uc := onboardingUCWithRepo(repo)
	phone := "+56 9 1234 5678"
	err := uc.UpdateSportCenterOnboarding(context.Background(), "auth0|wizard", UpdateOnboardingInput{
		Step:     OnboardingStepContacto,
		Telefono: &phone,
	})
	if err == nil || !strings.Contains(err.Error(), "no se encontró un centro") {
		t.Errorf("error = %v, want 'no se encontró un centro'", err)
	}
}

func TestUpdateOnboarding_Finalizar_Incompleto_Rechaza(t *testing.T) {
	repo := &mockOnboardingRepo{centers: []domain.SportCenter{onboardingCenter()}}
	uc := onboardingUCWithRepo(repo)
	err := uc.UpdateSportCenterOnboarding(context.Background(), "auth0|wizard", UpdateOnboardingInput{
		Step:     OnboardingStepFinal,
		Finalize: true,
	})
	if err == nil {
		t.Fatal("finalizar sin teléfono ni modalidad debe fallar")
	}
	if repo.lastUpdate != nil {
		t.Error("repo.Update no debe llamarse al finalizar incompleto")
	}
}

func TestUpdateOnboarding_Finalizar_Completo_MarcaCompleted(t *testing.T) {
	center := onboardingCenter()
	center.Contact.Phone = "+56 9 1234 5678"
	center.PaymentMode = PaymentModePayAtCenter
	repo := &mockOnboardingRepo{centers: []domain.SportCenter{center}}
	spaces := &mockSpaceRepo{}
	uc := onboardingUCWithRepos(repo, spaces, &mockRuleRepo{})

	err := uc.UpdateSportCenterOnboarding(context.Background(), "auth0|wizard", UpdateOnboardingInput{
		Step:     OnboardingStepFinal,
		Finalize: true,
		Templates: []domain.SpaceTemplate{
			{Tipo: "cancha", Cantidad: 2, Cancha: true},
		},
	})
	if err != nil {
		t.Fatalf("UpdateSportCenterOnboarding retornó error = %v", err)
	}
	if repo.lastUpdate.OnboardingStatus != OnboardingStatusCompleted {
		t.Errorf("onboarding_status = %q, want completed", repo.lastUpdate.OnboardingStatus)
	}
	if repo.lastUpdate.OnboardingStep != OnboardingStepFinal {
		t.Errorf("onboarding_step = %d, want %d", repo.lastUpdate.OnboardingStep, OnboardingStepFinal)
	}
	if !time.Now().After(repo.lastUpdate.UpdatedAt.Add(-time.Minute)) {
		t.Error("updated_at debe actualizarse al guardar")
	}
}

func TestUpdateOnboarding_Finalizar_MaterializaEspaciosYReglas(t *testing.T) {
	center := onboardingCenter()
	center.Contact.Phone = "+56 9 1234 5678"
	center.PaymentMode = PaymentModePayOnline
	repo := &mockOnboardingRepo{centers: []domain.SportCenter{center}}
	spaces := &mockSpaceRepo{}
	rules := &mockRuleRepo{}
	uc := onboardingUCWithRepos(repo, spaces, rules)

	err := uc.UpdateSportCenterOnboarding(context.Background(), "auth0|wizard", UpdateOnboardingInput{
		Step:     OnboardingStepFinal,
		Finalize: true,
		Templates: []domain.SpaceTemplate{
			{
				Tipo:     "cancha",
				Cantidad: 2,
				Cancha:   true,
				Rules: []domain.ScheduleRule{
					{
						DaysOfWeek: []int{1, 2, 3, 4, 5},
						Slots:      []domain.ScheduleRuleSlot{{StartTime: "09:00", EndTime: "12:00", Price: 10000}},
					},
				},
			},
			{Tipo: "quincho", Cantidad: 1, PriceAllDay: 50000, PriceHalfDay: 30000},
			{Tipo: "sede", Cantidad: 1, PricePerDay: 20000, PricePerMonth: 400000},
		},
	})
	if err != nil {
		t.Fatalf("UpdateSportCenterOnboarding retornó error = %v", err)
	}

	// 2 canchas + 1 quincho + 1 sede = 4 espacios en la colección `spaces`.
	if len(spaces.created) != 4 {
		t.Fatalf("espacios materializados = %d, want 4", len(spaces.created))
	}
	names := map[string]bool{}
	for _, s := range spaces.created {
		if s.SportCenterID != center.ID {
			t.Errorf("space.SportCenterID = %v, want %v", s.SportCenterID, center.ID)
		}
		if !s.Enabled {
			t.Errorf("space %s debe estar habilitado", s.Name)
		}
		names[s.Name] = true
	}
	for _, wantName := range []string{"Cancha 1", "Cancha 2", "Quincho 1", "Sede 1"} {
		if !names[wantName] {
			t.Errorf("falta el espacio %q; creados = %v", wantName, names)
		}
	}

	// La plantilla de cancha tenía 1 regla semanal → 2 reglas (una por cancha).
	if len(rules.created) != 2 {
		t.Fatalf("reglas materializadas = %d, want 2", len(rules.created))
	}
	for _, r := range rules.created {
		if r.SpaceID.IsZero() {
			t.Error("regla sin space_id asignado")
		}
		if r.SportCenterID != center.ID {
			t.Errorf("rule.SportCenterID = %v, want %v", r.SportCenterID, center.ID)
		}
		if !r.Active {
			t.Error("regla debe estar activa")
		}
		if len(r.Slots) != 1 {
			t.Fatalf("franjas de la regla = %d, want 1", len(r.Slots))
		}
		// Pago anticipado → toda franja queda con pago requerido por defecto.
		if !r.Slots[0].PaymentRequired {
			t.Errorf("franja de %q con modalidad pago anticipado debe tener payment_required = true", center.PaymentMode)
		}
		if r.Slots[0].PaymentOptional {
			t.Error("franja con pago anticipado no debe tener payment_optional = true")
		}
	}
}

func TestUpdateOnboarding_Finalizar_PagoAnticipado_MarcaPagoRequeridoEnFranjas(t *testing.T) {
	center := onboardingCenter()
	center.Contact.Phone = "+56 9 1234 5678"
	center.PaymentMode = PaymentModePayOnline
	repo := &mockOnboardingRepo{centers: []domain.SportCenter{center}}
	spaces := &mockSpaceRepo{}
	rules := &mockRuleRepo{}
	uc := onboardingUCWithRepos(repo, spaces, rules)

	err := uc.UpdateSportCenterOnboarding(context.Background(), "auth0|wizard", UpdateOnboardingInput{
		Step:     OnboardingStepFinal,
		Finalize: true,
		Templates: []domain.SpaceTemplate{
			{
				Tipo:     "cancha",
				Cantidad: 1,
				Cancha:   true,
				Rules: []domain.ScheduleRule{
					{
						DaysOfWeek: []int{1},
						Slots:      []domain.ScheduleRuleSlot{{StartTime: "09:00", EndTime: "12:00", Price: 10000}},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("UpdateSportCenterOnboarding retornó error = %v", err)
	}
	if len(rules.created) != 1 || len(rules.created[0].Slots) != 1 {
		t.Fatalf("se esperaba 1 regla con 1 franja, got %d reglas", len(rules.created))
	}
	slot := rules.created[0].Slots[0]
	if !slot.PaymentRequired || slot.PaymentOptional {
		t.Errorf("franja con pago anticipado = {required:%v optional:%v}, want {required:true optional:false}", slot.PaymentRequired, slot.PaymentOptional)
	}
}

func TestUpdateOnboarding_Finalizar_SoloReservas_NoMarcaPagoRequerido(t *testing.T) {
	center := onboardingCenter()
	center.Contact.Phone = "+56 9 1234 5678"
	center.PaymentMode = PaymentModePayAtCenter
	repo := &mockOnboardingRepo{centers: []domain.SportCenter{center}}
	spaces := &mockSpaceRepo{}
	rules := &mockRuleRepo{}
	uc := onboardingUCWithRepos(repo, spaces, rules)

	err := uc.UpdateSportCenterOnboarding(context.Background(), "auth0|wizard", UpdateOnboardingInput{
		Step:     OnboardingStepFinal,
		Finalize: true,
		Templates: []domain.SpaceTemplate{
			{
				Tipo:     "cancha",
				Cantidad: 1,
				Cancha:   true,
				Rules: []domain.ScheduleRule{
					{
						DaysOfWeek: []int{1},
						Slots:      []domain.ScheduleRuleSlot{{StartTime: "09:00", EndTime: "12:00", Price: 10000, PaymentRequired: true}},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("UpdateSportCenterOnboarding retornó error = %v", err)
	}
	if len(rules.created) != 1 || len(rules.created[0].Slots) != 1 {
		t.Fatalf("se esperaba 1 regla con 1 franja, got %d reglas", len(rules.created))
	}
	slot := rules.created[0].Slots[0]
	// Solo reservas: el pago se hace en el centro; la franja no exige pago.
	if slot.PaymentRequired || slot.PaymentOptional {
		t.Errorf("franja con solo reservas = {required:%v optional:%v}, want ambos false", slot.PaymentRequired, slot.PaymentOptional)
	}
}

func TestValidarTelefonoChileno(t *testing.T) {
	cases := []struct {
		name  string
		phone string
		want  bool
	}{
		{"movil con espacios", "+56 9 1234 5678", true},
		{"movil sin espacios", "+56912345678", true},
		{"fijo con guiones", "+56-2-2345-6789", true},
		{"sin codigo pais", "9 1234 5678", false},
		{"sin mas", "56 9 1234 5678", false},
		{"demasiado corto", "+56 123", false},
		{"letras", "+56 9 ABC DEFGH", false},
		{"vacío", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := validarTelefonoChileno(tc.phone); got != tc.want {
				t.Errorf("validarTelefonoChileno(%q) = %v, want %v", tc.phone, got, tc.want)
			}
		})
	}
}

// ---------- Templates por tipo (etapa Espacios) ----------

func TestUpdateOnboarding_EspaciosConSpaceTemplates_PersisteBorrador(t *testing.T) {
	repo := &mockOnboardingRepo{centers: []domain.SportCenter{onboardingCenter()}}
	uc := onboardingUCWithRepo(repo)

	templates := []domain.SpaceTemplate{
		{
			Tipo:     "cancha",
			Cantidad: 2,
			Cancha:   true,
			Rules: []domain.ScheduleRule{
				{
					DaysOfWeek: []int{1, 2, 3, 4, 5},
					Slots: []domain.ScheduleRuleSlot{
						{StartTime: "09:00", EndTime: "12:00", Price: 10000},
					},
					Active: true,
				},
			},
		},
		{Tipo: "piscina", Cantidad: 1, PricePerDay: 5000},
	}

	err := uc.UpdateSportCenterOnboarding(context.Background(), "auth0|wizard", UpdateOnboardingInput{
		Step:       OnboardingStepEspacios,
		Templates: templates,
	})
	if err != nil {
		t.Fatalf("UpdateSportCenterOnboarding retornó error = %v", err)
	}
	if repo.lastUpdate == nil {
		t.Fatal("repo.Update no fue llamado")
	}
	if repo.lastUpdate.OnboardingStep != OnboardingStepEspacios {
		t.Errorf("onboarding_step = %d, want %d", repo.lastUpdate.OnboardingStep, OnboardingStepEspacios)
	}
	// El borrador se persiste (con reglas/valores) para retomar el wizard.
	if len(repo.lastUpdate.SpaceTemplates) != 2 {
		t.Fatalf("space_templates = %d, want 2", len(repo.lastUpdate.SpaceTemplates))
	}
	if repo.lastUpdate.SpaceTemplates[0].Cantidad != 2 || repo.lastUpdate.SpaceTemplates[1].Tipo != "piscina" {
		t.Errorf("templates = %+v, want cancha:2 y piscina", repo.lastUpdate.SpaceTemplates)
	}
}

func TestUpdateOnboarding_EspaciosConSpaceTemplates_CanchaSinFranjas_Rechaza(t *testing.T) {
	repo := &mockOnboardingRepo{centers: []domain.SportCenter{onboardingCenter()}}
	uc := onboardingUCWithRepo(repo)

	// Regla con día definido pero sin franjas → debe rechazarse.
	templates := []domain.SpaceTemplate{
		{
			Tipo:   "cancha",
			Cantidad: 1,
			Cancha: true,
			Rules: []domain.ScheduleRule{
				{DaysOfWeek: []int{1}, Slots: []domain.ScheduleRuleSlot{}},
			},
		},
	}
	err := uc.UpdateSportCenterOnboarding(context.Background(), "auth0|wizard", UpdateOnboardingInput{
		Step:       OnboardingStepEspacios,
		Templates: templates,
	})
	if err == nil || !strings.Contains(err.Error(), "horarios") {
		t.Errorf("error = %v, want mencionar horarios", err)
	}
	if repo.lastUpdate != nil {
		t.Error("repo.Update no debe llamarse con plantilla inválida")
	}
}

func TestUpdateOnboarding_EspaciosConSpaceTemplates_FranjasSolapadasMismoDia_Rechaza(t *testing.T) {
	repo := &mockOnboardingRepo{centers: []domain.SportCenter{onboardingCenter()}}
	uc := onboardingUCWithRepo(repo)

	// Misma regla, mismo día, franjas solapadas (09-12 y 10-13).
	templates := []domain.SpaceTemplate{
		{
			Tipo:     "cancha",
			Cantidad: 1,
			Cancha:   true,
			Rules: []domain.ScheduleRule{
				{
					DaysOfWeek: []int{1},
					Slots: []domain.ScheduleRuleSlot{
						{StartTime: "09:00", EndTime: "12:00", Price: 10000},
						{StartTime: "10:00", EndTime: "13:00", Price: 9000},
					},
				},
			},
		},
	}
	err := uc.UpdateSportCenterOnboarding(context.Background(), "auth0|wizard", UpdateOnboardingInput{
		Step:       OnboardingStepEspacios,
		Templates: templates,
	})
	if err == nil || !strings.Contains(err.Error(), "solapadas") {
		t.Errorf("error = %v, want mencionar solapadas", err)
	}
}

func TestUpdateOnboarding_EspaciosConSpaceTemplates_CantidadCero_Rechaza(t *testing.T) {
	repo := &mockOnboardingRepo{centers: []domain.SportCenter{onboardingCenter()}}
	uc := onboardingUCWithRepo(repo)

	templates := []domain.SpaceTemplate{
		{Tipo: "cancha", Cantidad: 0, Cancha: true},
	}
	err := uc.UpdateSportCenterOnboarding(context.Background(), "auth0|wizard", UpdateOnboardingInput{
		Step:       OnboardingStepEspacios,
		Templates: templates,
	})
	if err == nil || !strings.Contains(err.Error(), "cantidad") {
		t.Errorf("error = %v, want mencionar cantidad", err)
	}
}

func TestValidarTemplates_ValoresFacilityNegativos_Rechaza(t *testing.T) {
	_, err := validarTemplates([]domain.SpaceTemplate{
		{Tipo: "piscina", Cantidad: 1, PricePerDay: -100},
	})
	if err == nil || !strings.Contains(err.Error(), "negativos") {
		t.Errorf("error = %v, want mencionar negativos", err)
	}
}