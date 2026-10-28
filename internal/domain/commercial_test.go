package domain

import (
	"testing"
	"time"
)

func basicCenter() *SportCenter {
	return &SportCenter{Name: "Centro", Address: "Calle 1", City: "Santiago", OnboardingStatus: "completed"}
}

func connectedMP() *MercadoPagoConfig { return &MercadoPagoConfig{AccessToken: "tok"} }

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

func TestCanPublishPayAtCenterRequiresActiveSubscription(t *testing.T) {
	c := basicCenter()
	c.Commercial = &Commercial{PaymentMode: CommercialModePayAtCenter, Published: boolPtr(false)}
	in := PublishInputs{HasSpaces: true, HasSchedules: true}

	check := CanPublishSportCenter(c, in)
	if check.CanPublish || !contains(check.Missing, PublishMissingSubscription) {
		t.Fatalf("sin suscripción no debe publicar: %+v", check)
	}
	if check.Status != LifecyclePendingSubscription {
		t.Errorf("status = %s, want PENDING_SUBSCRIPTION", check.Status)
	}

	c.Commercial.Subscription = &Subscription{Status: SubscriptionActive}
	check = CanPublishSportCenter(c, in)
	if !check.CanPublish || check.Status != LifecycleReady {
		t.Fatalf("con suscripción activa debe estar READY: %+v", check)
	}
}

func TestCanPublishPayOnlineRequiresMercadoPago(t *testing.T) {
	c := basicCenter()
	c.Commercial = &Commercial{PaymentMode: CommercialModePayOnline, Published: boolPtr(false)}
	in := PublishInputs{HasSpaces: true, HasSchedules: true}

	// El valor guardado "CONNECTED" no basta: manda la conexión real.
	c.Commercial.OnlinePayment = &OnlinePayment{PaymentProvider: &PaymentProvider{Provider: ProviderMercadoPago, Status: ProviderStatusConnected}}
	check := CanPublishSportCenter(c, in)
	if check.CanPublish || check.Status != LifecyclePendingPaymentSetup {
		t.Fatalf("sin credenciales MP no debe publicar: %+v", check)
	}

	c.MercadoPago = connectedMP()
	if check = CanPublishSportCenter(c, in); !check.CanPublish {
		t.Fatalf("con MP conectado debe poder publicar: %+v", check)
	}

	expired := time.Now().Add(-time.Hour)
	c.MercadoPago = &MercadoPagoConfig{AccessToken: "tok", ExpiresAt: &expired}
	if check = CanPublishSportCenter(c, in); check.CanPublish {
		t.Fatalf("credenciales vencidas no deben publicar: %+v", check)
	}
}

func TestCanPublishGeneralRules(t *testing.T) {
	c := basicCenter()
	c.PaymentMode = "pay_online"
	c.MercadoPago = connectedMP()

	check := CanPublishSportCenter(c, PublishInputs{})
	for _, want := range []string{PublishMissingSpaces, PublishMissingSchedules} {
		if !contains(check.Missing, want) {
			t.Errorf("falta %s en %v", want, check.Missing)
		}
	}

	c.Address = ""
	if check = CanPublishSportCenter(c, PublishInputs{HasSpaces: true, HasSchedules: true}); !contains(check.Missing, PublishMissingBasicInfo) {
		t.Errorf("sin dirección debe faltar basic_info: %v", check.Missing)
	}

	noMode := basicCenter()
	if check = CanPublishSportCenter(noMode, PublishInputs{HasSpaces: true, HasSchedules: true}); !contains(check.Missing, PublishMissingPaymentMode) {
		t.Errorf("sin modalidad debe faltar payment_mode: %v", check.Missing)
	}

	review := basicCenter()
	review.Commercial = &Commercial{PaymentMode: CommercialModeNeedsReview, Review: true}
	if check = CanPublishSportCenter(review, PublishInputs{HasSpaces: true, HasSchedules: true}); check.CanPublish {
		t.Errorf("NEEDS_REVIEW no debe publicar: %+v", check)
	}
}

func TestLegacyCenterStaysPublishedAndOperational(t *testing.T) {
	legacy := &SportCenter{Name: "Viejo", OnboardingStatus: ""}
	if !legacy.IsPublished() || !legacy.IsOperational() {
		t.Fatal("un centro histórico sin commercial debe seguir publicado")
	}
	migrated := &SportCenter{Commercial: &Commercial{PaymentMode: CommercialModePayOnline}}
	if !migrated.IsPublished() || !migrated.IsOperational() {
		t.Fatal("un centro migrado (published nil) debe seguir publicado")
	}
	fresh := &SportCenter{Commercial: &Commercial{Published: boolPtr(false)}}
	if fresh.IsPublished() || fresh.IsOperational() {
		t.Fatal("un centro nuevo sin publicar no debe operar")
	}
}

func TestLifecycleSuspended(t *testing.T) {
	c := basicCenter()
	c.Status = SportCenterStatusSuspended
	if got := CanPublishSportCenter(c, PublishInputs{}).Status; got != LifecycleSuspended {
		t.Errorf("status = %s, want SUSPENDED", got)
	}
}

func TestEffectiveCommission(t *testing.T) {
	zero := 0
	four := 4
	online := func(v *int, src string) *SportCenter {
		return &SportCenter{Commercial: &Commercial{PaymentMode: CommercialModePayOnline,
			OnlinePayment: &OnlinePayment{Commission: &CommissionConfig{Type: CommissionTypePercentage, Value: v, Source: src}}}}
	}
	tests := []struct {
		name     string
		c        *SportCenter
		enforced bool
		rate     int
		source   string
	}{
		{"flag off ignora commercial", online(&zero, CommissionSourceCenter), false, 7, CommissionSourceDefault},
		{"flag off respeta fee legacy", &SportCenter{MarketplaceFeePercent: 5}, false, 5, CommissionSourceCenter},
		{"flag on 0% explícito", online(&zero, CommissionSourceCenter), true, 0, CommissionSourceCenter},
		{"flag on valor propio", online(&four, CommissionSourceCenter), true, 4, CommissionSourceCenter},
		{"flag on origen DEFAULT cae al default", online(&four, CommissionSourceDefault), true, 7, CommissionSourceDefault},
		{"flag on sin valor", online(nil, CommissionSourceCenter), true, 7, CommissionSourceDefault},
		{"flag on PAY_AT_CENTER sin comisión", &SportCenter{PaymentMode: "pay_at_center"}, true, 0, CommissionSourceCenter},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rate, source := tt.c.EffectiveCommission(tt.enforced, 7)
			if rate != tt.rate || source != tt.source {
				t.Fatalf("got (%d,%s), want (%d,%s)", rate, source, tt.rate, tt.source)
			}
		})
	}
}

func boolPtr(b bool) *bool { return &b }
