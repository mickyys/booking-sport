package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"github.com/hamp/booking-sport/pkg/fintoc"
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
	repo       BookingRepository
	courtRepo  CourtRepository
	centerRepo SportCenterRepository
	userRepo   UserRepository
}

func NewBookingUseCase(repo BookingRepository, courtRepo CourtRepository, centerRepo SportCenterRepository, userRepo UserRepository) *BookingUseCase {
	return &BookingUseCase{
		repo:       repo,
		courtRepo:  courtRepo,
		centerRepo: centerRepo,
		userRepo:   userRepo,
	}
}

// GetUserCancelledBookingsPaged retorna solo las reservas canceladas del usuario
func (uc *BookingUseCase) GetUserCancelledBookingsPaged(ctx context.Context, userID string, page, limit int) ([]domain.BookingSummary, int64, error) {
	return uc.repo.FindByUserIDAndStatusPaged(ctx, userID, domain.BookingStatusCancelled, page, limit)
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
			if s.Status != "available" {
				return "", fmt.Errorf("hour %d is not available", booking.Hour)
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
				_ = uc.repo.Update(ctx, booking)
				return booking.BookingCode, nil
			}
		}
	}

	// Para cualquier otro caso (failed, cancelled o si sigue pendiente en Fintoc)
	return booking.BookingCode, nil
}

func (uc *BookingUseCase) GetByBookingCode(ctx context.Context, code string) (*domain.Booking, error) {
	return uc.repo.FindByBookingCode(ctx, code)
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

	// Obtener información de la cancha y el centro para calcular políticas
	court, err := uc.courtRepo.FindByID(ctx, booking.CourtID)
	if err != nil {
		return fmt.Errorf("court not found: %w", err)
	}

	center, err := uc.centerRepo.FindByID(ctx, court.SportCenterID)
	if err != nil {
		return fmt.Errorf("sport center not found: %w", err)
	}

	// Calcular horas restantes
	bookingDateTime := time.Date(booking.Date.Year(), booking.Date.Month(), booking.Date.Day(), booking.Hour, 0, 0, 0, booking.Date.Location())

	if time.Until(bookingDateTime) <= 0 {
		return fmt.Errorf("cannot cancel a past or ongoing booking")
	}

	hoursUntilMatch := time.Until(bookingDateTime).Hours()

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

	// Verificar pertenencia del usuario (si la reserva tiene userID)
	if booking.UserID != "" && booking.UserID != userID {
		return fmt.Errorf("unauthorized to cancel this booking")
	}

	if booking.Status == domain.BookingStatusCancelled {
		return fmt.Errorf("booking is already cancelled")
	}

	log.Printf("[CANCEL_BOOKING] Iniciando cancelación de reserva %s con %d%% de reembolso\n", id.Hex(), refundPercentage)

	// TODO: Aquí se podría llamar a la API de Fintoc para procesar el reembolso
	// usando refundPercentage y booking.FintocPaymentIntentID

	// Actualizar estado a cancelado
	err = uc.repo.UpdateStatus(ctx, id, domain.BookingStatusCancelled)
	if err != nil {
		return fmt.Errorf("error updating booking status: %w", err)
	}

	return nil
}
