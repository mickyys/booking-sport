package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"github.com/hamp/booking-sport/pkg/fintoc"
	"github.com/hamp/booking-sport/pkg/mercadopago"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func generateBookingCode() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().Unix())
	}
	return hex.EncodeToString(b)
}

type BookingUseCase struct {
	repo                     BookingRepository
	courtRepo                CourtRepository
	centerRepo               SportCenterRepository
	userRepo                 UserRepository
	mailer                   Mailer
	recurringReservationRepo RecurringReservationRepository
}

func NewBookingUseCase(repo BookingRepository, courtRepo CourtRepository, centerRepo SportCenterRepository, userRepo UserRepository, mailer Mailer, recurringRepo RecurringReservationRepository) *BookingUseCase {
	return &BookingUseCase{
		repo:                     repo,
		courtRepo:                courtRepo,
		centerRepo:               centerRepo,
		userRepo:                 userRepo,
		mailer:                   mailer,
		recurringReservationRepo: recurringRepo,
	}
}

// GetUserCancelledBookingsPaged retorna solo las reservas canceladas del usuario
func (uc *BookingUseCase) GetUserCancelledBookingsPaged(ctx context.Context, userID string, page, limit int) ([]domain.BookingSummary, int64, error) {
	return uc.repo.FindByUserIDAndStatusPaged(ctx, userID, domain.BookingStatusCancelled, page, limit)
}

func (uc *BookingUseCase) DeleteSeries(ctx context.Context, seriesID string) error {
	return uc.repo.DeleteBySeriesID(ctx, seriesID)
}

func (uc *BookingUseCase) GetRecurringSeries(ctx context.Context, userID string, sportCenterID string) ([]domain.RecurringSeries, error) {
	// Si se especifica un centro, filtramos solo por ese centro
	if sportCenterID != "" {
		centerID, err := primitive.ObjectIDFromHex(sportCenterID)
		if err != nil {
			return nil, fmt.Errorf("invalid sport_center_id: %w", err)
		}
		return uc.repo.GetRecurringSeries(ctx, []primitive.ObjectID{centerID}, sportCenterID)
	}

	// Buscamos los centros deportivos asociados al usuario
	centers, err := uc.centerRepo.FindByUserID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("error al buscar centros del usuario: %w", err)
	}

	// Si el usuario no administra centros, retornamos lista vacía
	if len(centers) == 0 {
		return []domain.RecurringSeries{}, nil
	}

	// Extraemos solo los IDs para pasárselos al repositorio de bookings
	var centerIDs []primitive.ObjectID
	for _, c := range centers {
		centerIDs = append(centerIDs, c.ID)
	}

	return uc.repo.GetRecurringSeries(ctx, centerIDs, "")
}

func (uc *BookingUseCase) CreateFintocPaymentIntent(ctx context.Context, booking *domain.Booking) (string, error) {
	court, err := uc.courtRepo.FindByID(ctx, booking.CourtID)
	if err != nil {
		return "", fmt.Errorf("court not found: %w", err)
	}

	price := 0.0
	found := false
	for _, s := range court.Schedule {
		if s.Hour == booking.Hour {
			// Check if slot has already passed
			loc, _ := time.LoadLocation("America/Santiago")
			bookingDateTime := time.Date(booking.Date.Year(), booking.Date.Month(), booking.Date.Day(), booking.Hour, 0, 0, 0, loc)
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
			found = true
			break
		}
	}

	if !found {
		return "", fmt.Errorf("hour %d not found in schedule", booking.Hour)
	}

	booking.Price = price
	booking.FinalPrice = price
	booking.Status = domain.BookingStatusPending
	booking.BookingCode = generateBookingCode()
	booking.PaymentMethod = "fintoc"
	booking.SportCenterID = court.SportCenterID

	// Obtener el centro deportivo para sacar la secret key de Fintoc
	center, err := uc.centerRepo.FindByID(ctx, court.SportCenterID)
	if err != nil {
		return "", fmt.Errorf("sport center not found: %w", err)
	}

	booking.SportCenterName = center.Name
	booking.CourtName = court.Name
	booking.CreatedAt = time.Now()
	booking.UpdatedAt = time.Now()

	if center.Fintoc == nil || center.Fintoc.Payment.SecretKey == "" {
		return "", fmt.Errorf("fintoc payment not configured for this sport center")
	}

	fintocSecret := center.Fintoc.Payment.SecretKey
	urlPaymentCallback := os.Getenv("URL_PAYMENT_CALLBACK")

	client := fintoc.NewClient(fintocSecret)

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

	// successURL apunta al backend para validar y redirigir
	url := fmt.Sprintf("%s?id=%s", urlPaymentCallback, booking.BookingCode)

	orderID := fmt.Sprintf("booking-%s-%d", booking.CourtID.Hex(), booking.Hour)
	res, err := client.CreateCheckoutSession(int(booking.Price), "CLP", email, orderID, url, url)
	if err != nil {
		return "", fmt.Errorf("error creating fintoc checkout: %w", err)
	}

	booking.FintocPaymentID = res.ID

	if err := uc.repo.Create(ctx, booking); err != nil {
		return "", err
	}

	return res.RedirectURL, nil
}

