package app

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"github.com/hamp/booking-sport/pkg/logger"
	"github.com/hamp/booking-sport/pkg/mercadopago"
	"github.com/mercadopago/sdk-go/pkg/payment"
)

// Estados de pago de Mercado Pago que expone la reconciliación.
const (
	MPStatusNotFound = "not_found"
	MPStatusError    = "error"

	// paymentStatusMinInterval evita golpear la API de MP cuando el cliente
	// consulta el estado en ráfaga (polling, recargas).
	paymentStatusMinInterval = 3 * time.Second
)

// PaymentReconcileResult es el estado de una reserva tras consultar Mercado Pago.
type PaymentReconcileResult struct {
	BookingCode       string               `json:"booking_code"`
	BookingStatus     domain.BookingStatus `json:"booking_status"`
	MPStatus          string               `json:"mp_status,omitempty"`
	HasPayment        bool                 `json:"has_payment"`
	PaidAmount        float64              `json:"paid_amount"`
	PendingAmount     float64              `json:"pending_amount"`
	EmailSent         bool                 `json:"email_sent"`
	Refunded          bool                 `json:"refunded,omitempty"`
	LastPaymentSyncAt *time.Time           `json:"last_payment_sync_at,omitempty"`
}

func newReconcileResult(b *domain.Booking) *PaymentReconcileResult {
	return &PaymentReconcileResult{
		BookingCode:       b.BookingCode,
		BookingStatus:     b.Status,
		MPStatus:          b.MPStatus,
		HasPayment:        b.MPPaymentID != "",
		PaidAmount:        b.PaidAmount,
		PendingAmount:     b.PendingAmount,
		EmailSent:         b.ConfirmationEmailSentAt != nil,
		LastPaymentSyncAt: b.LastPaymentSyncAt,
	}
}

// ReconcileMercadoPagoPayment consulta Mercado Pago (con el token del centro)
// por el booking_code y deja la reserva en el estado correcto. Es idempotente:
// la transición pending→confirmed y el correo se ejecutan una sola vez aunque
// webhook, retorno, cron o admin la invoquen en paralelo.
func (uc *BookingUseCase) ReconcileMercadoPagoPayment(ctx context.Context, bookingCode string) (*PaymentReconcileResult, error) {
	return uc.reconcileMercadoPagoPayment(ctx, bookingCode, 0)
}

// ReconcileMercadoPagoPaymentThrottled es igual pero omite la consulta a MP si
// se sincronizó hace muy poco; pensado para endpoints invocados por el cliente.
func (uc *BookingUseCase) ReconcileMercadoPagoPaymentThrottled(ctx context.Context, bookingCode string) (*PaymentReconcileResult, error) {
	return uc.reconcileMercadoPagoPayment(ctx, bookingCode, paymentStatusMinInterval)
}

func (uc *BookingUseCase) reconcileMercadoPagoPayment(ctx context.Context, bookingCode string, minInterval time.Duration) (*PaymentReconcileResult, error) {
	store, ok := uc.repo.(PaymentReconcileStore)
	if !ok {
		return nil, errors.New("payment reconciliation not supported by repository")
	}
	log := logger.FromContext(ctx)

	booking, err := uc.repo.FindByBookingCode(ctx, bookingCode)
	if err != nil {
		return nil, fmt.Errorf("booking not found for code %s: %w", bookingCode, err)
	}
	res := newReconcileResult(booking)

	if booking.PaymentMethod != "mercadopago" {
		return res, nil
	}

	center, err := uc.centerRepo.FindByID(ctx, booking.SportCenterID)
	if err != nil {
		return res, fmt.Errorf("sport center not found for booking %s: %w", booking.BookingCode, err)
	}

	if booking.Status == domain.BookingStatusConfirmed {
		res.EmailSent = uc.sendConfirmationEmailOnce(ctx, store, booking, center) || res.EmailSent
		return res, nil
	}
	if booking.Status != domain.BookingStatusPending && booking.Status != domain.BookingStatusExpired {
		return res, nil
	}
	if minInterval > 0 && booking.LastPaymentSyncAt != nil && time.Since(*booking.LastPaymentSyncAt) < minInterval {
		return res, nil
	}
	if center.MercadoPago == nil || center.MercadoPago.AccessToken == "" {
		return res, fmt.Errorf("mercadopago not configured for center %s", center.Name)
	}

	client := mercadopago.NewClient(center.MercadoPago.AccessToken)
	pay, err := resolveMPPayment(ctx, client, booking)
	if err != nil {
		res.MPStatus = MPStatusError
		log.Warnw("mp_reconcile_lookup_error", "booking_code", booking.BookingCode, "error", err)
		return res, err
	}
	now := time.Now()
	if pay == nil {
		_ = store.MarkPaymentSync(ctx, booking.ID, MPStatusNotFound, "", now)
		res.MPStatus = MPStatusNotFound
		res.LastPaymentSyncAt = &now
		return res, nil
	}

	paymentIDStr := strconv.Itoa(pay.ID)
	res.MPStatus = pay.Status
	res.HasPayment = true
	res.LastPaymentSyncAt = &now

	if pay.Status != "approved" {
		// pending / in_process / authorized / rejected / cancelled: la reserva
		// sigue pendiente. El hold vence solo (el usuario puede reintentar el pago
		// con la misma preferencia) y el barrido de expiración la libera.
		if err := store.MarkPaymentSync(ctx, booking.ID, pay.Status, paymentIDStr, now); err != nil {
			log.Warnw("mp_reconcile_mark_sync_error", "booking_code", booking.BookingCode, "error", err)
		}
		log.Infow("mp_reconcile_not_approved",
			"booking_code", booking.BookingCode, "payment_id", paymentIDStr, "status", pay.Status)
		return res, nil
	}

	return uc.applyApprovedPayment(ctx, store, booking, center, pay, res)
}

