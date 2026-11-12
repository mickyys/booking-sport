package app

import (
	"context"
	"testing"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
)

func payAtCenterCenter(firstMonthFree bool) *domain.SportCenter {
	c := readyCenter()
	c.AdminEmail = "admin@centro.cl"
	c.FirstMonthFree = firstMonthFree
	c.Commercial = &domain.Commercial{PaymentMode: domain.CommercialModePayAtCenter, Published: boolPtrApp(false)}
	return c
}

func TestSubscriptionFreeTrial_SinPrimerMesGratis_NoEnviaPrueba(t *testing.T) {
	uc, _, _, prov := newCommercialFixture(payAtCenterCenter(false))
	if _, err := uc.StartSubscription(context.Background(), payAtCenterCenter(false).ID, ""); err != nil {
		t.Fatal(err)
	}
	if prov.created.FreeTrialMonths != 0 {
		t.Errorf("FreeTrialMonths = %d, want 0", prov.created.FreeTrialMonths)
	}
}

func TestSubscriptionFreeTrial_ConPrimerMesGratis_EnviaUnMes(t *testing.T) {
	c := payAtCenterCenter(true)
	uc, _, _, prov := newCommercialFixture(c)
	if _, err := uc.StartSubscription(context.Background(), c.ID, ""); err != nil {
		t.Fatal(err)
	}
	if prov.created.FreeTrialMonths != 1 {
		t.Errorf("FreeTrialMonths = %d, want 1", prov.created.FreeTrialMonths)
	}
}

func TestSubscriptionFreeTrial_YaActivadaAlgunaVez_NoSeRepite(t *testing.T) {
	c := payAtCenterCenter(true)
	started := time.Now().Add(-48 * time.Hour)
	c.Commercial.Subscription = &domain.Subscription{Status: domain.SubscriptionCancelled, StartedAt: &started}
	uc, _, _, prov := newCommercialFixture(c)
	if _, err := uc.StartSubscription(context.Background(), c.ID, ""); err != nil {
		t.Fatal(err)
	}
	if prov.created.FreeTrialMonths != 0 {
		t.Errorf("FreeTrialMonths = %d, want 0: el mes gratis no se repite", prov.created.FreeTrialMonths)
	}
}

func TestSubscriptionFreeTrial_VistaIndicaElBeneficio(t *testing.T) {
	c := payAtCenterCenter(true)
	uc, _, _, _ := newCommercialFixture(c)
	v, err := uc.Get(context.Background(), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if v.Subscription == nil || !v.Subscription.FirstMonthFree {
		t.Errorf("subscription = %+v, want first_month_free=true", v.Subscription)
	}

	c2 := payAtCenterCenter(false)
	uc2, _, _, _ := newCommercialFixture(c2)
	v2, _ := uc2.Get(context.Background(), c2.ID)
	if v2.Subscription == nil || v2.Subscription.FirstMonthFree {
		t.Errorf("subscription = %+v, want first_month_free=false", v2.Subscription)
	}
}