func (uc *BookingUseCase) HandleFintocCheckoutFinished(ctx context.Context, checkoutSessionID string, paymentIntentID string) error {
	booking, err := uc.repo.FindByFintocPaymentID(ctx, checkoutSessionID)
	if err != nil {
		fmt.Printf("[FINTOC WEBHOOK ERROR] Reserva no encontrada para CheckoutSession ID: %s, Error: %v\n", checkoutSessionID, err)
		return err
	}

	fmt.Printf("[FINTOC WEBHOOK] Reserva encontrada para CheckoutSession ID: %s. Actualizando PaymentIntent ID a: %s\n", checkoutSessionID, paymentIntentID)

	err = uc.repo.UpdateFintocPaymentIntentID(ctx, booking.ID, paymentIntentID)
	if err != nil {
		fmt.Printf("[FINTOC WEBHOOK ERROR] Error al actualizar PaymentIntent ID para CheckoutSession %s: %v\n", checkoutSessionID, err)
		return err
	}

	return nil
}

func (uc *BookingUseCase) GetFintocPaymentStatus(ctx context.Context, paymentIntentID string) (string, error) {
	// Buscar la reserva para saber a qué centro pertenece
	booking, err := uc.repo.FindByFintocPaymentIntentID(ctx, paymentIntentID)
	if err != nil {
		return "", fmt.Errorf("booking not found for payment intent: %w", err)
	}

	// Obtener el centro para sacar la secret key
	center, err := uc.centerRepo.FindByID(ctx, booking.SportCenterID)
	if err != nil {
		return "", fmt.Errorf("sport center not found: %w", err)
	}

	if center.Fintoc == nil || center.Fintoc.Payment.SecretKey == "" {
		return "", fmt.Errorf("fintoc not configured for this center")
	}

	client := fintoc.NewClient(center.Fintoc.Payment.SecretKey)

	res, err := client.GetPaymentIntent(paymentIntentID)
	if err != nil {
		return "", err
	}

	return res.Status, nil
}

func (uc *BookingUseCase) ValidateFintocPaymentAndGetCode(ctx context.Context, bookingCode string) (string, error) {
	booking, err := uc.repo.FindByBookingCode(ctx, bookingCode)
	if err != nil {
		return "", fmt.Errorf("booking not found for code: %w", err)
	}

	// Si el estado ya es confirmado, redireccionamos directamente
	if booking.Status == domain.BookingStatusConfirmed {
		return booking.BookingCode, nil
	}

	// Si el estado es pendiente, consultamos Fintoc
	if booking.Status == domain.BookingStatusPending && booking.FintocPaymentID != "" {
		// Obtener el centro para sacar la secret key
		center, err := uc.centerRepo.FindByID(ctx, booking.SportCenterID)
		if err != nil {
			return booking.BookingCode, fmt.Errorf("sport center not found: %w", err)
		}

		if center.Fintoc == nil || center.Fintoc.Payment.SecretKey == "" {
			return booking.BookingCode, fmt.Errorf("fintoc not configured for this center")
		}

		client := fintoc.NewClient(center.Fintoc.Payment.SecretKey)

		// 1. Consultar la sesión de checkout
		session, err := client.GetCheckoutSession(booking.FintocPaymentID)
		if err != nil {
			return booking.BookingCode, fmt.Errorf("error getting checkout session: %w", err)
		}

		// 2. Verificar si la sesión terminó exitosamente y obtener el payment intent id
		if session.Status == "finished" && session.PaymentResource.PaymentIntent.ID != "" {
			paymentIntentID := session.PaymentResource.PaymentIntent.ID

			// 3. Consultar el pago (payment intent)
			payment, err := client.GetPaymentIntent(paymentIntentID)
			if err != nil {
				return booking.BookingCode, fmt.Errorf("error getting payment intent: %w", err)
			}

			// 4. Si el pago fue exitoso, actualizamos a confirmado
			if payment.Status == "succeeded" {
				booking.Status = domain.BookingStatusConfirmed
				booking.FintocPaymentIntentID = paymentIntentID
				booking.UpdatedAt = time.Now()
				if err := uc.repo.Update(ctx, booking); err != nil {
					return booking.BookingCode, fmt.Errorf("error updating booking: %w", err)
				}
				// Enviar correo de confirmación (si está configurado)
				if uc.mailer != nil {
					go func() {
						if err := uc.mailer.SendBookingConfirmation(context.Background(), booking); err != nil {
							log.Printf("[MAIL ERROR] sending booking confirmation: %v\n", err)
						}
					}()
				}
				return booking.BookingCode, nil
			}
		}
	}

	// Para cualquier otro caso (failed, cancelled o si sigue pendiente en Fintoc)
	return booking.BookingCode, nil
}

// ==================== MercadoPago Payment Methods ====================