func (uc *BookingUseCase) applyApprovedPayment(ctx context.Context, store PaymentReconcileStore, booking *domain.Booking, center *domain.SportCenter, pay *payment.Response, res *PaymentReconcileResult) (*PaymentReconcileResult, error) {
	log := logger.FromContext(ctx)
	originalStatus := booking.Status
	paymentIDStr := strconv.Itoa(pay.ID)

	charged := booking.ChargedPrice()
	paidAmount := pay.TransactionAmount
	pendingAmount := charged - paidAmount
	if pendingAmount < 0 {
		pendingAmount = 0
	}

	paymentInfo := &domain.PaymentInfo{
		PaymentMethodID:   pay.PaymentMethodID,
		PaymentMethodType: pay.PaymentMethod.Type,
		Installments:      pay.Installments,
		InstallmentAmount: pay.TransactionDetails.InstallmentAmount,
		NetReceivedAmount: pay.TransactionDetails.NetReceivedAmount,
	}
	if pay.Card.ID != "" {
		paymentInfo.CardLastFour = pay.Card.LastFourDigits
		paymentInfo.CardholderName = pay.Card.Cardholder.Name
	}

	late := false
	if originalStatus == domain.BookingStatusExpired {
		if other, _ := uc.repo.FindConfirmedBySlot(ctx, booking.CourtID, booking.Date, booking.Hour, booking.Minutes); other != nil {
			late = true
		}
	}
	if !late {
		if err := uc.beginConfirm(ctx, booking); err != nil {
			if !errors.Is(err, ErrLatePaymentConflict) {
				return res, fmt.Errorf("error confirming occupancy: %w", err)
			}
			late = true
		}
	}

	next := domain.BookingStatusConfirmed
	if late {
		next = domain.BookingStatusCancelled
		paymentInfo = &domain.PaymentInfo{
			PaymentMethodID:   paymentInfo.PaymentMethodID,
			PaymentMethodType: paymentInfo.PaymentMethodType,
		}
		log.Errorw("mp_late_payment_conflict",
			"msg", fmt.Sprintf("Pago aprobado para booking %s pero el horario ya no está disponible; se reembolsará", booking.BookingCode),
			"booking_code", booking.BookingCode, "payment_id", paymentIDStr)
	} else {
		rate := center.MarketplaceFeePercent
		if rate <= 0 || rate > 100 {
			rate = defaultMarketplaceFeePercent
		}
		paymentInfo.CommissionRate = rate
		paymentInfo.CommissionAmount = marketplaceFeeForAmount(paidAmount, rate)
	}

	claimPending := pendingAmount
	if late {
		claimPending = charged
	}
	claimed, err := store.ClaimPaymentTransition(ctx, booking.ID, originalStatus, next, paidAmount, claimPending, paymentInfo, paymentIDStr, pay.Status)
	if err != nil {
		if !late {
			uc.releaseOccupancy(ctx, booking)
		}
		return res, fmt.Errorf("error updating booking status: %w", err)
	}
	if !claimed {
		// Otro proceso ya resolvió la reserva: no se repiten efectos.
		if fresh, ferr := uc.repo.FindByBookingCode(ctx, booking.BookingCode); ferr == nil {
			return newReconcileResult(fresh), nil
		}
		return res, nil
	}

	booking.Status = next
	booking.MPPaymentID = paymentIDStr
	booking.MPStatus = pay.Status
	booking.PaidAmount = paidAmount
	booking.PendingAmount = claimPending
	booking.PaymentInfo = paymentInfo
	res.BookingStatus = next
	res.PaidAmount = paidAmount
	res.PendingAmount = claimPending
	res.HasPayment = true

	if booking.HoldID != nil {
		uc.holdRepo.Delete(ctx, *booking.HoldID)
	}

	if late {
		booking.IsPartialPayment = false
		if rerr := uc.processMercadoPagoRefund(ctx, booking, center, 100); rerr != nil {
			log.Errorw("mp_late_payment_refund_failed", "booking_code", booking.BookingCode, "error", rerr)
		} else {
			res.Refunded = true
		}
		uc.releaseDiscount(ctx, booking.ID)
		uc.releaseOccupancy(ctx, booking)
		return res, nil
	}

	uc.confirmDiscount(ctx, booking.ID)
	uc.finishConfirm(ctx, booking)

	log.Infow("mp_reconcile_booking_confirmed",
		"msg", fmt.Sprintf("Reserva confirmada via MercadoPago: %s en %s para el %s a las %02d:00 hrs (estaba %s)",
			booking.BookingCode, booking.SportCenterName, booking.Date.Format("02/01/2006"), booking.Hour, originalStatus),
		"booking_code", booking.BookingCode,
		"payment_id", paymentIDStr,
		"paid_amount", paidAmount,
		"pending_amount", pendingAmount,
	)

	uc.notifyAdmins(ctx, booking, "Pago Confirmado - MercadoPago",
		fmt.Sprintf("Nueva reserva en %s para el %s a las %02d:00 hrs.",
			booking.SportCenterName, booking.Date.Format("02/01/2006"), booking.Hour),
		"confirmation")

	res.EmailSent = uc.sendConfirmationEmailOnce(ctx, store, booking, center)
	return res, nil
}

