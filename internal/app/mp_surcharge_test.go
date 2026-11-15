package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/hamp/booking-sport/internal/domain"
	"github.com/hamp/booking-sport/pkg/mercadopago"
)

func TestComputeSurchargeCharge(t *testing.T) {
	c := computeSurchargeCharge(30000, 5, 3.7961)
	if c.Surcharge != 1500 || c.Total != 31500 {
		t.Fatalf("surcharge/total = %v/%v, want 1500/31500", c.Surcharge, c.Total)
	}
	if c.MPCost != 1196 || c.MarketplaceFee != 304 {
		t.Fatalf("mp cost/fee = %v/%v, want 1196/304", c.MPCost, c.MarketplaceFee)
	}
	if got := c.Total - c.MPCost - c.MarketplaceFee; got != 30000 {
		t.Fatalf("neto centro = %v, want 30000", got)
	}

	zero := computeSurchargeCharge(30000, 0, 3.7961)
	if zero.Total != 30000 || zero.MarketplaceFee != 0 {
		t.Fatalf("0%%: %+v", zero)
	}

	low := computeSurchargeCharge(30000, 2, 3.7961)
	if low.MarketplaceFee != 0 {
		t.Fatalf("comisión < costo MP: fee = %v, want 0", low.MarketplaceFee)
	}
}

func TestSurchargeChargeForManual(t *testing.T) {
	manual := &domain.SportCenter{MercadoPago: &domain.MercadoPagoConfig{
		AccessToken: "APP_USR-1", ConnectionType: domain.MPConnectionManual,
	}}
	c := surchargeChargeFor(manual, 30000)
	if c.Total != 30000 || c.MarketplaceFee != 0 {
		t.Fatalf("manual debe cobrar solo el precio: %+v", c)
	}
	if got := baseFromGross(manual, 30000); got != 30000 {
		t.Fatalf("baseFromGross manual = %v", got)
	}
}

func TestBaseFromGrossOAuth(t *testing.T) {
	five := intPtr(5)
	oauth := &domain.SportCenter{
		MercadoPago: &domain.MercadoPagoConfig{AccessToken: "APP_USR-1", ConnectionType: domain.MPConnectionOAuth},
		Commercial: &domain.Commercial{
			PaymentMode: domain.CommercialModePayOnline,
			OnlinePayment: &domain.OnlinePayment{Commission: &domain.CommissionConfig{
				Type: domain.CommissionTypePercentage, Value: five, Source: domain.CommissionSourceCenter,
			}},
		},
	}
	if got := baseFromGross(oauth, 31500); got != 30000 {
		t.Fatalf("baseFromGross = %v, want 30000", got)
	}
}

type captureRequester struct {
	body []byte
}

func (c *captureRequester) Do(req *http.Request) (*http.Response, error) {
	c.body, _ = io.ReadAll(req.Body)
	return &http.Response{
		StatusCode: 201,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"id":"pref-1","init_point":"https://mp/checkout"}`)),
	}, nil
}

func oauthCenter(rate int) *domain.SportCenter {
	return &domain.SportCenter{
		MercadoPago: &domain.MercadoPagoConfig{AccessToken: "APP_USR-1234567890", ConnectionType: domain.MPConnectionOAuth},
		Commercial: &domain.Commercial{
			PaymentMode: domain.CommercialModePayOnline,
			OnlinePayment: &domain.OnlinePayment{Commission: &domain.CommissionConfig{
				Type: domain.CommissionTypePercentage, Value: intPtr(rate), Source: domain.CommissionSourceCenter,
			}},
		},
	}
}

func preferenceBody(t *testing.T, center *domain.SportCenter, base float64) (float64, float64) {
	t.Helper()
	cap := &captureRequester{}
	orig := newMPClient
	newMPClient = func(token string) *mercadopago.Client { return mercadopago.NewClientWithRequester(token, cap) }
	defer func() { newMPClient = orig }()

	res, _, err := createSurchargePreference(context.Background(), center, base, mercadopago.PreferenceInput{Title: "Reserva", Email: "a@b.cl", ExternalRef: "X"})
	if err != nil || res == nil || res.ID != "pref-1" {
		t.Fatalf("preferencia: %v %+v", err, res)
	}
	var req struct {
		Items []struct {
			UnitPrice float64 `json:"unit_price"`
		} `json:"items"`
		MarketplaceFee float64 `json:"marketplace_fee"`
	}
	if err := json.Unmarshal(cap.body, &req); err != nil || len(req.Items) != 1 {
		t.Fatalf("body inválido: %v %s", err, cap.body)
	}
	return req.Items[0].UnitPrice, req.MarketplaceFee
}

func TestCreateSurchargePreferenceOAuth(t *testing.T) {
	price, fee := preferenceBody(t, oauthCenter(5), 30000)
	if price != 31500 || fee != 304 {
		t.Fatalf("unit_price/marketplace_fee = %v/%v, want 31500/304", price, fee)
	}
}

func TestCreateSurchargePreferenceManual(t *testing.T) {
	manual := &domain.SportCenter{MercadoPago: &domain.MercadoPagoConfig{
		AccessToken: "APP_USR-1234567890", ConnectionType: domain.MPConnectionManual,
	}}
	price, fee := preferenceBody(t, manual, 30000)
	if price != 30000 || fee != 0 {
		t.Fatalf("manual: unit_price/marketplace_fee = %v/%v, want 30000/0", price, fee)
	}
}

func TestGrossFromBaseRefundsSurcharge(t *testing.T) {
	c := oauthCenter(5)
	if got := grossFromBase(c, 30000); got != 31500 {
		t.Fatalf("grossFromBase = %v, want 31500", got)
	}
	if got := grossFromBase(c, 27000); got != 28350 {
		t.Fatalf("reembolso parcial = %v, want 28350", got)
	}
	if got := ServiceFeePercent(c); got != 5 {
		t.Fatalf("ServiceFeePercent = %d, want 5", got)
	}
	if got := ServiceFeePercent(&domain.SportCenter{}); got != 0 {
		t.Fatalf("sin OAuth = %d, want 0", got)
	}
}

func TestValidateCommissionRate(t *testing.T) {
	for _, ok := range []int{0, 4, 5, 100} {
		if err := validateCommissionRate(ok); err != nil {
			t.Errorf("%d%% debe ser válida: %v", ok, err)
		}
	}
	for _, bad := range []int{-1, 1, 2, 3, 101} {
		if err := validateCommissionRate(bad); err == nil {
			t.Errorf("%d%% debe rechazarse", bad)
		}
	}
}
