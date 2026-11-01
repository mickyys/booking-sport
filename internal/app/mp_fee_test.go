package app

import (
	"math"
	"testing"

	"github.com/hamp/booking-sport/internal/domain"
)

func TestCommissionFee(t *testing.T) {
	tests := []struct {
		name   string
		amount float64
		rate   int
		want   float64
	}{
		{"zero is explicit zero", 10000, 0, 0},
		{"negative is zero", 10000, -1, 0},
		{">100 is zero", 10000, 150, 0},
		{"custom 10%", 10000, 10, 1000},
		{"custom 5%", 20000, 5, 1000},
		{"small amount", 1000, 3, 30},
		{"amount zero", 0, 10, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := commissionFee(tt.amount, tt.rate)
			if math.Abs(got-tt.want) > 0.001 {
				t.Fatalf("commissionFee(%v, %d) = %v, want %v", tt.amount, tt.rate, got, tt.want)
			}
		})
	}
}

func TestCenterCommissionRate(t *testing.T) {
	explicitZero := &domain.SportCenter{Commercial: &domain.Commercial{
		PaymentMode: domain.CommercialModePayOnline,
		OnlinePayment: &domain.OnlinePayment{Commission: &domain.CommissionConfig{
			Type: domain.CommissionTypePercentage, Value: intPtr(0), Source: domain.CommissionSourceCenter,
		}},
	}}
	legacy := &domain.SportCenter{}
	ownFee := &domain.SportCenter{MarketplaceFeePercent: 4}

	defer SetCommercialModelEnforced(false)

	SetCommercialModelEnforced(false)
	if got := centerCommissionRate(explicitZero); got != defaultMarketplaceFeePercent {
		t.Errorf("flag off: 0%% explícito = %d, want %d (comportamiento histórico)", got, defaultMarketplaceFeePercent)
	}
	if got := centerCommissionRate(legacy); got != defaultMarketplaceFeePercent {
		t.Errorf("flag off: legacy = %d, want default", got)
	}
	if got := centerCommissionRate(ownFee); got != 4 {
		t.Errorf("flag off: fee propio = %d, want 4", got)
	}

	SetCommercialModelEnforced(true)
	if got := centerCommissionRate(explicitZero); got != 0 {
		t.Errorf("flag on: 0%% explícito = %d, want 0", got)
	}
	if got := centerCommissionRate(legacy); got != defaultMarketplaceFeePercent {
		t.Errorf("flag on: centro nuevo sin comisión = %d, want default", got)
	}
	atCenter := &domain.SportCenter{PaymentMode: "pay_at_center"}
	if got := centerCommissionRate(atCenter); got != 0 {
		t.Errorf("flag on: PAY_AT_CENTER = %d, want 0", got)
	}
}