// resolveMPPayment busca el pago de la reserva: primero por mp_payment_id y, si
// no existe o falla, por external_reference (= booking_code). Prefiere un pago
// aprobado; devuelve nil si MP no tiene pagos para la reserva.
func resolveMPPayment(ctx context.Context, client *mercadopago.Client, booking *domain.Booking) (*payment.Response, error) {
	var lookupErr error
	if booking.MPPaymentID != "" {
		if id, err := strconv.Atoi(booking.MPPaymentID); err == nil {
			p, err := client.GetPayment(ctx, id)
			if err == nil && (p.ExternalReference == "" || p.ExternalReference == booking.BookingCode) {
				if p.Status == "approved" {
					return p, nil
				}
			} else if err != nil {
				lookupErr = err
			}
		}
	}

	search, err := client.SearchPaymentsByExternalReference(ctx, booking.BookingCode)
	if err != nil {
		if lookupErr != nil {
			return nil, lookupErr
		}
		return nil, err
	}
	var first *payment.Response
	for i := range search.Results {
		p := search.Results[i]
		if p.ExternalReference != "" && p.ExternalReference != booking.BookingCode {
			continue
		}
		if p.Status == "approved" {
			return &p, nil
		}
		if first == nil {
			first = &p
		}
	}
	return first, nil
}

// sendConfirmationEmailOnce envía el correo de confirmación una sola vez por
// reserva. Reclama el envío de forma atómica; si Mailgun falla libera la marca
// para que la próxima reconciliación reintente. Devuelve true si se reclamó.
func (uc *BookingUseCase) sendConfirmationEmailOnce(ctx context.Context, store PaymentReconcileStore, booking *domain.Booking, center *domain.SportCenter) bool {
	if uc.mailer == nil || booking.ConfirmationEmailSentAt != nil {
		return false
	}
	if booking.GuestDetails == nil || booking.GuestDetails.Email == "" {
		return false
	}
	claimed, err := store.ClaimConfirmationEmail(ctx, booking.ID, time.Now())
	if err != nil || !claimed {
		return false
	}

	cancellationHours := center.CancellationHours
	if cancellationHours == 0 {
		cancellationHours = 3
	}
	retentionPercent := center.RetentionPercent
	if retentionPercent == 0 {
		retentionPercent = 10
	}
	paid := booking.PaidAmount
	pending := booking.PendingAmount
	b := *booking
	go func() {
		bg := context.Background()
		if err := uc.mailer.SendBookingConfirmation(bg, &b, cancellationHours, retentionPercent, paid, pending); err != nil {
			logger.FromContext(bg).Errorw("mail_booking_confirmation_error",
				"msg", fmt.Sprintf("Error al enviar correo de confirmación para booking %s", b.BookingCode),
				"booking_code", b.BookingCode,
				"error", err,
			)
			if rerr := store.ReleaseConfirmationEmail(bg, b.ID); rerr != nil {
				logger.FromContext(bg).Errorw("mail_release_claim_error", "booking_code", b.BookingCode, "error", rerr)
			}
		}
	}()
	return true
}

