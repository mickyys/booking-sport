package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"github.com/hamp/booking-sport/pkg/logger"
	"github.com/hamp/booking-sport/pkg/mercadopago"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
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
	holdRepo                 SlotHoldRepository
	courtRepo                CourtRepository
	centerRepo               SportCenterRepository
	userRepo                 UserRepository
	deviceRepo               UserDeviceRepository
	mailer                   Mailer
	notifier                 NotificationService
	recurringReservationRepo RecurringReservationRepository
	mongoClient              *mongo.Client
	mpClient                 *mercadopago.Client
	scheduleResolver         *ScheduleResolver
}

// SetScheduleResolver inyecta el resolver de disponibilidad basado en reglas
// para resolver precios y validar intervalos al crear reservas.
func (uc *BookingUseCase) SetScheduleResolver(resolver *ScheduleResolver) {
	uc.scheduleResolver = resolver
}

// NewBookingUseCase crea el use case de bookings. mpClient es el cliente de la
// plataforma (token MP_ACCESS_TOKEN), usado únicamente para verificar pagos en
// el webhook. Las preferencias y reembolsos se crean con el token de cada
// vendedor (center.MercadoPago.AccessToken) en tiempo de ejecución.
func NewBookingUseCase(repo BookingRepository, holdRepo SlotHoldRepository, courtRepo CourtRepository, centerRepo SportCenterRepository, userRepo UserRepository, deviceRepo UserDeviceRepository, mailer Mailer, notifier NotificationService, recurringRepo RecurringReservationRepository, mongoClient *mongo.Client, mpClient *mercadopago.Client) *BookingUseCase {
	return &BookingUseCase{
		repo:                     repo,
		holdRepo:                 holdRepo,
		courtRepo:                courtRepo,
		centerRepo:               centerRepo,
		userRepo:                 userRepo,
		deviceRepo:               deviceRepo,
		mailer:                   mailer,
		notifier:                 notifier,
		recurringReservationRepo: recurringRepo,
		mongoClient:              mongoClient,
		mpClient:                 mpClient,
	}
}

func (uc *BookingUseCase) RegisterDevice(ctx context.Context, userID, token, platform, deviceName, osVersion string, userRoles []string) error {
	// 1. Validar por el claim de roles de Auth0
	isAdminRole := false
	for _, role := range userRoles {
		lowerRole := strings.ToLower(role)
		if lowerRole == "admin" || lowerRole == "administrator" {
			isAdminRole = true
			break
		}
	}

	// 2. Buscar centros administrados por el usuario
	centers, err := uc.centerRepo.FindByUserID(ctx, userID)
	if err != nil {
		return fmt.Errorf("error verifying admin status: %w", err)
	}

	// Si no tiene el rol en el token ni administra centros, rechazamos
	if !isAdminRole && len(centers) == 0 {
		return fmt.Errorf("unauthorized: only administrators can register devices")
	}

	sportCenterID := ""
	if len(centers) > 0 {
		sportCenterID = centers[0].ID.Hex()
	}

	device := &domain.UserDevice{
		UserID:        userID,
		SportCenterID: sportCenterID,
		FCMToken:      token,
		Platform:      platform,
		DeviceName:    deviceName,
		OSVersion:     osVersion,
	}
	return uc.deviceRepo.Upsert(ctx, device)
}

