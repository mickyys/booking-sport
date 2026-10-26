package app

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"github.com/hamp/booking-sport/pkg/mercadopago"
)

// computeAmountToPay resuelve cuánto se cobra ahora: el total o el abono
// configurado por el centro. Se usa tanto en el primer intento de pago como
// al reintentar una reserva pendiente con un descuento recalculado.
func computeAmountToPay(price float64, usePartialPayment bool, partialPaymentEnabled *bool, center *domain.SportCenter) (float64, bool) {
	if !usePartialPayment {
		return price, false
	}
	enabled := center.PartialPaymentEnabled
	if partialPaymentEnabled != nil {
		enabled = *partialPaymentEnabled
	}
	if !enabled {
		return price, false
	}
	percent := center.PartialPaymentPercent
	if percent <= 0 || percent > 100 {
		percent = 50
	}
	return (price * float64(percent)) / 100, true
}

// buildPreferenceMetadata construye la metadata asociada a una preferencia de
// pago, útil para auditar y correlacionar en el webhook.
func buildPreferenceMetadata(bookingCode, courtName, centerName, centerID string, hour int, date time.Time, isPartial bool) map[string]any {
	return map[string]any{
		"booking_code": bookingCode,
		"court":        courtName,
		"center":       centerName,
		"center_id":    centerID,
		"hour":         hour,
		"date":         date.Format("02/01/2006"),
		"is_partial":   isPartial,
	}
}

// slotTimeLabel describe el horario de la reserva para títulos de pago.
func slotTimeLabel(court *domain.Court, hour int) string {
	if court.IsFacility() {
		return "día completo"
	}
	return fmt.Sprintf("%02d:00 hrs", hour)
}

