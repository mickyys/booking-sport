package app

import (
	"context"
	"testing"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// ---------- Mocks para UpdateSettings ----------

type settingsCenterRepo struct {
	SportCenterRepository
	center      domain.SportCenter
	updatedMode *string
}

func (m *settingsCenterRepo) FindByID(ctx context.Context, id primitive.ObjectID) (*domain.SportCenter, error) {
	return &m.center, nil
}

func (m *settingsCenterRepo) UpdateSettings(ctx context.Context, id primitive.ObjectID, slug *string, cancellationHours *int, retentionPercent *int, partialPaymentEnabled *bool, partialPaymentPercent *int, imageURL *string, marketplaceFeePercent *int, branding *domain.SportCenterBranding, address *string, city *string, coordinates *domain.Coordinates, paymentMode *string) error {
	m.updatedMode = paymentMode
	if paymentMode != nil {
		m.center.PaymentMode = *paymentMode
	}
	return nil
}

type settingsCourtRepo struct {
	CourtRepository
	called        bool
	required      bool
	partialCalled bool
	partialValue  bool
}

func (m *settingsCourtRepo) SyncPartialPaymentSlots(ctx context.Context, centerID primitive.ObjectID, partialPaymentEnabled bool) (int64, error) {
	m.partialCalled = true
	m.partialValue = partialPaymentEnabled
	return 1, nil
}

func (m *settingsCourtRepo) SyncPaymentModeSlots(ctx context.Context, centerID primitive.ObjectID, paymentRequired bool) (int64, error) {
	m.called = true
	m.required = paymentRequired
	return 1, nil
}

type settingsRuleRepo struct {
	ScheduleRuleRepository
	called        bool
	required      bool
	partialCalled bool
	partialValue  bool
}

func (m *settingsRuleRepo) SyncPartialPaymentSlots(ctx context.Context, centerID primitive.ObjectID, partialPaymentEnabled bool) (int64, error) {
	m.partialCalled = true
	m.partialValue = partialPaymentEnabled
	return 1, nil
}

type settingsExceptionSyncer struct {
	called bool
	value  bool
}

func (m *settingsExceptionSyncer) SyncPartialPaymentSlots(ctx context.Context, centerID primitive.ObjectID, partialPaymentEnabled bool) (int64, error) {
	m.called = true
	m.value = partialPaymentEnabled
	return 1, nil
}

func (m *settingsRuleRepo) SyncPaymentModeSlots(ctx context.Context, centerID primitive.ObjectID, paymentRequired bool) (int64, error) {
	m.called = true
	m.required = paymentRequired
	return 1, nil
}

type settingsDisconnector struct {
	called   bool
	centerID primitive.ObjectID
}

func (m *settingsDisconnector) DisconnectByCenterID(ctx context.Context, centerID primitive.ObjectID) error {
	m.called = true
	m.centerID = centerID
	return nil
}

func newSettingsUseCase(initialMode string) (*SportCenterUseCase, *settingsCenterRepo, *settingsCourtRepo, *settingsRuleRepo, *settingsDisconnector) {
	centerRepo := &settingsCenterRepo{
		center: domain.SportCenter{
			ID:          newObjectID(),
			Name:        "Club Settings",
			PaymentMode: initialMode,
		},
	}
	courtRepo := &settingsCourtRepo{}
	ruleRepo := &settingsRuleRepo{}
	disconnector := &settingsDisconnector{}
	uc := &SportCenterUseCase{
		repo:      centerRepo,
		courtRepo: courtRepo,
		ruleRepo:  ruleRepo,
	}
	uc.SetMPDisconnector(disconnector)
	return uc, centerRepo, courtRepo, ruleRepo, disconnector
}

func TestUpdateSettings_ActivaPagoOnlineSincronizaFranjas(t *testing.T) {
	uc, centerRepo, courtRepo, ruleRepo, disconnector := newSettingsUseCase(PaymentModePayAtCenter)

	mode := PaymentModePayOnline
	if err := uc.UpdateSettings(context.Background(), centerRepo.center.ID, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, &mode); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if centerRepo.updatedMode == nil || *centerRepo.updatedMode != PaymentModePayOnline {
		t.Fatalf("payment_mode no se persistió como pay_online: %v", centerRepo.updatedMode)
	}
	if !ruleRepo.called || !ruleRepo.required {
		t.Errorf("schedule_rules debe sincronizarse con payment_required=true (called=%v required=%v)", ruleRepo.called, ruleRepo.required)
	}
	if !courtRepo.called || !courtRepo.required {
		t.Errorf("spaces.schedule debe sincronizarse con payment_required=true (called=%v required=%v)", courtRepo.called, courtRepo.required)
	}
	if disconnector.called {
		t.Errorf("no debe desconectar Mercado Pago al activar el pago online")
	}
}

func TestUpdateSettings_DesactivaPagoOnlineBorraCredenciales(t *testing.T) {
	uc, centerRepo, courtRepo, ruleRepo, disconnector := newSettingsUseCase(PaymentModePayOnline)

	mode := PaymentModePayAtCenter
	if err := uc.UpdateSettings(context.Background(), centerRepo.center.ID, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, &mode); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if centerRepo.updatedMode == nil || *centerRepo.updatedMode != PaymentModePayAtCenter {
		t.Fatalf("payment_mode no se persistió como pay_at_center: %v", centerRepo.updatedMode)
	}
	if !disconnector.called {
		t.Errorf("debe revocar/eliminar las credenciales de Mercado Pago al desactivar")
	}
	if disconnector.centerID != centerRepo.center.ID {
		t.Errorf("disconnect center_id = %s, want %s", disconnector.centerID.Hex(), centerRepo.center.ID.Hex())
	}
	if !ruleRepo.called || ruleRepo.required {
		t.Errorf("schedule_rules debe sincronizarse con payment_required=false (called=%v required=%v)", ruleRepo.called, ruleRepo.required)
	}
	if !courtRepo.called || courtRepo.required {
		t.Errorf("spaces.schedule debe sincronizarse con payment_required=false (called=%v required=%v)", courtRepo.called, courtRepo.required)
	}
}

func TestUpdateSettings_MismaModalidadNoSincroniza(t *testing.T) {
	uc, centerRepo, courtRepo, ruleRepo, disconnector := newSettingsUseCase(PaymentModePayOnline)

	mode := PaymentModePayOnline
	if err := uc.UpdateSettings(context.Background(), centerRepo.center.ID, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, &mode); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ruleRepo.called || courtRepo.called || disconnector.called {
		t.Errorf("no debe sincronizar ni desconectar si la modalidad no cambió (rule=%v court=%v disconnect=%v)", ruleRepo.called, courtRepo.called, disconnector.called)
	}
}

func TestUpdateSettings_PagoParcialSeAplicaATodasLasFranjas(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		uc, centerRepo, courtRepo, ruleRepo, _ := newSettingsUseCase(PaymentModePayOnline)
		centerRepo.center.PartialPaymentEnabled = enabled // ya estaba así: igual debe sincronizar
		exceptions := &settingsExceptionSyncer{}
		uc.SetExceptionPartialSyncer(exceptions)

		if err := uc.UpdateSettings(context.Background(), centerRepo.center.ID, nil, nil, nil, &enabled, nil, nil, nil, nil, nil, nil, nil, nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !ruleRepo.partialCalled || ruleRepo.partialValue != enabled {
			t.Errorf("schedule_rules: called=%v value=%v, want %v", ruleRepo.partialCalled, ruleRepo.partialValue, enabled)
		}
		if !exceptions.called || exceptions.value != enabled {
			t.Errorf("schedule_exceptions: called=%v value=%v, want %v", exceptions.called, exceptions.value, enabled)
		}
		if !courtRepo.partialCalled || courtRepo.partialValue != enabled {
			t.Errorf("courts: called=%v value=%v, want %v", courtRepo.partialCalled, courtRepo.partialValue, enabled)
		}
	}
}

func TestUpdateSettings_SinPagoParcialNoSincronizaFranjas(t *testing.T) {
	uc, centerRepo, courtRepo, ruleRepo, _ := newSettingsUseCase(PaymentModePayOnline)

	hours := 3
	if err := uc.UpdateSettings(context.Background(), centerRepo.center.ID, nil, &hours, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ruleRepo.partialCalled || courtRepo.partialCalled {
		t.Errorf("no debe tocar franjas si no se envía partial_payment_enabled")
	}
}

func TestApplyPartialPaymentDefault(t *testing.T) {
	off := false
	slots := []domain.ScheduleRuleSlot{{StartTime: "15:00"}, {StartTime: "16:00", PartialPaymentEnabled: &off}}

	online := &domain.SportCenter{PaymentMode: PaymentModePayOnline, PartialPaymentEnabled: true}
	out := applyPartialPaymentDefault(online, slots)
	if out[0].PartialPaymentEnabled == nil || !*out[0].PartialPaymentEnabled {
		t.Errorf("franja sin valor debe heredar el parcial del centro")
	}
	if out[1].PartialPaymentEnabled == nil || *out[1].PartialPaymentEnabled {
		t.Errorf("franja con valor propio no debe cambiar")
	}

	atCenter := &domain.SportCenter{PaymentMode: PaymentModePayAtCenter, PartialPaymentEnabled: true}
	if got := applyPartialPaymentDefault(atCenter, slots); got[0].PartialPaymentEnabled != nil {
		t.Errorf("sin cobro online no se completa el parcial")
	}
}

// ---------- Guard de pagos online ----------

type mpGuardCenterRepo struct {
	SportCenterRepository
	center *domain.SportCenter
}

func (m *mpGuardCenterRepo) FindByID(ctx context.Context, id primitive.ObjectID) (*domain.SportCenter, error) {
	return m.center, nil
}

func TestCreateMercadoPagoPayment_RechazaModalidadSoloReservas(t *testing.T) {
	courtID := newObjectID()
	centerID := newObjectID()
	court := &domain.Court{
		ID:            courtID,
		SportCenterID: centerID,
		Name:          "Cancha 1",
		Schedule: []domain.CourtSchedule{{
			Hour:            10,
			Minutes:         0,
			Price:           30000,
			Status:          "available",
			PaymentRequired: true,
		}},
	}

	uc := &BookingUseCase{
		repo:      &mockBookingRepoForCreate{mockBookingRepoForHold: &mockBookingRepoForHold{}},
		courtRepo: &mockCourtRepoForRecurring{FindByIDFn: func(ctx context.Context, id primitive.ObjectID) (*domain.Court, error) { return court, nil }},
		centerRepo: &mpGuardCenterRepo{center: &domain.SportCenter{
			ID:          centerID,
			Name:        "Club Solo Reservas",
			PaymentMode: PaymentModePayAtCenter,
		}},
	}

	_, err := uc.CreateMercadoPagoPayment(context.Background(), &domain.Booking{
		CourtID: courtID,
		Hour:    10,
		Date:    time.Now().AddDate(0, 0, 2),
	}, false)
	assertErrorContains(t, err, "online payments are disabled")
}

func TestCreateMercadoPagoPayment_PermiteCentroLegacySinModalidad(t *testing.T) {
	courtID := newObjectID()
	centerID := newObjectID()
	court := &domain.Court{
		ID:            courtID,
		SportCenterID: centerID,
		Name:          "Cancha 1",
		Schedule: []domain.CourtSchedule{{
			Hour:            10,
			Minutes:         0,
			Price:           30000,
			Status:          "available",
			PaymentRequired: true,
		}},
	}

	uc := &BookingUseCase{
		repo:      &mockBookingRepoForCreate{mockBookingRepoForHold: &mockBookingRepoForHold{}},
		courtRepo: &mockCourtRepoForRecurring{FindByIDFn: func(ctx context.Context, id primitive.ObjectID) (*domain.Court, error) { return court, nil }},
		centerRepo: &mpGuardCenterRepo{center: &domain.SportCenter{
			ID:   centerID,
			Name: "Club Legacy",
		}},
	}

	_, err := uc.CreateMercadoPagoPayment(context.Background(), &domain.Booking{
		CourtID: courtID,
		Hour:    10,
		Date:    time.Now().AddDate(0, 0, 2),
	}, false)
	// Sin modalidad explícita se conserva el comportamiento legacy: el error
	// proviene de que el centro no tiene Mercado Pago configurado, no del guard.
	assertErrorContains(t, err, "mercadopago not configured")
}