func (uc *BookingUseCase) notifyAdmins(ctx context.Context, booking *domain.Booking, title, body string, notificationType string) {
	loc, _ := time.LoadLocation("America/Santiago")

	if uc.notifier == nil {
		logger.FromContext(ctx).Warnw("push_notifier_not_configured",
			"msg", "Servicio de notificaciones push no está configurado, no se enviarán notificaciones",
		)
		return
	}

	sportCenterName := booking.SportCenterName
	sportCenterID := booking.SportCenterID

	log := logger.FromContext(ctx)

	log.Infow("push_notify_admins_started",
		"msg", fmt.Sprintf("Iniciando envío de notificación push a administradores del centro %s", sportCenterName),
		"center_id", sportCenterID.Hex(),
		"center_name", sportCenterName,
		"notification_type", notificationType,
		"booking_id", booking.ID.Hex(),
	)

	devices, err := uc.deviceRepo.FindBySportCenterID(ctx, sportCenterID.Hex())
	if err != nil {
		log.Errorw("push_notify_admins_find_devices_error",
			"msg", fmt.Sprintf("Error al buscar dispositivos del centro %s", sportCenterName),
			"center_id", sportCenterID.Hex(),
			"error", err,
		)
	}

	log.Infow("push_notify_admins_devices_found",
		"msg", fmt.Sprintf("Se encontraron %d dispositivos registrados para el centro %s", len(devices), sportCenterName),
		"center_id", sportCenterID.Hex(),
		"center_name", sportCenterName,
		"devices_count", len(devices),
	)

	tokenMap := make(map[string]bool)
	var allTokens []string

	for _, d := range devices {
		if !tokenMap[d.FCMToken] {
			tokenMap[d.FCMToken] = true
			allTokens = append(allTokens, d.FCMToken)
		}
	}

	var centerSlug string
	center, err := uc.centerRepo.FindByID(ctx, sportCenterID)
	if err == nil {
		centerSlug = center.Slug
		log.Infow("push_notify_admins_center_users",
			"msg", fmt.Sprintf("El centro %s tiene %d usuarios administradores", center.Name, len(center.Users)),
			"center_id", sportCenterID.Hex(),
			"center_name", center.Name,
			"users_count", len(center.Users),
		)
		for _, adminID := range center.Users {
			adminDevices, err := uc.deviceRepo.FindByUserID(ctx, adminID)
			if err != nil {
				log.Warnw("push_notify_admins_user_devices_error",
					"msg", fmt.Sprintf("Error al buscar dispositivos del usuario administrador %s", adminID),
					"user_id", adminID,
					"error", err,
				)
				continue
			}
			log.Infow("push_notify_admins_user_devices_found",
				"msg", fmt.Sprintf("Se encontraron %d dispositivos para el usuario administrador %s", len(adminDevices), adminID),
				"user_id", adminID,
				"devices_count", len(adminDevices),
			)
			for _, d := range adminDevices {
				if !tokenMap[d.FCMToken] {
					tokenMap[d.FCMToken] = true
					allTokens = append(allTokens, d.FCMToken)
				}
			}
		}
	} else {
		log.Warnw("push_notify_admins_center_not_found",
			"msg", fmt.Sprintf("No se encontró el centro deportivo %s para buscar usuarios administradores", sportCenterName),
			"center_id", sportCenterID.Hex(),
			"error", err,
		)
	}

	if len(allTokens) == 0 {
		log.Warnw("push_notify_admins_no_tokens",
			"msg", fmt.Sprintf("No se encontraron tokens FCM para notificar al centro %s", sportCenterName),
			"center_id", sportCenterID.Hex(),
			"center_name", sportCenterName,
		)
		return
	}

	// Determinar estado de pago
	paymentStatus := "not_required"
	if booking.PaymentMethod == "mercadopago" {
		if booking.PendingAmount <= 0 {
			paymentStatus = "paid"
		} else {
			paymentStatus = "pending"
		}
	}

	customerEmail := ""
	if booking.GuestDetails != nil {
		customerEmail = booking.GuestDetails.Email
	}

	data := map[string]string{
		"booking_id":        booking.ID.Hex(),
		"customer_name":     booking.CustomerName,
		"customer_phone":    booking.CustomerPhone,
		"customer_email":    customerEmail,
		"booking_code":      booking.BookingCode,
		"court_name":        booking.CourtName,
		"center_name":       booking.SportCenterName,
		"date":              booking.Date.Format("2006-01-02"),
		"hour":              fmt.Sprintf("%d", booking.Hour),
		"time":              fmt.Sprintf("%02d:%02d", booking.Hour, booking.Minutes),
		"status":            string(booking.Status),
		"payment_method":    booking.PaymentMethod,
		"payment_status":    paymentStatus,
		"price":             fmt.Sprintf("%.0f", booking.Price),
		"created_at":        booking.CreatedAt.In(loc).Format(time.RFC3339),
		"sport_center_slug": centerSlug,
		"center_id":         sportCenterID.Hex(),
		"notification_type": notificationType,
		"click_action":      "FLUTTER_NOTIFICATION_CLICK",
	}

	log.Infow("push_notify_admins_sending",
		"msg", fmt.Sprintf("Enviando notificación push a %d dispositivos del centro %s", len(allTokens), sportCenterName),
		"center_id", sportCenterID.Hex(),
		"center_name", sportCenterName,
		"notification_type", notificationType,
		"tokens_count", len(allTokens),
	)

	go func() {
		if err := uc.notifier.SendPushNotification(context.Background(), allTokens, title, body, data, notificationType); err != nil {
			log.Errorw("push_notify_admins_failed",
				"msg", fmt.Sprintf("Falló el envío de notificación push al centro %s", sportCenterName),
				"center_id", sportCenterID.Hex(),
				"center_name", sportCenterName,
				"notification_type", notificationType,
				"error", err,
			)
		} else {
			log.Infow("push_notify_admins_success",
				"msg", fmt.Sprintf("Notificación push enviada exitosamente a %d dispositivos del centro %s", len(allTokens), sportCenterName),
				"center_id", sportCenterID.Hex(),
				"center_name", sportCenterName,
				"notification_type", notificationType,
				"tokens_count", len(allTokens),
			)
		}
	}()
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

func generateDeviceID() string {
	b := make([]byte, 12)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func resolveEffectiveUserID(booking *domain.Booking) string {
	if booking.UserID != "" {
		return booking.UserID
	}
	if booking.GuestDeviceID != "" {
		return booking.GuestDeviceID
	}
	return "device:" + generateDeviceID()
}

func (uc *BookingUseCase) ClaimOrRenewSlot(ctx context.Context, courtID primitive.ObjectID, date time.Time, hour int, userID string) (*domain.SlotHold, *domain.Booking, error) {
	loc, _ := time.LoadLocation("America/Santiago")
	dateCL := date.In(loc)
	normalizedDate := time.Date(dateCL.Year(), dateCL.Month(), dateCL.Day(), 0, 0, 0, 0, loc)

	now := time.Now().In(loc)
	expiresAt := now.Add(15 * time.Minute)

	log := logger.FromContext(ctx)

	confirmed, _ := uc.repo.FindConfirmedBySlot(ctx, courtID, normalizedDate, hour)
	if confirmed != nil {
		log.Infow("claim_renew_confirmed_found",
			"court_id", courtID.Hex(),
			"date", normalizedDate.Format("02/01/2006"),
			"hour", hour,
		)
		return nil, nil, domain.NewConflictError("este horario ya esta reservado")
	}

	pending, _ := uc.repo.FindPendingBySlot(ctx, courtID, normalizedDate, hour)
	if pending != nil {
		log.Infow("claim_renew_pending_found",
			"booking_id", pending.ID.Hex(),
			"booking_code", pending.BookingCode,
			"status", pending.Status,
			"lock_expires_at", pending.LockExpiresAt,
			"hold_id", pending.HoldID,
			"guest_device_id", pending.GuestDeviceID,
			"user_id", pending.UserID,
			"current_user", userID,
		)
		if pending.LockExpiresAt != nil && now.Before(*pending.LockExpiresAt) {
			log.Infow("claim_renew_lock_active",
				"booking_code", pending.BookingCode,
				"lock_expires_at", pending.LockExpiresAt.Format(time.RFC3339),
				"now", now.Format(time.RFC3339),
			)
			if userID != "" && (pending.GuestDeviceID == userID || pending.UserID == userID) {
				log.Infow("claim_renew_same_user_renew",
					"booking_code", pending.BookingCode,
					"user", userID,
				)
				uc.repo.UpdateLockExpiresAt(ctx, pending.ID, expiresAt)
				hold, _ := uc.holdRepo.FindByBookingID(ctx, pending.ID)
				if hold != nil {
					uc.holdRepo.RenewExpiration(ctx, hold.ID, expiresAt)
				} else {
					newHold := &domain.SlotHold{
						CourtID:   courtID,
						Date:      normalizedDate,
						Hour:      hour,
						UserID:    userID,
						BookingID: pending.ID,
						ExpiresAt: expiresAt,
						CreatedAt: now,
					}
					hold, _ = uc.holdRepo.TryClaimSlot(ctx, newHold)
				}
				return hold, pending, nil
			}
			log.Infow("claim_renew_different_user_blocked",
				"booking_code", pending.BookingCode,
				"pending_user", pending.UserID,
				"requesting_user", userID,
			)
			return nil, nil, domain.NewConflictError("otro usuario esta en proceso de pago, intentalo en unos minutos por si no confirma la reserva")
		}
		log.Infow("claim_renew_lock_expired_marking",
			"booking_code", pending.BookingCode,
			"lock_expires_at", pending.LockExpiresAt,
			"hold_id", pending.HoldID,
		)
		if pending.HoldID != nil {
			uc.holdRepo.Delete(ctx, *pending.HoldID)
		}
		uc.repo.MarkExpired(ctx, pending.ID)
	}

	newHold := &domain.SlotHold{
		CourtID:   courtID,
		Date:      normalizedDate,
		Hour:      hour,
		UserID:    userID,
		ExpiresAt: expiresAt,
		CreatedAt: now,
	}

	claimedHold, err := uc.holdRepo.TryClaimSlot(ctx, newHold)
	if err == nil {
		log.Infow("claim_render_hold_created",
			"hold_id", claimedHold.ID.Hex(),
			"expires_at", claimedHold.ExpiresAt.Format(time.RFC3339),
		)
		return claimedHold, nil, nil
	}

	log.Infow("claim_renew_try_claim_failed",
		"error", err.Error(),
	)

	if err.Error() != "duplicate hold" && !strings.Contains(err.Error(), "duplicate key") && !strings.Contains(err.Error(), "E11000") {
		return nil, nil, err
	}

	existingHold, err := uc.holdRepo.FindBySlot(ctx, courtID, normalizedDate, hour)
	if err != nil || existingHold == nil {
		log.Infow("claim_renew_hold_not_found",
			"error", err,
		)
		return nil, nil, fmt.Errorf("error interno al verificar disponibilidad")
	}

	log.Infow("claim_renew_duplicate_hold_found",
		"hold_id", existingHold.ID.Hex(),
		"user_id", existingHold.UserID,
		"current_user", userID,
		"expires_at", existingHold.ExpiresAt.Format(time.RFC3339),
		"now", now.Format(time.RFC3339),
		"is_expired", now.After(existingHold.ExpiresAt),
	)

	if userID != "" && existingHold.UserID == userID {
		log.Infow("claim_renew_same_user_hold_renew",
			"hold_id", existingHold.ID.Hex(),
		)
		uc.holdRepo.RenewExpiration(ctx, existingHold.ID, expiresAt)
		booking, _ := uc.repo.FindByID(ctx, existingHold.BookingID)
		if booking != nil && booking.Status == domain.BookingStatusPending {
			log.Infow("claim_renew_updating_booking_lock",
				"booking_id", booking.ID.Hex(),
				"booking_status", booking.Status,
			)
			uc.repo.UpdateLockExpiresAt(ctx, booking.ID, expiresAt)
		} else {
			log.Infow("claim_renew_booking_not_found_or_not_pending",
				"booking_id", existingHold.BookingID.Hex(),
				"booking_found", booking != nil,
			)
		}
		return existingHold, booking, nil
	}

	if now.After(existingHold.ExpiresAt) {
		log.Infow("claim_renew_hold_expired_deleting",
			"hold_id", existingHold.ID.Hex(),
		)
		deletedHold, _ := uc.holdRepo.FindOneAndDeleteIfExpired(ctx, courtID, normalizedDate, hour)
		if deletedHold != nil {
			log.Infow("claim_renew_hold_deleted",
				"hold_id", deletedHold.ID.Hex(),
				"booking_id", deletedHold.BookingID.Hex(),
			)
			if !deletedHold.BookingID.IsZero() {
				uc.repo.MarkExpired(ctx, deletedHold.BookingID)
			}
			return uc.ClaimOrRenewSlot(ctx, courtID, date, hour, userID)
		}
		log.Infow("claim_renew_find_and_delete_returned_nil",
			"hold_id", existingHold.ID.Hex(),
		)
		return nil, nil, domain.NewConflictError("otro usuario esta en proceso de pago, intentalo en unos minutos por si no confirma la reserva")
	}

	log.Infow("claim_renew_hold_still_active",
		"hold_id", existingHold.ID.Hex(),
	)
	return nil, nil, domain.NewConflictError("otro usuario esta en proceso de pago, intentalo en unos minutos por si no confirma la reserva")
}

// ==================== MercadoPago Payment Methods ====================

func (uc *BookingUseCase) GetByBookingCode(ctx context.Context, code string) (*domain.Booking, error) {
	booking, err := uc.repo.FindByBookingCode(ctx, code)
	if err != nil {
		return nil, err
	}

	if booking.Status == domain.BookingStatusPending && booking.PaymentMethod == "mercadopago" {
		uc.SyncPendingBookingPaymentStatus(ctx, booking)
		booking, _ = uc.repo.FindByBookingCode(ctx, code)
	}

	return booking, nil
}

func (uc *BookingUseCase) GetByMPPaymentID(ctx context.Context, paymentID string) (*domain.Booking, error) {
	return uc.repo.FindByMPPaymentID(ctx, paymentID)
}

func (uc *BookingUseCase) GetSportCenterByID(ctx context.Context, id primitive.ObjectID) (*domain.SportCenter, error) {
	return uc.centerRepo.FindByID(ctx, id)
}

func (uc *BookingUseCase) GetCourtByID(ctx context.Context, id primitive.ObjectID) (*domain.Court, error) {
	return uc.courtRepo.FindByID(ctx, id)
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

	log := logger.FromContext(ctx)

	if isAdmin {
		refundPercentage = 100
	}

	if booking.Status == domain.BookingStatusCancelled {
		return fmt.Errorf("booking is already cancelled")
	}

	log.Infow("booking_cancel_started",
		"msg", fmt.Sprintf("Iniciando cancelación del booking %s en %s — reembolso %d%%",
			booking.BookingCode, booking.SportCenterName, refundPercentage),
		"booking_id", id.Hex(),
		"booking_code", booking.BookingCode,
		"refund_percentage", refundPercentage,
		"user_id", userID,
		"is_admin", isAdmin,
	)

	// Procesar reembolso en MercadoPago si aplica

	log.Infow("booking_cancel_payment_info",
		"msg", fmt.Sprintf("Información de pago para cancelación — booking %s, método %s, reembolso %d%%",
			booking.BookingCode, booking.PaymentMethod, refundPercentage),
		"booking_id", id.Hex(),
		"payment_method", booking.PaymentMethod,
		"mp_payment_id", booking.MPPaymentID,
		"refund_percentage", refundPercentage,
	)

	// Procesar reembolso en MercadoPago si aplica
	if err := uc.processMercadoPagoRefund(ctx, booking, center, refundPercentage); err != nil {
		return err
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

	if booking.HoldID != nil {
		uc.holdRepo.Delete(ctx, *booking.HoldID)
	}

	log.Infow("booking_cancelled",
		"msg", fmt.Sprintf("Booking %s cancelado en %s — cancelado por %s",
			booking.BookingCode, booking.SportCenterName, cancelledBy),
		"booking_id", booking.ID.Hex(),
		"booking_code", booking.BookingCode,
		"center_id", booking.SportCenterID.Hex(),
		"center_name", booking.SportCenterName,
		"cancelled_by", cancelledBy,
	)

	title := "Reserva Cancelada"
	body := fmt.Sprintf("La reserva en %s para el %s a las %02d:00 hrs fue cancelada.",
		booking.SportCenterName, booking.Date.Format("02/01/2006"), booking.Hour)

	log.Infow("booking_cancel_sending_notification",
		"msg", fmt.Sprintf("Enviando notificación push de cancelación para booking %s en centro %s",
			booking.BookingCode, booking.SportCenterName),
		"booking_id", booking.ID.Hex(),
		"center_id", booking.SportCenterID.Hex(),
	)
	uc.notifyAdmins(ctx, booking, title, body, "cancellation")

	// Enviar correo de confirmación de cancelación si está configurado
	if uc.mailer != nil {
		go func(b *domain.Booking) {
			if err := uc.mailer.SendBookingCancellation(context.Background(), b); err != nil {
				logger.FromContext(context.Background()).Errorw("mail_booking_cancellation_error",
					"msg", fmt.Sprintf("Error al enviar correo de cancelación para booking %s", b.BookingCode),
					"booking_code", b.BookingCode,
					"error", err,
				)
			}
		}(booking)
	}

	return nil
}
func (uc *BookingUseCase) DeleteBooking(ctx context.Context, id primitive.ObjectID) error {
	return uc.repo.Delete(ctx, id)
}

// resolveBookingPrice determina el precio de una reserva. Si el espacio tiene
// reglas (schedule_rules), resuelve la franja que cubre el intervalo
// [hora, hora+duración]; si no, usa el horario legacy de la cancha.
func (uc *BookingUseCase) resolveBookingPrice(ctx context.Context, court *domain.Court, booking *domain.Booking, minutes int) error {
	if booking.DurationMinutes <= 0 {
		booking.DurationMinutes = 60
	}

	if uc.scheduleResolver != nil {
		franja, hasRules, err := uc.scheduleResolver.FindFranjaForBooking(ctx, court.ID, booking.Date, booking.Hour, minutes, booking.DurationMinutes)
		if err != nil {
			return err
		}
		if hasRules {
			if franja == nil {
				return fmt.Errorf("el horario %02d:%02d (%d min) no está dentro de la configuración de la cancha", booking.Hour, minutes, booking.DurationMinutes)
			}
			booking.Price = franja.Price
			return nil
		}
	}

	for _, s := range court.Schedule {
		if s.Hour == booking.Hour && s.Minutes == minutes {
			booking.Price = s.Price
			return nil
		}
	}
	return fmt.Errorf("hour %02d:%02d not found in schedule", booking.Hour, minutes)
}

// checkConfirmedIntervalConflict valida que el intervalo de la reserva no se
// superponga con reservas confirmadas existentes del mismo espacio y fecha.
func (uc *BookingUseCase) checkConfirmedIntervalConflict(ctx context.Context, booking *domain.Booking, minutes int) error {
	duration := booking.DurationMinutes
	if duration <= 0 {
		duration = 60
	}
	start := booking.Hour*60 + minutes
	end := start + duration

	confirmed, err := uc.repo.FindConfirmedByCourtAndDate(ctx, booking.CourtID, booking.Date)
	if err != nil {
		return nil
	}
	for _, b := range confirmed {
		if b.ID == booking.ID {
			continue
		}
		bDur := b.DurationMinutes
		if bDur <= 0 {
			bDur = 60
		}
		bStart := b.Hour*60 + b.Minutes
		if start < bStart+bDur && bStart < end {
			return domain.NewConflictError("ya existe una reserva confirmada que se superpone con este horario")
		}
	}
	return nil
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

	loc, _ := time.LoadLocation("America/Santiago")
	booking.Date = time.Date(booking.Date.In(loc).Year(), booking.Date.In(loc).Month(), booking.Date.In(loc).Day(), 0, 0, 0, 0, loc)

	// For internal bookings, we don't strict check availability if admin wants to force it,
	// but let's check it for safety or just set it.
	minutes := booking.Minutes

	if err := uc.resolveBookingPrice(ctx, court, booking, minutes); err != nil {
		return err
	}

	// Check if slot has already passed
	bookingDateTime := time.Date(booking.Date.Year(), booking.Date.Month(), booking.Date.Day(), booking.Hour, minutes, 0, 0, loc)
	if bookingDateTime.Before(time.Now().In(loc)) {
		return fmt.Errorf("cannot book a past slot")
	}

	booking.FinalPrice = booking.Price
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

	conflicting, err := uc.repo.FindConfirmedBySlot(ctx, booking.CourtID, booking.Date, booking.Hour)
	if err != nil {
		return fmt.Errorf("error checking availability: %w", err)
	}
	if conflicting != nil {
		return fmt.Errorf("ya existe un proceso de reserva o reserva confirmada para este horario")
	}

	if err := uc.checkConfirmedIntervalConflict(ctx, booking, minutes); err != nil {
		return err
	}

	if uc.recurringReservationRepo != nil {
		dayOfWeek := int(booking.Date.Weekday())
		existingRecurring, _ := uc.recurringReservationRepo.FindByCourtHourAndDay(ctx, booking.CourtID, booking.Hour, dayOfWeek)
		if existingRecurring != nil && !existingRecurring.IsDateCancelled(booking.Date) {
			return domain.NewConflictError("no disponible: existe una reserva recurrente semanal para este horario")
		}
	}

	activeHold, _ := uc.holdRepo.FindBySlot(ctx, booking.CourtID, booking.Date, booking.Hour)
	if activeHold != nil && time.Now().Before(activeHold.ExpiresAt) {
		return domain.NewConflictError("otro usuario esta en proceso de pago, intentalo en unos minutos por si no confirma la reserva")
	}

	pending, _ := uc.repo.FindPendingBySlot(ctx, booking.CourtID, booking.Date, booking.Hour)
	if pending != nil {
		if pending.LockExpiresAt == nil || time.Now().After(*pending.LockExpiresAt) {
			if pending.HoldID != nil {
				uc.holdRepo.Delete(ctx, *pending.HoldID)
			}
			uc.repo.MarkExpired(ctx, pending.ID)
		} else {
			return domain.NewConflictError("otro usuario esta en proceso de pago, intentalo en unos minutos por si no confirma la reserva")
		}
	}

	if err := uc.repo.Create(ctx, booking); err != nil {
		return err
	}

	log := logger.FromContext(ctx)
	log.Infow("internal_booking_created",
		"msg", fmt.Sprintf("Booking interno creado: %s en %s (%s) para el %s a las %02d:00 hrs — código %s",
			booking.CustomerName, booking.SportCenterName, booking.CourtName,
			booking.Date.Format("02/01/2006"), booking.Hour, booking.BookingCode),
		"booking_id", booking.ID.Hex(),
		"booking_code", booking.BookingCode,
		"center_id", booking.SportCenterID.Hex(),
		"center_name", booking.SportCenterName,
	)

	title := "Nueva Reserva"
	body := fmt.Sprintf("Nueva reserva en %s para el %s a las %02d:00 hrs.",
		booking.SportCenterName, booking.Date.Format("02/01/2006"), booking.Hour)

	log.Infow("internal_booking_sending_notification",
		"msg", fmt.Sprintf("Enviando notificación push para booking interno %s en %s",
			booking.BookingCode, booking.SportCenterName),
		"booking_id", booking.ID.Hex(),
		"center_id", booking.SportCenterID.Hex(),
	)
	uc.notifyAdmins(ctx, booking, title, body, "confirmation")

	if uc.mailer != nil {
		cancellationHours := center.CancellationHours
		if cancellationHours == 0 {
			cancellationHours = 3
		}
		retentionPercent := center.RetentionPercent
		if retentionPercent == 0 {
			retentionPercent = 10
		}
		paidAmount := booking.PaidAmount
		pendingAmount := booking.FinalPrice - booking.PaidAmount
		go func(b *domain.Booking) {
			if err := uc.mailer.SendBookingConfirmation(context.Background(), b, cancellationHours, retentionPercent, paidAmount, pendingAmount); err != nil {
				logger.FromContext(context.Background()).Errorw("mail_booking_confirmation_error",
					"msg", fmt.Sprintf("Error al enviar correo de confirmación para booking %s", b.BookingCode),
					"booking_code", b.BookingCode,
					"error", err,
				)
			}
		}(booking)
	}

	return nil
}

func (uc *BookingUseCase) CreateInternalBookingsBatch(ctx context.Context, bookings []domain.Booking, seriesID string) ([]domain.Booking, error) {
	if len(bookings) == 0 {
		return nil, fmt.Errorf("no bookings provided")
	}

	firstBooking := bookings[0]
	court, err := uc.courtRepo.FindByID(ctx, firstBooking.CourtID)
	if err != nil {
		return nil, fmt.Errorf("court not found: %w", err)
	}

	center, err := uc.centerRepo.FindByID(ctx, court.SportCenterID)
	if err != nil {
		return nil, fmt.Errorf("sport center not found: %w", err)
	}

	loc, _ := time.LoadLocation("America/Santiago")

	type validatedBooking struct {
		booking domain.Booking
		price   float64
	}

	validated := make([]validatedBooking, 0, len(bookings))

	for _, b := range bookings {
		b.Date = time.Date(b.Date.In(loc).Year(), b.Date.In(loc).Month(), b.Date.In(loc).Day(), 0, 0, 0, 0, loc)

		if err := uc.resolveBookingPrice(ctx, court, &b, b.Minutes); err != nil {
			return nil, fmt.Errorf("No disponible: %s - %v", b.Date.Format("02/01/2006"), err)
		}

		minutes := b.Minutes
		bookingDateTime := time.Date(b.Date.Year(), b.Date.Month(), b.Date.Day(), b.Hour, minutes, 0, 0, loc)
		if bookingDateTime.Before(time.Now().In(loc)) {
			return nil, fmt.Errorf("No disponible: %s - el horario ya pasó", b.Date.Format("02/01/2006"))
		}

		price := b.Price

		dateStr := b.Date.Format("02/01/2006")

		conflicting, err := uc.repo.FindConfirmedBySlot(ctx, b.CourtID, b.Date, b.Hour)
		if err != nil {
			return nil, fmt.Errorf("error checking availability: %w", err)
		}
		if conflicting != nil {
			return nil, fmt.Errorf("No disponible: %s - ya existe una reserva confirmada para este horario", dateStr)
		}

		if err := uc.checkConfirmedIntervalConflict(ctx, &b, b.Minutes); err != nil {
			return nil, fmt.Errorf("No disponible: %s - %v", dateStr, err)
		}

		if uc.recurringReservationRepo != nil {
			dayOfWeek := int(b.Date.Weekday())
			existingRecurring, _ := uc.recurringReservationRepo.FindByCourtHourAndDay(ctx, b.CourtID, b.Hour, dayOfWeek)
			if existingRecurring != nil && !existingRecurring.IsDateCancelled(b.Date) {
				return nil, fmt.Errorf("No disponible: %s - existe una reserva recurrente semanal para este horario", dateStr)
			}
		}

		activeHold, _ := uc.holdRepo.FindBySlot(ctx, b.CourtID, b.Date, b.Hour)
		if activeHold != nil && time.Now().Before(activeHold.ExpiresAt) {
			return nil, fmt.Errorf("No disponible: %s - otro usuario está en proceso de pago", dateStr)
		}

		pending, _ := uc.repo.FindPendingBySlot(ctx, b.CourtID, b.Date, b.Hour)
		if pending != nil {
			if pending.LockExpiresAt == nil || time.Now().After(*pending.LockExpiresAt) {
				if pending.HoldID != nil {
					uc.holdRepo.Delete(ctx, *pending.HoldID)
				}
				uc.repo.MarkExpired(ctx, pending.ID)
			} else {
				return nil, fmt.Errorf("No disponible: %s - otro usuario está en proceso de pago", dateStr)
			}
		}

		b.SportCenterID = court.SportCenterID
		b.SportCenterName = center.Name
		b.CourtName = court.Name
		b.Price = price
		b.FinalPrice = price
		b.Status = domain.BookingStatusConfirmed
		b.BookingCode = generateBookingCode()
		b.PaymentMethod = "internal"
		b.SeriesID = seriesID
		b.CreatedAt = time.Now()
		b.UpdatedAt = time.Now()

		if b.GuestDetails != nil {
			if b.CustomerName == "" {
				b.CustomerName = b.GuestDetails.Name
			}
			if b.CustomerPhone == "" {
				b.CustomerPhone = b.GuestDetails.Phone
			}
		}

		validated = append(validated, validatedBooking{booking: b, price: price})
	}

	created := make([]domain.Booking, 0, len(validated))
	for _, vb := range validated {
		b := vb.booking
		if err := uc.repo.Create(ctx, &b); err != nil {
			return nil, fmt.Errorf("error creating booking for %s: %w", b.Date.Format("02/01/2006"), err)
		}
		created = append(created, b)
	}

	log := logger.FromContext(ctx)
	log.Infow("batch_bookings_created",
		"msg", fmt.Sprintf("Batch de %d bookings internos creados para %s en %s (%s) — serie %s",
			len(created), firstBooking.CustomerName, firstBooking.SportCenterName, firstBooking.CourtName, seriesID),
		"count", len(created),
		"series_id", seriesID,
		"center_id", court.SportCenterID.Hex(),
	)

	return created, nil
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

	log := logger.FromContext(ctx)
	log.Infow("booking_create_started",
		"msg", fmt.Sprintf("Se crea booking para las %02d:00 hrs en %s (%s)",
			booking.Hour, booking.SportCenterName, booking.CourtName),
		"center_id", booking.SportCenterID.Hex(),
		"center_name", booking.SportCenterName,
	)

	conflicting, err := uc.repo.FindConfirmedBySlot(ctx, booking.CourtID, booking.Date, booking.Hour)
	if err != nil {
		return fmt.Errorf("error checking availability: %w", err)
	}
	if conflicting != nil {
		return domain.NewConflictError("ya existe una reserva confirmada para este horario")
	}

	activeHold, _ := uc.holdRepo.FindBySlot(ctx, booking.CourtID, booking.Date, booking.Hour)
	if activeHold != nil && time.Now().Before(activeHold.ExpiresAt) {
		return domain.NewConflictError("otro usuario esta en proceso de pago, intentalo en unos minutos por si no confirma la reserva")
	}

	pendingBooking, _ := uc.repo.FindPendingBySlot(ctx, booking.CourtID, booking.Date, booking.Hour)
	if pendingBooking != nil {
		if pendingBooking.LockExpiresAt == nil || time.Now().After(*pendingBooking.LockExpiresAt) {
			if pendingBooking.HoldID != nil {
				uc.holdRepo.Delete(ctx, *pendingBooking.HoldID)
			}
			uc.repo.MarkExpired(ctx, pendingBooking.ID)
		} else {
			return domain.NewConflictError("otro usuario esta en proceso de pago, intentalo en unos minutos por si no confirma la reserva")
		}
	}

	if err := uc.repo.Create(ctx, booking); err != nil {
		return err
	}

	log.Infow("booking_created",
		"msg", fmt.Sprintf("Booking creado exitosamente: %s en %s (%s) para el %s a las %02d:00 hrs — código %s",
			booking.CustomerName, booking.SportCenterName, booking.CourtName,
			booking.Date.Format("02/01/2006"), booking.Hour, booking.BookingCode),
		"booking_id", booking.ID.Hex(),
		"booking_code", booking.BookingCode,
		"center_id", booking.SportCenterID.Hex(),
		"center_name", booking.SportCenterName,
	)

	title := "Nueva Reserva Confirmada"
	body := fmt.Sprintf("Nueva reserva en %s para el %s a las %02d:00 hrs.",
		booking.SportCenterName, booking.Date.Format("02/01/2006"), booking.Hour)

	log.Infow("booking_sending_notification",
		"msg", fmt.Sprintf("Enviando notificación push para booking %s en %s",
			booking.BookingCode, booking.SportCenterName),
		"booking_id", booking.ID.Hex(),
		"center_id", booking.SportCenterID.Hex(),
	)
	uc.notifyAdmins(ctx, booking, title, body, "confirmation")

	if uc.mailer != nil {
		log.Infow("booking_sending_confirmation_email",
			"msg", fmt.Sprintf("Enviando correo de confirmación para booking %s a %s",
				booking.BookingCode, booking.CustomerName),
			"booking_id", booking.ID.Hex(),
			"booking_code", booking.BookingCode,
		)
		cancellationHours := center.CancellationHours
		if cancellationHours == 0 {
			cancellationHours = 3
		}
		retentionPercent := center.RetentionPercent
		if retentionPercent == 0 {
			retentionPercent = 10
		}
		paidAmount := booking.PaidAmount
		pendingAmount := booking.FinalPrice - booking.PaidAmount
		go func(b *domain.Booking) {
			if err := uc.mailer.SendBookingConfirmation(context.Background(), b, cancellationHours, retentionPercent, paidAmount, pendingAmount); err != nil {
				logger.FromContext(context.Background()).Errorw("mail_booking_confirmation_error",
					"msg", fmt.Sprintf("Error al enviar correo de confirmación para booking %s", b.BookingCode),
					"booking_code", b.BookingCode,
					"error", err,
				)
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
	log := logger.FromContext(ctx)

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

	// Verificar si ya existen reservas individuales activas en serie para esta cancha, hora y día de la semana
	if uc.repo != nil {
		seriesBookings, err := uc.repo.FindActiveSeriesByCourtHour(ctx, reservation.CourtID, reservation.Hour)
		if err != nil {
			log.Warnw("find_active_series_error", "error", err)
		} else {
			log.Infow("active_series_check", "court_id", reservation.CourtID.Hex(), "hour", reservation.Hour, "day_of_week", dayOfWeek, "count", len(seriesBookings))
			for _, b := range seriesBookings {
				bDay := int(b.Date.Weekday())
				log.Infow("active_series_booking", "booking_id", b.ID.Hex(), "series_id", b.SeriesID, "date", b.Date.Format("2006-01-02"), "weekday", bDay)
				if bDay == dayOfWeek {
					return fmt.Errorf("ya existe una reserva recurrente semanal para esta cancha, hora y día")
				}
			}
		}
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

	log.Infow("recurring_reservation_created",
		"msg", fmt.Sprintf("Reserva recurrente creada: %s en cancha %s, todos los %s a las %02d:00 hrs — cliente %s",
			reservation.CustomerName, court.Name, reservation.DayOfWeekName, reservation.Hour, reservation.CustomerName),
		"court_id", reservation.CourtID.Hex(),
		"court_name", court.Name,
		"hour", reservation.Hour,
		"day_of_week", reservation.DayOfWeekName,
		"customer_name", reservation.CustomerName,
	)

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
		Minutes:         reservation.Minutes,
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
			Minutes:         r.Minutes,
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
			Minutes:         r.Minutes,
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

func (uc *BookingUseCase) CancelRecurringDate(ctx context.Context, id primitive.ObjectID, date string) error {
	reservation, err := uc.recurringReservationRepo.FindByID(ctx, id)
	if err != nil {
		return fmt.Errorf("recurring reservation not found: %w", err)
	}

	if reservation.Status == domain.RecurringReservationStatusCancelled {
		return fmt.Errorf("recurring reservation is already cancelled")
	}

	loc, err := time.LoadLocation("America/Santiago")
	if err != nil {
		return fmt.Errorf("failed to load timezone: %w", err)
	}

	parsedDate, err := time.ParseInLocation("2006-01-02", date, loc)
	if err != nil {
		return fmt.Errorf("invalid date format, expected YYYY-MM-DD")
	}

	if int(parsedDate.Weekday()) != reservation.DayOfWeek {
		return fmt.Errorf("la fecha no corresponde al dia semanal de la recurrencia (%s)", reservation.DayOfWeekName)
	}

	today := time.Now().In(loc)
	todayStart := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, loc)
	if parsedDate.Before(todayStart) {
		return fmt.Errorf("no se puede anular una fecha pasada")
	}

	return uc.recurringReservationRepo.AddCancelledDate(ctx, id, date)
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