func (uc *BookingUseCase) CreateMercadoPagoPayment(ctx context.Context, booking *domain.Booking, usePartialPayment bool) (string, error) {
	court, err := uc.courtRepo.FindByID(ctx, booking.CourtID)
	if err != nil {
		return "", fmt.Errorf("court not found: %w", err)
	}

	price := 0.0
	found := false
	var selectedSlot *domain.CourtSchedule
	for _, s := range court.Schedule {
		if s.Hour == booking.Hour {
			loc, _ := time.LoadLocation("America/Santiago")
			bookingDateTime := time.Date(booking.Date.Year(), booking.Date.Month(), booking.Date.Day(), booking.Hour, 0, 0, 0, loc)
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
			selectedSlot = &s
			found = true
			break
		}
	}

	if !found {
		return "", fmt.Errorf("hour %d not found in schedule", booking.Hour)
	}

	center, err := uc.centerRepo.FindByID(ctx, court.SportCenterID)
	if err != nil {
		return "", fmt.Errorf("sport center not found: %w", err)
	}

	amountToPay := price
	isPartial := false

	if usePartialPayment {
		partialEnabled := false
		if selectedSlot.PartialPaymentEnabled != nil {
			partialEnabled = *selectedSlot.PartialPaymentEnabled
		} else {
			partialEnabled = center.PartialPaymentEnabled
		}

		if partialEnabled {
			percent := center.PartialPaymentPercent
			if percent <= 0 || percent > 100 {
				percent = 50
			}
			amountToPay = (price * float64(percent)) / 100
			isPartial = true
		}
	}

	booking.Price = price
	booking.FinalPrice = price
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

	client := mercadopago.NewClient(center.MercadoPago.AccessToken)

	urlFrontend := os.Getenv("URL_FRONTEND")
	notificationURL := os.Getenv("URL_MP_WEBHOOK")
	urlPaymentCallback := os.Getenv("URL_MP_CALLBACK")

	successURL := fmt.Sprintf("%s?code=%s", urlPaymentCallback, booking.BookingCode)
	failureURL := fmt.Sprintf("%s/booking/failure", urlFrontend)
	pendingURL := fmt.Sprintf("%s?code=%s", urlPaymentCallback, booking.BookingCode)

	title := fmt.Sprintf("Reserva %s - %s", court.Name, center.Name)
	if isPartial {
		title = fmt.Sprintf("Abono Reserva %s - %s", court.Name, center.Name)
	}
	externalRef := booking.BookingCode

	result, err := client.CreatePreference(ctx, title, amountToPay, email, externalRef, successURL, failureURL, pendingURL, notificationURL)
	if err != nil {
		return "", fmt.Errorf("error creating mercadopago preference: %w", err)
	}

	booking.MPPreferenceID = result.ID

	if err := uc.repo.Create(ctx, booking); err != nil {
		return "", err
	}

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

func (uc *BookingUseCase) HandleMercadoPagoWebhook(ctx context.Context, paymentIDStr string) error {
	paymentID, err := strconv.Atoi(paymentIDStr)
	if err != nil {
		return fmt.Errorf("invalid payment ID: %w", err)
	}

	// Buscar la reserva por mp_payment_id (guardado previamente por StoreMPPaymentID o webhook anterior)
	booking, err := uc.repo.FindByMPPaymentID(ctx, paymentIDStr)
	if err != nil {
		return fmt.Errorf("booking not found for mp_payment_id %s: %w", paymentIDStr, err)
	}

	// Obtener el centro deportivo por ID
	center, err := uc.centerRepo.FindByID(ctx, booking.SportCenterID)
	if err != nil {
		return fmt.Errorf("sport center not found for booking %s: %w", booking.BookingCode, err)
	}

	if center.MercadoPago == nil || center.MercadoPago.AccessToken == "" {
		return fmt.Errorf("mercadopago not configured for center %s", center.Name)
	}

	// Verificar el pago con el token del centro
	client := mercadopago.NewClient(center.MercadoPago.AccessToken)
	payment, err := client.GetPayment(ctx, paymentID)
	if err != nil {
		return fmt.Errorf("error getting payment %d from mercadopago: %w", paymentID, err)
	}
	log.Printf("[MP WEBHOOK] Pago %d verificado con token de %s, status: %s\n", paymentID, center.Name, payment.Status)

	paymentStatus := payment.Status

	var newStatus domain.BookingStatus
	switch paymentStatus {
	case "approved":
		newStatus = domain.BookingStatusConfirmed
	case "rejected", "cancelled":
		newStatus = domain.BookingStatusCancelled
	case "pending", "in_process", "authorized":
		log.Printf("[MP WEBHOOK] Payment %d still %s for booking %s\n", paymentID, paymentStatus, booking.BookingCode)
		return nil
	default:
		log.Printf("[MP WEBHOOK] Unknown payment status %s for payment %d\n", paymentStatus, paymentID)
		return nil
	}

	paidAmount := 0.0
	pendingAmount := booking.Price
	if newStatus == domain.BookingStatusConfirmed {
		paidAmount = payment.TransactionAmount
		pendingAmount = booking.Price - paidAmount
		if pendingAmount < 0 {
			pendingAmount = 0
		}
	}

	if err := uc.repo.ConfirmPayment(ctx, booking.ID, newStatus, paidAmount, pendingAmount); err != nil {
		return fmt.Errorf("error updating booking status: %w", err)
	}

	log.Printf("[MP WEBHOOK] Booking %s updated to %s (paid: %.2f, pending: %.2f)\n", booking.BookingCode, newStatus, paidAmount, pendingAmount)

	if newStatus == domain.BookingStatusConfirmed && uc.mailer != nil {
		go func(b *domain.Booking) {
			if err := uc.mailer.SendBookingConfirmation(context.Background(), b); err != nil {
				log.Printf("[MAIL ERROR] sending booking confirmation: %v\n", err)
			}
		}(booking)
	}

	return nil
}

func (uc *BookingUseCase) ValidateMercadoPagoPaymentAndGetCode(ctx context.Context, bookingCode string) (string, error) {
	booking, err := uc.repo.FindByBookingCode(ctx, bookingCode)
	if err != nil {
		return "", fmt.Errorf("booking not found for code: %w", err)
	}

	if booking.Status == domain.BookingStatusConfirmed {
		return booking.BookingCode, nil
	}

	// If still pending but has MP payment, try to check status
	if booking.Status == domain.BookingStatusPending && booking.MPPaymentID != "" {
		center, err := uc.centerRepo.FindByID(ctx, booking.SportCenterID)
		if err != nil {
			return booking.BookingCode, nil
		}

		if center.MercadoPago == nil || center.MercadoPago.AccessToken == "" {
			return booking.BookingCode, nil
		}
		accessToken := center.MercadoPago.AccessToken

		paymentID, err := strconv.Atoi(booking.MPPaymentID)
		if err != nil {
			return booking.BookingCode, nil
		}

		client := mercadopago.NewClient(accessToken)
		payment, err := client.GetPayment(ctx, paymentID)
		if err != nil {
			return booking.BookingCode, nil
		}

		if payment.Status == "approved" {
			booking.Status = domain.BookingStatusConfirmed
			booking.UpdatedAt = time.Now()
			if err := uc.repo.Update(ctx, booking); err != nil {
				return booking.BookingCode, fmt.Errorf("error updating booking: %w", err)
			}
			if uc.mailer != nil {
				go func() {
					if err := uc.mailer.SendBookingConfirmation(context.Background(), booking); err != nil {
						log.Printf("[MAIL ERROR] sending booking confirmation: %v\n", err)
					}
				}()
			}
		}
	}

	return booking.BookingCode, nil
}

func (uc *BookingUseCase) GetByBookingCode(ctx context.Context, code string) (*domain.Booking, error) {
	return uc.repo.FindByBookingCode(ctx, code)
}

func (uc *BookingUseCase) GetByMPPaymentID(ctx context.Context, paymentID string) (*domain.Booking, error) {
	return uc.repo.FindByMPPaymentID(ctx, paymentID)
}

func (uc *BookingUseCase) HandleFintocRefund(ctx context.Context, paymentIntentID string, refundID string, amount int, status string) error {
	refund := domain.Refund{
		ID:        refundID,
		Amount:    amount,
		Status:    status,
		CreatedAt: time.Now(),
	}

	return uc.repo.AddRefund(ctx, paymentIntentID, refund)
}

func (uc *BookingUseCase) GetWebhookSecret(ctx context.Context, id string) (string, error) {
	var booking *domain.Booking
	var err error

	// 1. First try by Checkout Session ID (fintoc_payment_id)
	log.Printf("GetWebhookSecret - trying to find booking by Checkout Session ID: %s\n", id)
	booking, err = uc.repo.FindByFintocPaymentID(ctx, id)
	if err != nil || booking == nil {
		// 2. Then try by Payment Intent ID (fintoc_payment_intent_id)
		log.Printf("GetWebhookSecret - trying to find booking by Payment Intent ID: %s\n", id)
		booking, err = uc.repo.FindByFintocPaymentIntentID(ctx, id)
	}

	log.Printf("Booking =====> %+v\n", booking)
	if err != nil || booking == nil {
		return "", fmt.Errorf("booking not found for webhook validation (ID: %s)", id)
	}

	center, err := uc.centerRepo.FindByID(ctx, booking.SportCenterID)
	if err != nil {
		return "", fmt.Errorf("sport center not found for webhook validation")
	}
	log.Printf("Center =====> %+v\n", center)
	if center.Fintoc == nil || center.Fintoc.Webhook.SecretKey == "" {
		return "", fmt.Errorf("fintoc webhook secret not configured")
	}

	log.Printf("Webhook ======> %s\n", center.Fintoc.Webhook.SecretKey)
	return center.Fintoc.Webhook.SecretKey, nil
}

func (uc *BookingUseCase) GetBookingByFintocID(ctx context.Context, fintocID string) (*domain.Booking, error) {
	booking, err := uc.repo.FindByFintocPaymentID(ctx, fintocID)
	if err != nil || booking == nil {
		booking, err = uc.repo.FindByFintocPaymentIntentID(ctx, fintocID)
	}
	return booking, err
}

func (uc *BookingUseCase) GetSportCenterByID(ctx context.Context, id primitive.ObjectID) (*domain.SportCenter, error) {
	return uc.centerRepo.FindByID(ctx, id)
}

func (uc *BookingUseCase) GetCourtByID(ctx context.Context, id primitive.ObjectID) (*domain.Court, error) {
	return uc.courtRepo.FindByID(ctx, id)
}

func (uc *BookingUseCase) HandleFintocWebhook(ctx context.Context, id string, status string) error {
	// Intentamos buscar por PaymentIntentID primero, luego por CheckoutSessionID (para compatibilidad)
	booking, err := uc.repo.FindByFintocPaymentIntentID(ctx, id)
	if err != nil {
		booking, err = uc.repo.FindByFintocPaymentID(ctx, id)
	}

	if err != nil {
		fmt.Printf("[WEBHOOK ERROR] Reserva no encontrada para Fintoc ID: %s\n", id)
		return err
	}

	newStatus := domain.BookingStatusPending
	switch status {
	case "succeeded":
		newStatus = domain.BookingStatusConfirmed
	case "failed":
		newStatus = domain.BookingStatusCancelled
	}

	err = uc.repo.UpdateStatus(ctx, booking.ID, newStatus)
	if err != nil {
		return err
	}

	// Si se confirmó la reserva, enviar correo de confirmación
	if newStatus == domain.BookingStatusConfirmed && uc.mailer != nil {
		go func(b *domain.Booking) {
			if err := uc.mailer.SendBookingConfirmation(context.Background(), b); err != nil {
				log.Printf("[MAIL ERROR] sending booking confirmation: %v\n", err)
			}
		}(booking)
	}

	return nil
}

func (uc *BookingUseCase) GetUserBookings(ctx context.Context, userID string) ([]domain.BookingSummary, error) {
	bookings, _, err := uc.repo.FindByUserIDPaged(ctx, userID, 1, 100, false)
	return bookings, err
}

func (uc *BookingUseCase) GetUserBookingsPaged(ctx context.Context, userID string, page, limit int, isOld bool) ([]domain.BookingSummary, int64, error) {
	return uc.repo.FindByUserIDPaged(ctx, userID, page, limit, isOld)
}

func (uc *BookingUseCase) GetConfirmedBookingCount(ctx context.Context, userID string) (int64, error) {
	return uc.repo.CountConfirmedByUserID(ctx, userID)
}

func (uc *BookingUseCase) GetByID(ctx context.Context, id primitive.ObjectID) (*domain.Booking, error) {
	return uc.repo.FindByID(ctx, id)
}

func (uc *BookingUseCase) CancelBooking(ctx context.Context, id primitive.ObjectID, userID string) error {
	booking, err := uc.repo.FindByID(ctx, id)
	if err != nil {
		return fmt.Errorf("booking not found: %w", err)
	}

	center, err := uc.centerRepo.FindByID(ctx, booking.SportCenterID)
	if err != nil {
		return fmt.Errorf("sport center not found: %w", err)
	}

	// Verificar si el usuario es administrador del centro
	isAdmin := false
	for _, u := range center.Users {
		if u == userID {
			isAdmin = true
			break
		}
	}

	// Calcular horas restantes (negativo si ya pasó) en horario de Santiago
	loc, _ := time.LoadLocation("America/Santiago")
	bookingDateTime := time.Date(booking.Date.Year(), booking.Date.Month(), booking.Date.Day(), booking.Hour, 0, 0, 0, loc)
	hoursUntilMatch := time.Until(bookingDateTime.In(loc)).Hours()

	if hoursUntilMatch <= 0 {
		// Los admins pueden cancelar hasta 48 horas pasadas
		if isAdmin && hoursUntilMatch >= -48 {
			// Okay
		} else {
			return fmt.Errorf("cannot cancel a past or ongoing booking")
		}
	}

	// Políticas de cancelación
	configCancellationHours := center.CancellationHours
	if configCancellationHours == 0 {
		configCancellationHours = 3
	}
	configRetentionPercent := center.RetentionPercent
	if configRetentionPercent == 0 {
		configRetentionPercent = 10
	}

	refundPercentage := 0
	if hoursUntilMatch >= float64(configCancellationHours) {
		refundPercentage = 100
	} else {
		refundPercentage = 100 - configRetentionPercent
	}

	// Verificar pertenencia del usuario (si la reserva tiene userID y no es admin)
	if !isAdmin && booking.UserID != "" && booking.UserID != userID {
		return fmt.Errorf("unauthorized to cancel this booking")
	}

	if isAdmin {
		refundPercentage = 100
	}

	if booking.Status == domain.BookingStatusCancelled {
		return fmt.Errorf("booking is already cancelled")
	}

	log.Printf("[CANCEL_BOOKING] Iniciando cancelación de reserva %s con %d%% de reembolso\n", id.Hex(), refundPercentage)

	// Procesar reembolso en MercadoPago si aplica

	log.Printf("booking.PaymentMethod: %s, booking.MPPaymentID: %s, refundPercentage: %d\n", booking.PaymentMethod, booking.MPPaymentID, refundPercentage)
	log.Printf("center.MercadoPago.AccessToken: %s", center.MercadoPago.AccessToken)

	if booking.PaymentMethod == "mercadopago" && booking.MPPaymentID != "" && refundPercentage > 0 {
		mpPaymentID, convErr := strconv.Atoi(booking.MPPaymentID)
		if convErr != nil {
			return fmt.Errorf("invalid mp_payment_id '%s': %w", booking.MPPaymentID, convErr)
		}

		if center.MercadoPago == nil || center.MercadoPago.AccessToken == "" {
			return fmt.Errorf("mercadopago not configured for center %s, cannot process refund", center.Name)
		}

		mpClient := mercadopago.NewClient(center.MercadoPago.AccessToken)

		var refundResult *mercadopago.RefundResult
		retentionAmount := (booking.Price * float64(100-refundPercentage)) / 100
		refundAmount := booking.PaidAmount - retentionAmount
		if refundAmount <= 0 {
			log.Printf("[CANCEL_BOOKING] PaidAmount %.2f is less or equal to retention %.2f, no refund processed\n", booking.PaidAmount, retentionAmount)
			refundAmount = 0
		}

		if refundAmount > 0 {
			if refundAmount >= booking.PaidAmount {
				refundResult, err = mpClient.CreateRefund(ctx, mpPaymentID)
			} else {
				refundResult, err = mpClient.CreatePartialRefund(ctx, mpPaymentID, refundAmount)
			}
		}
		if err != nil {
			log.Printf("err: %s", err)
			return fmt.Errorf("error processing mercadopago refund: %w", err)
		}

		if refundResult != nil {
			log.Printf("[CANCEL_BOOKING] Refund MP procesado: ID=%d, Status=%s, Amount=%.2f\n",
				refundResult.ID, refundResult.Status, refundResult.Amount)

			// Registrar el refund en la reserva
			mpRefund := domain.Refund{
				ID:        strconv.Itoa(refundResult.ID),
				Amount:    int(refundResult.Amount),
				Status:    refundResult.Status,
				CreatedAt: time.Now(),
			}
			if addErr := uc.repo.AddRefundByBookingID(ctx, booking.ID, mpRefund); addErr != nil {
				log.Printf("[CANCEL_BOOKING] Error guardando refund en DB: %v\n", addErr)
			}
		}
	}

	cancelledBy := "user"
	if isAdmin {
		cancelledBy = "admin"
	}

	// Actualizar estado a cancelado con detalles
	err = uc.repo.UpdateCancellation(ctx, id, domain.BookingStatusCancelled, cancelledBy, "Cancelación por "+cancelledBy)
	if err != nil {
		return fmt.Errorf("error updating booking status: %w", err)
	}

	// Enviar correo de confirmación de cancelación si está configurado
	if uc.mailer != nil {
		go func(b *domain.Booking) {
			if err := uc.mailer.SendBookingCancellation(context.Background(), b); err != nil {
				log.Printf("[MAIL ERROR] sending cancellation confirmation: %v\n", err)
			}
		}(booking)
	}

	return nil
}
func (uc *BookingUseCase) DeleteBooking(ctx context.Context, id primitive.ObjectID) error {
	return uc.repo.Delete(ctx, id)
}

func (uc *BookingUseCase) CreateInternalBooking(ctx context.Context, booking *domain.Booking, paymentMethod string) error {
	court, err := uc.courtRepo.FindByID(ctx, booking.CourtID)
	if err != nil {
		return fmt.Errorf("court not found: %w", err)
	}

	center, err := uc.centerRepo.FindByID(ctx, court.SportCenterID)
	if err != nil {
		return fmt.Errorf("sport center not found: %w", err)
	}

	// For internal bookings, we don't strict check availability if admin wants to force it,
	// but let's check it for safety or just set it.
	price := 0.0
	minutes := booking.Minutes
	if minutes == 0 {
		minutes = 0
	}
	for _, s := range court.Schedule {
		if s.Hour == booking.Hour && s.Minutes == minutes {
			// Check if slot has already passed
			loc, _ := time.LoadLocation("America/Santiago")
			bookingDateTime := time.Date(booking.Date.Year(), booking.Date.Month(), booking.Date.Day(), booking.Hour, minutes, 0, 0, loc)
			if bookingDateTime.Before(time.Now().In(loc)) {
				return fmt.Errorf("cannot book a past slot")
			}
			price = s.Price
			break
		}
	}

	booking.Price = price
	booking.FinalPrice = price
	booking.Status = domain.BookingStatusConfirmed
	booking.BookingCode = generateBookingCode()
	booking.PaymentMethod = paymentMethod
	booking.SportCenterID = court.SportCenterID
	booking.SportCenterName = center.Name
	booking.CourtName = court.Name
	booking.CreatedAt = time.Now()
	booking.UpdatedAt = time.Now()

	if booking.GuestDetails != nil {
		if booking.CustomerName == "" {
			booking.CustomerName = booking.GuestDetails.Name
		}
		if booking.CustomerPhone == "" {
			booking.CustomerPhone = booking.GuestDetails.Phone
		}
	}

	if err := uc.repo.Create(ctx, booking); err != nil {
		return err
	}

	if uc.mailer != nil {
		go func(b *domain.Booking) {
			if err := uc.mailer.SendBookingConfirmation(context.Background(), b); err != nil {
				log.Printf("[MAIL ERROR] sending booking confirmation: %v\n", err)
			}
		}(booking)
	}

	return nil
}

func (uc *BookingUseCase) Create(ctx context.Context, booking *domain.Booking) error {
	court, err := uc.courtRepo.FindByID(ctx, booking.CourtID)
	if err != nil {
		return fmt.Errorf("court not found: %w", err)
	}

	found := false
	for _, s := range court.Schedule {
		if s.Hour == booking.Hour {
			if s.Status != "available" {
				return fmt.Errorf("hour %d is not available", booking.Hour)
			}
			if s.PaymentRequired {
				return fmt.Errorf("payment required for this slot")
			}
			booking.Price = s.Price
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("hour %d not found in schedule", booking.Hour)
	}

	center, err := uc.centerRepo.FindByID(ctx, court.SportCenterID)
	if err != nil {
		return fmt.Errorf("sport center not found: %w", err)
	}

	booking.FinalPrice = booking.Price
	booking.Status = domain.BookingStatusConfirmed
	booking.BookingCode = generateBookingCode()
	booking.PaymentMethod = "presential"
	booking.SportCenterID = court.SportCenterID
	booking.SportCenterName = center.Name
	booking.CourtName = court.Name
	booking.CreatedAt = time.Now()
	booking.UpdatedAt = time.Now()

	if booking.GuestDetails != nil {
		if booking.CustomerName == "" {
			booking.CustomerName = booking.GuestDetails.Name
		}
		if booking.CustomerPhone == "" {
			booking.CustomerPhone = booking.GuestDetails.Phone
		}
	}

	log.Println("booking para crear: ", booking)
	if err := uc.repo.Create(ctx, booking); err != nil {
		return err
	}

	log.Println("booking para creado: ", booking)
	log.Println("booking uc.mailer: ", uc.mailer)

	if uc.mailer != nil {
		log.Printf("[CREATE BOOKING] Enviando correo de confirmación para reserva %s\n", booking.ID.Hex())
		go func(b *domain.Booking) {
			if err := uc.mailer.SendBookingConfirmation(context.Background(), b); err != nil {
				log.Printf("[MAIL ERROR] sending booking confirmation: %v\n", err)
			}
		}(booking)
	}

	return nil
}

func (uc *BookingUseCase) GetAdminDashboard(ctx context.Context, userID string, page, limit int, dateStr, name, code, status string) (*domain.AdminDashboardData, error) {
	// 1. Get sport centers managed by this user
	centers, err := uc.centerRepo.FindByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}

	if len(centers) == 0 {
		return &domain.AdminDashboardData{
			RecentBookings: []domain.BookingSummary{},
		}, nil
	}

	centerIDs := make([]primitive.ObjectID, len(centers))
	for i, c := range centers {
		centerIDs[i] = c.ID
	}

	// 2. Get dashboard data from repo
	return uc.repo.GetDashboardData(ctx, centerIDs, page, limit, dateStr, name, code, status)
}

func (uc *BookingUseCase) MarkPartialPaymentAsPaid(ctx context.Context, id primitive.ObjectID, userID string) error {
	booking, err := uc.repo.FindByID(ctx, id)
	if err != nil {
		return fmt.Errorf("booking not found: %w", err)
	}

	center, err := uc.centerRepo.FindByID(ctx, booking.SportCenterID)
	if err != nil {
		return fmt.Errorf("sport center not found: %w", err)
	}

	isAdmin := false
	for _, u := range center.Users {
		if u == userID {
			isAdmin = true
			break
		}
	}

	if !isAdmin {
		return fmt.Errorf("unauthorized: only admins can mark balance as paid")
	}

	if !booking.IsPartialPayment {
		return fmt.Errorf("booking is not a partial payment")
	}

	if booking.PartialPaymentPaid {
		return fmt.Errorf("balance already paid")
	}

	return uc.repo.MarkBalanceAsPaid(ctx, id, userID)
}

func (uc *BookingUseCase) UndoBalancePayment(ctx context.Context, id primitive.ObjectID, userID string) error {
	booking, err := uc.repo.FindByID(ctx, id)
	if err != nil {
		return fmt.Errorf("booking not found: %w", err)
	}

	center, err := uc.centerRepo.FindByID(ctx, booking.SportCenterID)
	if err != nil {
		return fmt.Errorf("sport center not found: %w", err)
	}

	isAdmin := false
	for _, u := range center.Users {
		if u == userID {
			isAdmin = true
			break
		}
	}

	if !isAdmin {
		return fmt.Errorf("unauthorized: only admins can undo balance payments")
	}

	if !booking.IsPartialPayment {
		return fmt.Errorf("booking is not a partial payment")
	}

	if !booking.PartialPaymentPaid {
		return fmt.Errorf("balance is not marked as paid")
	}

	return uc.repo.UndoBalancePayment(ctx, id, userID)
}

// ==================== Recurring Reservation Methods ====================

func (uc *BookingUseCase) CreateRecurringReservation(ctx context.Context, reservation *domain.RecurringReservation, date time.Time) error {
	court, err := uc.courtRepo.FindByID(ctx, reservation.CourtID)
	if err != nil {
		return fmt.Errorf("court not found: %w", err)
	}

	// Calcular el día de la semana (0=domingo, 1=lunes, ..., 6=sábado)
	dayOfWeek := int(date.Weekday())
	dayNames := []string{"domingo", "lunes", "martes", "miércoles", "jueves", "viernes", "sábado"}

	reservation.DayOfWeek = dayOfWeek
	reservation.DayOfWeekName = dayNames[dayOfWeek]

	// Verificar si ya existe una reserva recurrente activa para esta cancha, hora y día de la semana
	existing, err := uc.recurringReservationRepo.FindByCourtHourAndDay(ctx, reservation.CourtID, reservation.Hour, dayOfWeek)
	if err == nil && existing != nil {
		return fmt.Errorf("ya existe una reserva recurrente semanal para esta cancha, hora y día")
	}

	// Verificar que el precio no sea 0
	if reservation.Price <= 0 {
		// Obtener precio del schedule
		for _, s := range court.Schedule {
			if s.Hour == reservation.Hour {
				reservation.Price = s.Price
				break
			}
		}
	}

	reservation.SportCenterID = court.SportCenterID
	reservation.Status = domain.RecurringReservationStatusActive
	reservation.CreatedAt = time.Now()
	reservation.UpdatedAt = time.Now()

	if err := uc.recurringReservationRepo.Create(ctx, reservation); err != nil {
		return err
	}

	log.Printf("[RECURRING] Created weekly recurring reservation for court %s at %d:00 every %s - Customer: %s\n",
		court.Name, reservation.Hour, reservation.DayOfWeekName, reservation.CustomerName)

	return nil
}

func (uc *BookingUseCase) GetRecurringReservationByID(ctx context.Context, id primitive.ObjectID) (*domain.RecurringReservationResponse, error) {
	reservation, err := uc.recurringReservationRepo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}

	court, err := uc.courtRepo.FindByID(ctx, reservation.CourtID)
	if err != nil {
		return nil, fmt.Errorf("court not found: %w", err)
	}

	center, err := uc.centerRepo.FindByID(ctx, court.SportCenterID)
	if err != nil {
		return nil, fmt.Errorf("sport center not found: %w", err)
	}

	return &domain.RecurringReservationResponse{
		ID:              reservation.ID,
		SportCenterID:   reservation.SportCenterID,
		SportCenterName: center.Name,
		CourtID:         reservation.CourtID,
		CourtName:       court.Name,
		CustomerName:    reservation.CustomerName,
		CustomerPhone:   reservation.CustomerPhone,
		Hour:            reservation.Hour,
		Minutes:        reservation.Minutes,
		DayOfWeek:       reservation.DayOfWeek,
		DayOfWeekName:   reservation.DayOfWeekName,
		Price:           reservation.Price,
		Notes:           reservation.Notes,
		Status:          reservation.Status,
		CancelledBy:     reservation.CancelledBy,
		CancelReason:    reservation.CancelReason,
		CreatedAt:       reservation.CreatedAt,
		UpdatedAt:       reservation.UpdatedAt,
	}, nil
}

