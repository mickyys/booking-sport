package app

import (
	"math"
	"sync/atomic"

	"github.com/hamp/booking-sport/internal/domain"
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
	rate, _ := center.EffectiveCommission(CommercialModelEnforced(), defaultMarketplaceFeePercent)
	return rate
}

// commissionFee calcula la comisión en moneda local para un porcentaje ya
// resuelto. No aplica defaults: 0% es 0.
func commissionFee(amount float64, ratePercent int) float64 {
	if ratePercent <= 0 || ratePercent > 100 {
		return 0
	}
	return math.Round(amount*float64(ratePercent)) / 100
}
