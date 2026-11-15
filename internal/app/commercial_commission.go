package app

import (
	"context"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/hamp/booking-sport/internal/domain"
	"github.com/hamp/booking-sport/pkg/mercadopago"
)

// defaultMarketplaceFeePercent es la comisión global para centros que no
// tienen una propia (centros nuevos en PAY_ONLINE).
const defaultMarketplaceFeePercent = 7

var commercialModelEnforced atomic.Bool

// SetCommercialModelEnforced activa (COMMERCIAL_MODEL_ENFORCED=true) las reglas
// del modelo comercial: comisión por centro y publicación. Apagado, ningún
// centro existente cambia de comportamiento.
func SetCommercialModelEnforced(v bool) { commercialModelEnforced.Store(v) }

// CommercialModelEnforced informa si el modelo comercial está activo.
func CommercialModelEnforced() bool { return commercialModelEnforced.Load() }

// centerCommissionRate resuelve el porcentaje de comisión (0-100) que se cobra
// hoy a un centro. Un 0% explícito se respeta.
func centerCommissionRate(center *domain.SportCenter) int {
	rate, _ := center.EffectiveCommission(true, defaultMarketplaceFeePercent)
	return rate
}

// defaultMPEffectiveRate es la tasa efectiva de Mercado Pago Chile con
// acreditación al instante: 3,19% + IVA 19% = 3,7961%. Se puede sobreescribir
// con MP_EFFECTIVE_RATE (porcentaje, p. ej. "3.7961").
const defaultMPEffectiveRate = 3.7961

func mpEffectiveRate() float64 {
	if v := strings.TrimSpace(os.Getenv("MP_EFFECTIVE_RATE")); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 && f < 100 {
			return f
		}
	}
	return defaultMPEffectiveRate
}

// SurchargeCharge es el desglose de un cobro con comisión sumada al precio.
type SurchargeCharge struct {
	Base           float64 // precio que debe recibir el centro
	Rate           int     // % de comisión de ReservaloYA
	Surcharge      float64 // comisión sumada al precio
	Total          float64 // lo que paga el cliente
	MPCost         float64 // costo de Mercado Pago sobre el total
	MarketplaceFee float64 // marketplace_fee enviado a Mercado Pago
}

// computeSurchargeCharge suma la comisión al precio. Mercado Pago descuenta su
// costo al vendedor, así que el marketplace_fee es comisión − costo MP para que
// el centro reciba exactamente el precio. Todo en pesos enteros.
func computeSurchargeCharge(base float64, ratePercent int, mpRatePercent float64) SurchargeCharge {
	c := SurchargeCharge{Base: base, Total: base}
	if base <= 0 || ratePercent <= 0 || ratePercent > 100 {
		return c
	}
	c.Rate = ratePercent
	c.Surcharge = math.Round(base * float64(ratePercent) / 100)
	c.Total = base + c.Surcharge
	c.MPCost = math.Round(c.Total * mpRatePercent / 100)
	c.MarketplaceFee = c.Surcharge - c.MPCost
	if c.MarketplaceFee < 0 {
		c.MarketplaceFee = 0
	}
	return c
}

// surchargeChargeFor resuelve el cobro de una reserva. Solo los centros OAuth
// reciben recargo; con conexión manual la comisión se factura aparte y el
// cliente paga el precio.
func surchargeChargeFor(center *domain.SportCenter, base float64) SurchargeCharge {
	if center == nil || !center.MercadoPago.SupportsMarketplaceFee() {
		return SurchargeCharge{Base: base, Total: base}
	}
	return computeSurchargeCharge(base, centerCommissionRate(center), mpEffectiveRate())
}

// baseFromGross revierte el recargo: dado lo pagado por el cliente devuelve el
// monto que corresponde al precio de la reserva.
func baseFromGross(center *domain.SportCenter, gross float64) float64 {
	if center == nil || !center.MercadoPago.SupportsMarketplaceFee() {
		return gross
	}
	rate := centerCommissionRate(center)
	if rate <= 0 || rate > 100 {
		return gross
	}
	return math.Round(gross * 100 / float64(100+rate))
}