func (uc *BookingUseCase) GetRecurringReservationsByCenter(ctx context.Context, userID string) ([]domain.RecurringReservationResponse, error) {
	centers, err := uc.centerRepo.FindByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}

	if len(centers) == 0 {
		return []domain.RecurringReservationResponse{}, nil
	}

	centerID := centers[0].ID

	reservations, err := uc.recurringReservationRepo.FindByCenterID(ctx, centerID)
	if err != nil {
		return nil, err
	}

	court, err := uc.courtRepo.FindByCenterID(ctx, centerID)
	if err != nil {
		return nil, err
	}
	courtMap := make(map[primitive.ObjectID]string)
	for _, c := range court {
		courtMap[c.ID] = c.Name
	}

	centerName := centers[0].Name

	results := make([]domain.RecurringReservationResponse, 0, len(reservations))
	for _, r := range reservations {
		results = append(results, domain.RecurringReservationResponse{
			ID:              r.ID,
			SportCenterID:   r.SportCenterID,
			SportCenterName: centerName,
			CourtID:         r.CourtID,
			CourtName:       courtMap[r.CourtID],
			CustomerName:    r.CustomerName,
			CustomerPhone:   r.CustomerPhone,
			Hour:            r.Hour,
			Minutes:        r.Minutes,
			DayOfWeek:       r.DayOfWeek,
			DayOfWeekName:   r.DayOfWeekName,
			Price:           r.Price,
			Notes:           r.Notes,
			Status:          r.Status,
			CancelledBy:     r.CancelledBy,
			CancelReason:    r.CancelReason,
			CreatedAt:       r.CreatedAt,
			UpdatedAt:       r.UpdatedAt,
		})
	}

	return results, nil
}