func (uc *BookingUseCase) CreateMercadoPagoPayment(ctx context.Context, booking *domain.Booking, usePartialPayment bool) (string, error) {
	court, err := uc.courtRepo.FindAnyByID(ctx, booking.CourtID)
	if err != nil {
		return "", fmt.Errorf("court not found: %w", err)
	}

	loc := domain.GetSantiagoLocation()
	booking.Date = time.Date(booking.Date.In(loc).Year(), booking.Date.In(loc).Month(), booking.Date.In(loc).Day(), 0, 0, 0, 0, loc)

	applyFacilityDefaults(court, booking)
	if err := booking.NormalizeSchedule(); err != nil {
		return "", err
	}
	if err := uc.ensureFacilityDayOpen(ctx, court, booking.Date); err != nil {
		return "", err
	}

	minutes := booking.Minutes
	if booking.DurationMinutes <= 0 {
		booking.DurationMinutes = 60
	}

	price := 0.0
	resolved := false
	var partialPaymentEnabled *bool

	// Las facilities se cobran por día completo: el precio vive en el espacio y
	// el pago online lo gobierna la modalidad del centro, no las franjas.
	if court.IsFacility() {
		if bookingIsPast(court, booking.Date, 0, 0, loc) {
			return "", fmt.Errorf("cannot book a past slot")
		}
		if court.DayPrice() <= 0 {
			return "", fmt.Errorf("este espacio no tiene precio configurado para pago online")
		}
		price = court.DayPrice()
		resolved = true
	}

	// Resolución por reglas (si el espacio tiene schedule_rules)
	if !resolved && uc.scheduleResolver != nil {
		franja, hasRules, err := uc.scheduleResolver.FindFranjaForBooking(ctx, court.ID, booking.Date, booking.Hour, minutes, booking.DurationMinutes)
		if err != nil {
			return "", err
		}
		if hasRules {
			if franja == nil {
				return "", fmt.Errorf("hour %02d:%02d not found in schedule", booking.Hour, minutes)
			}
			bookingDateTime := time.Date(booking.Date.Year(), booking.Date.Month(), booking.Date.Day(), booking.Hour, minutes, 0, 0, loc)
			if bookingDateTime.Before(time.Now().In(loc)) {
				return "", fmt.Errorf("cannot book a past slot")
			}
			if !franja.PaymentRequired && !franja.PaymentOptional {
				return "", fmt.Errorf("payment not enabled for this slot, use standard booking")
			}
			price = franja.Price
			partialPaymentEnabled = franja.PartialPaymentEnabled
			resolved = true
		}
	}

	// Resolución legacy por slot de hora
	if !resolved {
		for _, s := range court.Schedule {
			if s.Hour == booking.Hour {
				bookingDateTime := time.Date(booking.Date.Year(), booking.Date.Month(), booking.Date.Day(), booking.Hour, minutes, 0, 0, loc)
				if bookingDateTime.Before(time.Now().In(loc)) {
					return "", fmt.Errorf("cannot book a past slot")
				}

				if s.Status != "available" {
					return "", fmt.Errorf("hour %d is not available", booking.Hour)
				}

				if !s.PaymentRequired && !s.PaymentOptional {
					return "", fmt.Errorf("payment not enabled for this slot, use standard booking")
				}

				price = s.Price
				partialPaymentEnabled = s.PartialPaymentEnabled
				resolved = true
				break
			}
		}
		if !resolved {
			return "", fmt.Errorf("hour %d not found in schedule", booking.Hour)
		}
	}

	if err := uc.checkConfirmedIntervalConflict(ctx, booking, minutes); err != nil {
		return "", err
	}

	center, err := uc.centerRepo.FindByID(ctx, court.SportCenterID)
	if err != nil {
		return "", fmt.Errorf("sport center not found: %w", err)
	}

	// La modalidad "solo reservas" desactiva explícitamente el cobro online.
	// Los centros legacy sin modalidad configurada mantienen el comportamiento
	// anterior y se resuelven por los flags de cada franja.
	if center.PaymentMode == PaymentModePayAtCenter {
		return "", fmt.Errorf("online payments are disabled for this sport center")
	}

	var quote *QuoteResult
	if uc.pricing != nil {
		quote, err = uc.quoteForBooking(ctx, court, center, booking, minutes)
		if err != nil {
			return "", err
		}
		applyQuoteToBooking(booking, quote)
		price = booking.FinalPrice
	}

	amountToPay, isPartial := computeAmountToPay(price, usePartialPayment, partialPaymentEnabled, center)

	if quote == nil {
		booking.Price = price
		booking.FinalPrice = price
	}
	applyCommissionSnapshot(booking, centerCommissionRate(center), amountToPay)
	booking.PaidAmount = 0
	booking.PendingAmount = price
	booking.IsPartialPayment = isPartial
	booking.Status = domain.BookingStatusPending
	booking.BookingCode = generateBookingCode()
	booking.PaymentMethod = "mercadopago"
	booking.SportCenterID = court.SportCenterID
	booking.SportCenterName = center.Name
	booking.CourtName = court.Name
	booking.CreatedAt = time.Now()
	booking.UpdatedAt = time.Now()

	if center.MercadoPago == nil || center.MercadoPago.AccessToken == "" {
		return "", fmt.Errorf("mercadopago not configured for this sport center")
	}

	email := "cliente@email.com"
	if booking.GuestDetails != nil {
		email = booking.GuestDetails.Email
		if booking.CustomerName == "" {
			booking.CustomerName = booking.GuestDetails.Name
		}
		if booking.CustomerPhone == "" {
			booking.CustomerPhone = booking.GuestDetails.Phone
		}
	}

	effectiveUserID := resolveEffectiveUserID(booking)
	if booking.GuestDeviceID == "" {
		booking.GuestDeviceID = effectiveUserID
	}

	hold, existingBooking, err := uc.ClaimOrRenewSlot(ctx, booking.CourtID, booking.Date, booking.Hour, booking.Minutes, effectiveUserID)
	if err != nil {
		return "", err
	}

	if existingBooking != nil {
		existingBooking.Date = time.Date(existingBooking.Date.In(loc).Year(), existingBooking.Date.In(loc).Month(), existingBooking.Date.In(loc).Day(), 0, 0, 0, 0, loc)
		existingBooking.UserID = booking.UserID
		existingBooking.GuestDetails = booking.GuestDetails
		existingBooking.CustomerName = booking.CustomerName
		existingBooking.CustomerPhone = booking.CustomerPhone
		// Se libera el uso anterior antes de recotizar: el contador por
		// persona incluye esta misma reserva y, sin liberarlo, un código de un
		// solo uso se rechazaría al reintentar el pago.
		previousQuote := quote
		uc.releaseDiscount(ctx, existingBooking.ID)
		if uc.pricing != nil {
			quote, err = uc.quoteForBooking(ctx, court, center, booking, minutes)
			if err != nil {
				if previousQuote != nil {
					_ = uc.reserveDiscount(ctx, previousQuote, existingBooking)
				}
				return "", err
			}
			applyQuoteToBooking(booking, quote)
			price = booking.FinalPrice
			amountToPay, isPartial = computeAmountToPay(price, usePartialPayment, partialPaymentEnabled, center)
		}
		if quote != nil {
			applyQuoteToBooking(existingBooking, quote)
			if err := uc.reserveDiscount(ctx, quote, existingBooking); err != nil {
				return "", err
			}
		} else {
			existingBooking.FinalPrice = price
			existingBooking.Price = price
		}
		applyCommissionSnapshot(existingBooking, centerCommissionRate(center), amountToPay)
		existingBooking.PaidAmount = 0
		existingBooking.PendingAmount = price
		existingBooking.IsPartialPayment = isPartial
		existingBooking.PaymentMethod = "mercadopago"
		existingBooking.UpdatedAt = time.Now()

		if hold == nil {
			newHold := &domain.SlotHold{
				CourtID:   booking.CourtID,
				Date:      booking.Date,
				Hour:      booking.Hour,
				Minutes:   booking.Minutes,
				UserID:    effectiveUserID,
				BookingID: existingBooking.ID,
				ExpiresAt: time.Now().Add(15 * time.Minute),
				CreatedAt: time.Now(),
			}
			claimed, err := uc.holdRepo.TryClaimSlot(ctx, newHold)
			if err != nil || claimed == nil {
				return "", domain.NewConflictError("no se pudo asegurar el bloque para continuar el pago, intentalo nuevamente")
			}
			hold = claimed
		}

		lockExpires := hold.ExpiresAt
		existingBooking.LockExpiresAt = &lockExpires
		existingBooking.HoldID = &hold.ID

		if err := uc.holdOccupancy(ctx, existingBooking, mpHoldTTL); err != nil {
			return "", err
		}
		uc.repo.Update(ctx, existingBooking)

		client := mercadopago.NewClient(center.MercadoPago.AccessToken)

		urlFrontend := os.Getenv("URL_FRONTEND")
		notificationURL := os.Getenv("URL_MP_WEBHOOK")
		urlPaymentCallback := os.Getenv("URL_MP_CALLBACK")

		successURL := fmt.Sprintf("%s?code=%s", urlPaymentCallback, existingBooking.BookingCode)
		failureURL := fmt.Sprintf("%s/booking/failure", urlFrontend)
		pendingURL := fmt.Sprintf("%s?code=%s", urlPaymentCallback, existingBooking.BookingCode)

		title := fmt.Sprintf("Reserva %s - %s - %s %s", court.Name, center.Name, existingBooking.Date.Format("02/01/2006"), slotTimeLabel(court, existingBooking.Hour))
		if isPartial {
			title = fmt.Sprintf("Abono Reserva %s - %s - %s %s", court.Name, center.Name, existingBooking.Date.Format("02/01/2006"), slotTimeLabel(court, existingBooking.Hour))
		}
		externalRef := existingBooking.BookingCode
		payerName, payerSurname := splitFullName(existingBooking.CustomerName)

		prefInput := mercadopago.PreferenceInput{
			Title:           title,
			Amount:          amountToPay,
			Email:           email,
			PayerName:       payerName,
			PayerSurname:    payerSurname,
			ExternalRef:     externalRef,
			SuccessURL:      successURL,
			FailureURL:      failureURL,
			PendingURL:      pendingURL,
			NotificationURL: notificationURL,
			ExpirationDate:  &lockExpires,
			MarketplaceFee:  commissionFee(amountToPay, centerCommissionRate(center)),
			Metadata:        buildPreferenceMetadata(existingBooking.BookingCode, court.Name, center.Name, center.ID.Hex(), existingBooking.Hour, existingBooking.Date, isPartial),
		}

		result, err := client.CreatePreference(ctx, prefInput)
		if err != nil {
			return "", fmt.Errorf("error creating mercadopago preference: %w", err)
		}

		existingBooking.MPPreferenceID = result.ID
		uc.repo.Update(ctx, existingBooking)
		return result.InitPoint, nil
	}

	lockExpires := hold.ExpiresAt
	booking.HoldID = &hold.ID
	booking.LockExpiresAt = &lockExpires

	if err := uc.holdOccupancy(ctx, booking, mpHoldTTL); err != nil {
		uc.holdRepo.Delete(ctx, hold.ID)
		return "", err
	}
	if err := uc.reserveDiscount(ctx, quote, booking); err != nil {
		uc.holdRepo.Delete(ctx, hold.ID)
		uc.releaseOccupancy(ctx, booking)
		return "", err
	}
	if err := uc.repo.Create(ctx, booking); err != nil {
		uc.holdRepo.Delete(ctx, hold.ID)
		uc.releaseOccupancy(ctx, booking)
		uc.releaseDiscount(ctx, booking.ID)
		return "", err
	}

	hold.BookingID = booking.ID

	client := mercadopago.NewClient(center.MercadoPago.AccessToken)

	urlFrontend := os.Getenv("URL_FRONTEND")
	notificationURL := os.Getenv("URL_MP_WEBHOOK")
	urlPaymentCallback := os.Getenv("URL_MP_CALLBACK")

	successURL := fmt.Sprintf("%s?code=%s", urlPaymentCallback, booking.BookingCode)
	failureURL := fmt.Sprintf("%s/booking/failure", urlFrontend)
	pendingURL := fmt.Sprintf("%s?code=%s", urlPaymentCallback, booking.BookingCode)

	title := fmt.Sprintf("Reserva %s - %s - %s %s", court.Name, center.Name, booking.Date.Format("02/01/2006"), slotTimeLabel(court, booking.Hour))
	if isPartial {
		title = fmt.Sprintf("Abono Reserva %s - %s - %s %s", court.Name, center.Name, booking.Date.Format("02/01/2006"), slotTimeLabel(court, booking.Hour))
	}
	externalRef := booking.BookingCode
	payerName, payerSurname := splitFullName(booking.CustomerName)

	prefInput := mercadopago.PreferenceInput{
		Title:           title,
		Amount:          amountToPay,
		Email:           email,
		PayerName:       payerName,
		PayerSurname:    payerSurname,
		ExternalRef:     externalRef,
		SuccessURL:      successURL,
		FailureURL:      failureURL,
		PendingURL:      pendingURL,
		NotificationURL: notificationURL,
		ExpirationDate:  &lockExpires,
		MarketplaceFee:  commissionFee(amountToPay, centerCommissionRate(center)),
		Metadata:        buildPreferenceMetadata(booking.BookingCode, court.Name, center.Name, center.ID.Hex(), booking.Hour, booking.Date, isPartial),
	}

	result, err := client.CreatePreference(ctx, prefInput)
	if err != nil {
		return "", fmt.Errorf("error creating mercadopago preference: %w", err)
	}

	booking.MPPreferenceID = result.ID

	return result.InitPoint, nil
}