// commissionFee calcula la comisión en moneda local para un porcentaje ya
// resuelto. No aplica defaults: 0% es 0.
func commissionFee(amount float64, ratePercent int) float64 {
	if ratePercent <= 0 || ratePercent > 100 {
		return 0
	}
	return math.Round(amount*float64(ratePercent)) / 100
}

// newMPClient permite reemplazar el cliente de Mercado Pago en tests.
var newMPClient = mercadopago.NewClient

// createSurchargePreference crea la preferencia aplicando el recargo: `base` es
// lo que debe recibir el centro; el cliente paga el total con comisión.
func createSurchargePreference(ctx context.Context, center *domain.SportCenter, base float64, in mercadopago.PreferenceInput) (*mercadopago.PreferenceResult, SurchargeCharge, error) {
	ch := surchargeChargeFor(center, base)
	in.Amount = ch.Total
	in.MarketplaceFee = ch.MarketplaceFee
	res, err := newMPClient(center.MercadoPago.AccessToken).CreatePreference(ctx, in)
	return res, ch, err
}

// grossFromBase es la inversa de baseFromGross: lo que corresponde devolver al
// cliente (incluido el recargo proporcional) por un monto base.
func grossFromBase(center *domain.SportCenter, base float64) float64 {
	if center == nil || !center.MercadoPago.SupportsMarketplaceFee() {
		return base
	}
	rate := centerCommissionRate(center)
	if rate <= 0 || rate > 100 {
		return base
	}
	return math.Round(base * float64(100+rate) / 100)
}

// ServiceFeePercent es el cargo por servicio (%) que se suma al precio para
// mostrarlo al cliente. 0 si no aplica recargo.
func ServiceFeePercent(center *domain.SportCenter) int {
	if center == nil || !center.MercadoPago.SupportsMarketplaceFee() {
		return 0
	}
	if center.EffectiveCommercialMode() == domain.CommercialModePayAtCenter {
		return 0
	}
	return centerCommissionRate(center)
}

// validateCommissionRate rechaza comisiones que no cubren el costo de Mercado
// Pago: con 1–3% el marketplace_fee sería 0 y el centro recibiría menos que su
// precio. 0% (sin recargo) sí es válido.
func validateCommissionRate(v int) error {
	if v < 0 || v > 100 {
		return domain.NewBadRequestError("la comisión debe estar entre 0 y 100")
	}
	if v > 0 && float64(v) <= mpEffectiveRate() {
		return domain.NewBadRequestError(fmt.Sprintf("la comisión debe ser 0 o mayor a %.2f%% (costo de Mercado Pago)", mpEffectiveRate()))
	}
	return nil
}

// CommissionSimulation es el resultado de simular el cobro de una reserva.
type CommissionSimulation struct {
	Price          float64 `json:"price"`
	Rate           int     `json:"rate"`
	Surcharge      float64 `json:"surcharge"`
	Total          float64 `json:"total"`
	MPRate         float64 `json:"mp_rate"`
	MPCost         float64 `json:"mp_cost"`
	MarketplaceFee float64 `json:"marketplace_fee"`
	CenterReceives float64 `json:"center_receives"`
	PlatformNet    float64 `json:"platform_net"`
	// BilledInSplit es false con conexión manual: no hay recargo ni split y la
	// comisión se factura aparte (el desglose es solo referencial).
	BilledInSplit bool `json:"billed_in_split"`
	// BelowMPCost avisa que la comisión no cubre el costo de Mercado Pago.
	BelowMPCost bool `json:"below_mp_cost"`
}

// SimulateCommission calcula el desglose con las mismas reglas que el cobro real.
func SimulateCommission(center *domain.SportCenter, price float64, rate int) CommissionSimulation {
	mpRate := mpEffectiveRate()
	ch := computeSurchargeCharge(price, rate, mpRate)
	sim := CommissionSimulation{
		Price: price, Rate: rate, Surcharge: ch.Surcharge, Total: ch.Total,
		MPRate: mpRate, MPCost: ch.MPCost, MarketplaceFee: ch.MarketplaceFee,
		BilledInSplit: center != nil && center.MercadoPago.SupportsMarketplaceFee(),
		BelowMPCost:   rate > 0 && float64(rate) <= mpRate,
	}
	sim.CenterReceives = ch.Total - ch.MPCost - ch.MarketplaceFee
	sim.PlatformNet = ch.MarketplaceFee
	return sim
}
