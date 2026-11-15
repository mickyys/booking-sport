package domain

import (
	"testing"
	"time"
)

func TestEffectiveServiceFee(t *testing.T) {
	oauth := &SportCenter{MercadoPago: &MercadoPagoConfig{ConnectionType: MPConnectionOAuth}}
	manual := &SportCenter{MercadoPago: &MercadoPagoConfig{ConnectionType: MPConnectionManual}}
	base := func() *Booking {
		return &Booking{
			Status:      BookingStatusConfirmed,
			PaidAmount:  30000,
			MPPaymentID: "123",
			CreatedAt:   ServiceFeeSince.Add(time.Hour),
			PaymentInfo: &PaymentInfo{CommissionRate: 5},
		}
	}

	if got := base().EffectiveServiceFee(oauth); got != 1500 {
		t.Fatalf("derivado = %v, quiero 1500", got)
	}
	stored := base()
	stored.PaymentInfo.ServiceFeeAmount = 1200
	if got := stored.EffectiveServiceFee(oauth); got != 1200 {
		t.Fatalf("guardado = %v, quiero 1200", got)
	}
	if got := base().EffectiveServiceFee(manual); got != 0 {
		t.Fatalf("conexión manual = %v, quiero 0", got)
	}
	old := base()
	old.CreatedAt = ServiceFeeSince.Add(-time.Hour)
	if got := old.EffectiveServiceFee(oauth); got != 0 {
		t.Fatalf("anterior al cargo = %v, quiero 0", got)
	}
	cancelled := base()
	cancelled.Status = BookingStatusCancelled
	if got := cancelled.EffectiveServiceFee(oauth); got != 0 {
		t.Fatalf("cancelada = %v, quiero 0", got)
	}
}
