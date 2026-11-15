package app

import (
	"fmt"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
)

// Ventana de pago de Mercado Pago.
//
// La línea de tiempo de un intento de pago se cuenta desde su ancla (T0: el
// created_at de la reserva pendiente o, si es nueva, el momento del bloqueo):
//
//	[-------------- ventana base 15m --------------][-- extensión 5m --]
//	T0                                             T0+15             T0+20
//
// Si el cliente vuelve a la pasarela después de los primeros 10 minutos (2/3
// de la ventana base), la ventana se extiende una sola vez hasta T0+20. Nunca
// puede pasar de ahí. Al vencer la ventana el link de Mercado Pago deja de
// aceptar pagos, pero el horario sigue bloqueado durante la gracia para cubrir
// pagos ya enviados que todavía se están procesando.
const (
	// mpPaymentExtension es la extensión única de la ventana de pago.
	mpPaymentExtension = 5 * time.Minute
	// mpHoldGrace es el tiempo extra que el horario queda bloqueado después de
	// que la ventana de pago vence.
	mpHoldGrace = 5 * time.Minute
)

// mpPaymentExtendAfter es el punto (2/3 de la ventana base) desde el cual un
// reintento extiende la ventana de pago.
func mpPaymentExtendAfter() time.Duration {
	return mpHoldTTL * 2 / 3
}

// paymentDeadline devuelve cuándo deja de aceptarse el pago para un intento
// anclado en anchor. Si now ya pasó el tope, devuelve el tope (una fecha
// pasada), lo que significa que la ventana está cerrada.
func paymentDeadline(anchor, now time.Time) time.Time {
	base := anchor.Add(mpHoldTTL)
	if now.Before(anchor.Add(mpPaymentExtendAfter())) {
		return base
	}
	return base.Add(mpPaymentExtension)
}

// holdDeadline es cuándo se libera el horario: el fin de la ventana de pago
// más la gracia.
func holdDeadline(anchor, now time.Time) time.Time {
	return paymentDeadline(anchor, now).Add(mpHoldGrace)
}

// paymentAnchor resuelve desde cuándo se cuenta la ventana de pago. Sin
// created_at (datos antiguos o reservas armadas fuera del flujo normal) se
// deduce del vencimiento del bloqueo, que siempre incluye la gracia. Se usa la
// interpretación más generosa (ventana base) para no vencer un pago antes de
// tiempo.
func paymentAnchor(b *domain.Booking, now time.Time) time.Time {
	if b == nil {
		return now
	}
	if !b.CreatedAt.IsZero() && !b.CreatedAt.After(now) {
		return b.CreatedAt
	}
	if b.LockExpiresAt != nil {
		return b.LockExpiresAt.Add(-(mpHoldTTL + mpHoldGrace))
	}
	return now
}

// lockDetails normaliza el vencimiento del bloqueo y arma los datos que viajan
// al cliente para la cuenta regresiva.
func lockDetails(lockEnd, now time.Time) (time.Duration, time.Time, map[string]any) {
	if lockEnd.Before(now) {
		lockEnd = now
	}
	remaining := lockEnd.Round(time.Second).Sub(now.Round(time.Second))
	if remaining < 0 {
		remaining = 0
	}
	return remaining, lockEnd, map[string]any{
		"lock_expires_at":     lockEnd.UTC().Format(time.RFC3339),
		"retry_after_seconds": int((remaining + time.Second - 1) / time.Second),
	}
}

// slotInPaymentError arma el 409 que ve el segundo usuario. Incluye el tiempo
// restante y el instante exacto de liberación para que el frontend pueda hacer
// una cuenta regresiva sin interpretar el texto.
func slotInPaymentError(lockEnd, now time.Time) *domain.AppError {
	remaining, lockEnd, details := lockDetails(lockEnd, now)
	details["code"] = "slot_in_payment"

	var message string
	switch {
	case remaining <= 0:
		message = "otro usuario esta en proceso de pago; el horario se libera en instantes, vuelve a intentarlo"
	case remaining < time.Minute:
		message = "otro usuario esta en proceso de pago; el horario se libera en menos de un minuto, vuelve a intentarlo"
	default:
		minutes := int((remaining + time.Minute - 1) / time.Minute)
		unit := "minutos"
		if minutes == 1 {
			unit = "minuto"
		}
		release := lockEnd.In(domain.GetSantiagoLocation()).Format("15:04")
		message = fmt.Sprintf(
			"otro usuario esta en proceso de pago; si no confirma, el horario se libera en %d %s (a las %s). Vuelve a intentarlo entonces",
			minutes, unit, release)
	}

	return domain.NewConflictErrorWithDetails(message, details)
}

// paymentWindowExpiredError es el 409 del propio cliente que vuelve cuando su
// ventana de pago ya venció pero el bloqueo sigue vigente (gracia). Le dice
// cuándo podrá reintentar en vez de dejarlo sin información.
func paymentWindowExpiredError(lockEnd, now time.Time) *domain.AppError {
	_, lockEnd, details := lockDetails(lockEnd, now)
	details["code"] = "payment_window_expired"
	release := lockEnd.In(domain.GetSantiagoLocation()).Format("15:04")

	return domain.NewConflictErrorWithDetails(
		fmt.Sprintf("tu tiempo para pagar venció; el horario se libera a las %s para cubrir pagos ya enviados. Vuelve a intentarlo entonces", release),
		details)
}

// holdTTLUntil devuelve el TTL a usar al renovar el bloqueo en el coordinador
// de ocupación. Si el vencimiento ya pasó usa la ventana por defecto: la
// confirmación de pago volverá a intentar adquirir el horario.
func holdTTLUntil(expiresAt time.Time) time.Duration {
	if ttl := time.Until(expiresAt); ttl > 0 {
		return ttl
	}
	return mpHoldTTL
}