// StoreMPPaymentID guarda el mp_payment_id en la reserva a partir del booking code.
// Debe llamarse antes de HandleMercadoPagoWebhook cuando se conoce el bookingCode (ej. desde MercadoPagoReturn).
func (uc *BookingUseCase) StoreMPPaymentID(ctx context.Context, bookingCode, paymentIDStr string) error {
	booking, err := uc.repo.FindByBookingCode(ctx, bookingCode)
	if err != nil {
		return fmt.Errorf("booking not found for code %s: %w", bookingCode, err)
	}
	return uc.repo.UpdateMPPaymentID(ctx, booking.ID, paymentIDStr)
}

func (uc *BookingUseCase) MockConfirmPayment(ctx context.Context, bookingCode, status string) (*domain.MockPaymentResult, error) {
	booking, err := uc.repo.FindByBookingCode(ctx, bookingCode)
	if err != nil {
		return nil, fmt.Errorf("booking not found: %w", err)
	}

	paymentID := fmt.Sprintf("MOCK-%d", time.Now().UnixMilli())

	switch status {
	case "approved":
		amount := booking.ChargedPrice()
		paymentInfo := &domain.PaymentInfo{
			PaymentMethodID:   "mock_credit_card",
			PaymentMethodType: "credit_card",
			Installments:      1,
			InstallmentAmount: amount,
			NetReceivedAmount: amount,
			CardLastFour:      "2580",
			CardholderName:    "Test User",
		}

		if err := uc.repo.UpdateMPPaymentID(ctx, booking.ID, paymentID); err != nil {
			return nil, fmt.Errorf("failed to store mock payment id: %w", err)
		}

		if err := uc.beginConfirm(ctx, booking); err != nil {
			return nil, fmt.Errorf("failed to confirm booking: %w", err)
		}
		if err := uc.repo.ConfirmPayment(ctx, booking.ID, domain.BookingStatusConfirmed, amount, 0, paymentInfo); err != nil {
			return nil, fmt.Errorf("failed to confirm booking: %w", err)
		}
		uc.confirmDiscount(ctx, booking.ID)
		uc.finishConfirm(ctx, booking)

	case "rejected":
		if err := uc.repo.UpdateMPPaymentID(ctx, booking.ID, paymentID); err != nil {
			return nil, fmt.Errorf("failed to store mock payment id: %w", err)
		}

		if err := uc.repo.ConfirmPayment(ctx, booking.ID, domain.BookingStatusCancelled, 0, booking.ChargedPrice(), nil); err != nil {
			return nil, fmt.Errorf("failed to cancel booking: %w", err)
		}
		uc.releaseDiscount(ctx, booking.ID)
		uc.releaseOccupancy(ctx, booking)

	default:
		return nil, fmt.Errorf("invalid status: %s", status)
	}

	return &domain.MockPaymentResult{
		Status:      status,
		PaymentID:   paymentID,
		BookingCode: bookingCode,
	}, nil
}

func splitFullName(fullName string) (string, string) {
	if fullName == "" {
		return "", ""
	}
	parts := strings.Fields(fullName)
	if len(parts) == 1 {
		return parts[0], ""
	}
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	firstName := parts[0]
	lastName := strings.Join(parts[1:], " ")
	return firstName, lastName
}