// resolveBookingCodeFromPayment obtiene el booking_code de un pago notificado
// por webhook. El webhook solo trae el payment_id, y el pago vive en la cuenta
// del centro vendedor: se prueba el token de plataforma y luego el de cada
// centro con Mercado Pago conectado.
func (uc *BookingUseCase) resolveBookingCodeFromPayment(ctx context.Context, paymentIDStr string) (string, error) {
	if b, err := uc.repo.FindByMPPaymentID(ctx, paymentIDStr); err == nil && b != nil {
		return b.BookingCode, nil
	}
	paymentID, err := strconv.Atoi(paymentIDStr)
	if err != nil {
		return "", fmt.Errorf("invalid payment ID: %w", err)
	}

	if uc.mpClient != nil {
		if p, err := uc.mpClient.GetPayment(ctx, paymentID); err == nil && p.ExternalReference != "" {
			return p.ExternalReference, nil
		}
	}

	centers, err := uc.centerRepo.FindAll(ctx)
	if err != nil {
		return "", fmt.Errorf("error listing sport centers: %w", err)
	}
	for i := range centers {
		c := &centers[i]
		if c.MercadoPago == nil || c.MercadoPago.AccessToken == "" {
			continue
		}
		p, err := mercadopago.NewClient(c.MercadoPago.AccessToken).GetPayment(ctx, paymentID)
		if err == nil && p.ExternalReference != "" {
			return p.ExternalReference, nil
		}
	}
	return "", fmt.Errorf("payment %s not found in any connected center", paymentIDStr)
}

// HandleMercadoPagoWebhook procesa una notificación de pago de Mercado Pago.
func (uc *BookingUseCase) HandleMercadoPagoWebhook(ctx context.Context, paymentIDStr string) error {
	code, err := uc.resolveBookingCodeFromPayment(ctx, paymentIDStr)
	if err != nil {
		return err
	}
	_, err = uc.ReconcileMercadoPagoPayment(ctx, code)
	return err
}

// SyncConfirmedPayment lo invoca el batch. No confía en los montos recibidos:
// verifica el pago contra Mercado Pago antes de confirmar.
func (uc *BookingUseCase) SyncConfirmedPayment(ctx context.Context, bookingCode string, mpPaymentID string, paidAmount, pendingAmount float64) error {
	res, err := uc.ReconcileMercadoPagoPayment(ctx, bookingCode)
	if err != nil {
		return err
	}
	if res.BookingStatus != domain.BookingStatusConfirmed {
		return fmt.Errorf("payment for booking %s is not approved (mp_status=%s)", bookingCode, res.MPStatus)
	}
	return nil
}

// ReconcilePendingMercadoPago reconcilia las reservas pendientes de pago online
// creadas en la ventana indicada. Lo usa el job periódico.
func (uc *BookingUseCase) ReconcilePendingMercadoPago(ctx context.Context, window time.Duration, limit int) (checked, confirmed int) {
	store, ok := uc.repo.(PaymentReconcileStore)
	if !ok {
		return 0, 0
	}
	now := time.Now()
	// Se ignoran las de menos de 1 min: el usuario aún está en el checkout.
	bookings, err := store.FindPendingMercadoPago(ctx, now.Add(-window), now.Add(-time.Minute), limit)
	if err != nil {
		logger.FromContext(ctx).Errorw("mp_reconcile_job_find_error", "error", err)
		return 0, 0
	}
	for i := range bookings {
		checked++
		res, err := uc.ReconcileMercadoPagoPayment(ctx, bookings[i].BookingCode)
		if err != nil {
			continue
		}
		if res.BookingStatus == domain.BookingStatusConfirmed {
			confirmed++
		}
	}
	return checked, confirmed
}

// RunPaymentReconcileLoop reconcilia periódicamente las reservas pendientes de
// pago online. El lock distribuido evita ejecuciones simultáneas entre réplicas.
func (uc *BookingUseCase) RunPaymentReconcileLoop(ctx context.Context, locks JobLockRepository, interval time.Duration) {
	const lockName = "mp-payment-reconcile"
	owner := fmt.Sprintf("%d", time.Now().UnixNano())
	log := logger.FromContext(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if locks != nil {
				got, err := locks.TryAcquire(ctx, lockName, owner, interval, time.Now())
				if err != nil || !got {
					continue
				}
			}
			checked, confirmed := uc.ReconcilePendingMercadoPago(ctx, 24*time.Hour, 200)
			if checked > 0 {
				log.Infow("mp_reconcile_job_done", "checked", checked, "confirmed", confirmed)
			}
			if locks != nil {
				_ = locks.Release(ctx, lockName, owner)
			}
		}
	}
}
