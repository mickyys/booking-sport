package app

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"github.com/hamp/booking-sport/pkg/logger"
	"github.com/hamp/booking-sport/pkg/mercadopago"
)

// processMercadoPagoRefund procesa el reembolso en Mercado Pago al cancelar una
// reserva: calcula el monto según la política de retención, ejecuta el reembolso
// (total o parcial) con el token del vendedor y registra el refund en la reserva.
func (uc *BookingUseCase) processMercadoPagoRefund(ctx context.Context, booking *domain.Booking, center *domain.SportCenter, refundPercentage int) error {
	log := logger.FromContext(ctx)

	if booking.PaymentMethod != "mercadopago" || booking.MPPaymentID == "" || refundPercentage <= 0 {
		return nil
	}

	mpPaymentID, convErr := strconv.Atoi(booking.MPPaymentID)
	if convErr != nil {
		return fmt.Errorf("invalid mp_payment_id '%s': %w", booking.MPPaymentID, convErr)
	}

	if center.MercadoPago == nil || center.MercadoPago.AccessToken == "" {
		return fmt.Errorf("mercadopago not configured for center %s, cannot process refund", center.Name)
	}

	mpClient := mercadopago.NewClient(center.MercadoPago.AccessToken)

	var refundResult *mercadopago.RefundResult
	var refundAmount float64
	if booking.IsPartialPayment {
		refundAmount = booking.PaidAmount
	} else {
		retentionAmount := (booking.Price * float64(100-refundPercentage)) / 100
		refundAmount = booking.PaidAmount - retentionAmount
		if refundAmount <= 0 {
			log.Infow("booking_cancel_no_refund",
				"msg", fmt.Sprintf("Cancelación sin reembolso para booking %s — pagado $%.0f, retención $%.0f",
					booking.BookingCode, booking.PaidAmount, retentionAmount),
				"booking_id", booking.ID.Hex(),
				"paid_amount", booking.PaidAmount,
				"retention_amount", retentionAmount,
			)
			refundAmount = 0
		}
	}

	if refundAmount > 0 {
		var refundErr error
		if refundAmount >= booking.PaidAmount {
			refundResult, refundErr = mpClient.CreateRefund(ctx, mpPaymentID)
		} else {
			refundResult, refundErr = mpClient.CreatePartialRefund(ctx, mpPaymentID, refundAmount)
		}
		if refundErr != nil {
			log.Errorw("booking_cancel_refund_error",
				"msg", fmt.Sprintf("Error al procesar reembolso de MercadoPago para booking %s", booking.BookingCode),
				"booking_id", booking.ID.Hex(),
				"error", refundErr,
			)
			return fmt.Errorf("error processing mercadopago refund: %w", refundErr)
		}
	}

	if refundResult != nil {
		log.Infow("booking_cancel_refund_processed",
			"msg", fmt.Sprintf("Reembolso procesado para booking %s — id %d, monto $%.0f, estado %s",
				booking.BookingCode, refundResult.ID, refundResult.Amount, refundResult.Status),
			"booking_id", booking.ID.Hex(),
			"refund_id", refundResult.ID,
			"status", refundResult.Status,
			"amount", refundResult.Amount,
		)

		mpRefund := domain.Refund{
			ID:        strconv.Itoa(refundResult.ID),
			Amount:    int(refundResult.Amount),
			Status:    refundResult.Status,
			CreatedAt: time.Now(),
		}
		if addErr := uc.repo.AddRefundByBookingID(ctx, booking.ID, mpRefund); addErr != nil {
			log.Errorw("booking_cancel_refund_save_error",
				"msg", fmt.Sprintf("Error al guardar registro de reembolso para booking %s", booking.BookingCode),
				"booking_id", booking.ID.Hex(),
				"error", addErr,
			)
		}
	}

	return nil
}