func (uc *BookingUseCase) GetRecurringReservationsByCourt(ctx context.Context, courtID primitive.ObjectID) ([]domain.RecurringReservationResponse, error) {
	reservations, err := uc.recurringReservationRepo.FindByCourtID(ctx, courtID)
	if err != nil {
		return nil, err
	}

	court, err := uc.courtRepo.FindByID(ctx, courtID)
	if err != nil {
		return nil, fmt.Errorf("court not found: %w", err)
	}

	center, err := uc.centerRepo.FindByID(ctx, court.SportCenterID)
	centerName := ""
	if err == nil {
		centerName = center.Name
	}

	dayNames := []string{"domingo", "lunes", "martes", "miércoles", "jueves", "viernes", "sábado"}

	results := make([]domain.RecurringReservationResponse, 0, len(reservations))
	for _, r := range reservations {
		dayOfWeekName := ""
		if r.DayOfWeek >= 0 && r.DayOfWeek <= 6 {
			dayOfWeekName = dayNames[r.DayOfWeek]
		}

		results = append(results, domain.RecurringReservationResponse{
			ID:              r.ID,
			SportCenterID:   r.SportCenterID,
			SportCenterName: centerName,
			CourtID:         r.CourtID,
			CourtName:       court.Name,
			CustomerName:    r.CustomerName,
			CustomerPhone:   r.CustomerPhone,
			Hour:            r.Hour,
			Minutes:        r.Minutes,
			DayOfWeek:       r.DayOfWeek,
			DayOfWeekName:   dayOfWeekName,
			Price:           r.Price,
			Notes:           r.Notes,
			Status:          r.Status,
			CancelledBy:     r.CancelledBy,
			CancelReason:    r.CancelReason,
			CreatedAt:       r.CreatedAt,
			UpdatedAt:       r.UpdatedAt,
		})
	}

	return results, nil
}

func (uc *BookingUseCase) CancelRecurringReservation(ctx context.Context, id primitive.ObjectID, cancelledBy string, reason string) error {
	reservation, err := uc.recurringReservationRepo.FindByID(ctx, id)
	if err != nil {
		return fmt.Errorf("recurring reservation not found: %w", err)
	}

	if reservation.Status == domain.RecurringReservationStatusCancelled {
		return fmt.Errorf("recurring reservation is already cancelled")
	}

	return uc.recurringReservationRepo.Cancel(ctx, id, cancelledBy, reason)
}

func (uc *BookingUseCase) IsSlotAvailableForRecurring(ctx context.Context, courtID primitive.ObjectID, hour int) (bool, error) {
	// Check if there's already an active recurring reservation
	existing, err := uc.recurringReservationRepo.FindActiveByCourtAndHour(ctx, courtID, hour)
	if err == nil && existing != nil {
		return false, nil
	}

	// Check for confirmed bookings today (or any day since it's weekly recurring)
	// For recurring reservations, we don't check specific date bookings
	// because they would override this anyway
	return true, nil
}
