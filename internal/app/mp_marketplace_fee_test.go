package app

import (
	"testing"

	"github.com/hamp/booking-sport/internal/domain"
)

// TestMarketplaceFeeFor verifica que el fee sólo viaje a Mercado Pago cuando la
// conexión del centro es OAuth (marketplace autorizado).
func TestMarketplaceFeeFor(t *testing.T) {
	oauth := &domain.SportCenter{MercadoPago: &domain.MercadoPagoConfig{
		AccessToken:    "APP_USR-1",
		ConnectionType: domain.MPConnectionOAuth,
	}}
	manual := &domain.SportCenter{MercadoPago: &domain.MercadoPagoConfig{
		AccessToken:    "APP_USR-1",
		ConnectionType: domain.MPConnectionManual,
	}}
	// Documento sin connection_type: se infiere manual porque no hay evidencia
	// de OAuth.
	inferredManual := &domain.SportCenter{MercadoPago: &domain.MercadoPagoConfig{
		AccessToken: "APP_USR-1",
	}}
	notConnected := &domain.SportCenter{}

	defer SetCommercialModelEnforced(false)
	SetCommercialModelEnforced(false)

	tests := []struct {
		name   string
		center *domain.SportCenter
		amount float64
		want   float64
	}{
		{name: "oauth cobra el 7% por defecto", center: oauth, amount: 30000, want: 2100},
		{name: "oauth respeta redondeo", center: oauth, amount: 15000, want: 1050},
		{name: "manual no envía fee", center: manual, amount: 30000, want: 0},
		{name: "sin connection_type se infiere manual", center: inferredManual, amount: 30000, want: 0},
		{name: "sin mercadopago no envía fee", center: notConnected, amount: 30000, want: 0},
		{name: "center nil", center: nil, amount: 30000, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := marketplaceFeeFor(tt.center, tt.amount); got != tt.want {
				t.Fatalf("marketplaceFeeFor() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestMarketplaceFeeForOAuthExplicitZero comprueba que con el modelo comercial
// activo un 0% explícito del centro se respete en la conexión OAuth.
func TestMarketplaceFeeForOAuthExplicitZero(t *testing.T) {
	zero := intPtr(0)
	center := &domain.SportCenter{
		MercadoPago: &domain.MercadoPagoConfig{
			AccessToken:    "APP_USR-1",
			ConnectionType: domain.MPConnectionOAuth,
		},
		Commercial: &domain.Commercial{
			PaymentMode: domain.CommercialModePayOnline,
			OnlinePayment: &domain.OnlinePayment{Commission: &domain.CommissionConfig{
				Type:   domain.CommissionTypePercentage,
				Value:  zero,
				Source: domain.CommissionSourceCenter,
			}},
		},
	}

	defer SetCommercialModelEnforced(false)
	SetCommercialModelEnforced(true)

	if got := marketplaceFeeFor(center, 30000); got != 0 {
		t.Fatalf("marketplaceFeeFor() = %v, want 0", got)
	}
}
