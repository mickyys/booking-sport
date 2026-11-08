package app

import (
	"context"
	"fmt"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"github.com/hamp/booking-sport/pkg/logger"
)

// PushEvent es un aviso de reserva que se manda por FCM.
type PushEvent struct {
	Type  string // booking_confirmed, booking_cancelled, booking_reminder, ...
	Title string
	Body  string
}

// PushService decide a qué dispositivos va cada aviso, evita duplicados y arma
// el payload. Es best-effort: nunca interrumpe la operación de negocio que lo
// dispara y no devuelve errores.
//
// Separar el envío del caso de uso permite probar la selección de tokens y el
// dedupe sin Firebase.
type PushService struct {
	devices    UserDeviceRepository
	centers    SportCenterRepository
	sender     NotificationService
	deliveries NotificationDeliveryRepository
}

func NewPushService(
	devices UserDeviceRepository,
	centers SportCenterRepository,
	sender NotificationService,
	deliveries NotificationDeliveryRepository,
) *PushService {
	return &PushService{devices: devices, centers: centers, sender: sender, deliveries: deliveries}
}

// NotifyAdmins avisa a los dispositivos del panel del centro de la reserva:
// los registrados con el centro y los de cada administrador del centro.
func (s *PushService) NotifyAdmins(ctx context.Context, booking *domain.Booking, ev PushEvent) {
	if s == nil || booking == nil {
		return
	}
	log := logger.FromContext(ctx)
	centerID := booking.SportCenterID.Hex()

	tokens := make([]string, 0, 4)
	seen := map[string]bool{}
	add := func(devices []domain.UserDevice) {
		for _, d := range devices {
			if d.FCMToken == "" || seen[d.FCMToken] {
				continue
			}
			seen[d.FCMToken] = true
			tokens = append(tokens, d.FCMToken)
		}
	}

	centerDevices, err := s.devices.FindBySportCenterIDAndApp(ctx, centerID, domain.DeviceAppAdmin)
	if err != nil {
		log.Warnw("push_admin_devices_error", "center_id", centerID, "error", err)
	}
	add(centerDevices)

	if s.centers != nil {
		if center, err := s.centers.FindByID(ctx, booking.SportCenterID); err == nil && center != nil {
			for _, adminID := range center.Users {
				adminDevices, err := s.devices.FindByUserIDAndApp(ctx, adminID, domain.DeviceAppAdmin)
				if err != nil {
					log.Warnw("push_admin_user_devices_error", "user_id", adminID, "error", err)
					continue
				}
				add(adminDevices)
			}
		}
	}

	s.dispatch(ctx, booking, ev, domain.DeviceAppAdmin, tokens)
}

// NotifyCustomer avisa al cliente de la reserva. Si la reserva es de un
// usuario autenticado se buscan sus dispositivos; si es de invitado, los
// registrados con el `guest_device_id` que reservó.
func (s *PushService) NotifyCustomer(ctx context.Context, booking *domain.Booking, ev PushEvent) {
	if s == nil || booking == nil {
		return
	}
	log := logger.FromContext(ctx)

	var devices []domain.UserDevice
	var err error
	switch {
	case booking.UserID != "":
		devices, err = s.devices.FindByUserIDAndApp(ctx, booking.UserID, domain.DeviceAppClient)
	case booking.GuestDeviceID != "":
		devices, err = s.devices.FindByGuestDeviceID(ctx, booking.GuestDeviceID)
	}
	if err != nil {
		log.Warnw("push_customer_devices_error", "booking_id", booking.ID.Hex(), "error", err)
		return
	}

	tokens := make([]string, 0, len(devices))
	seen := map[string]bool{}
	for _, d := range devices {
		if d.FCMToken == "" || seen[d.FCMToken] {
			continue
		}
		seen[d.FCMToken] = true
		tokens = append(tokens, d.FCMToken)
	}

	s.dispatch(ctx, booking, ev, domain.DeviceAppClient, tokens)
}

func (s *PushService) dispatch(ctx context.Context, booking *domain.Booking, ev PushEvent, audience string, tokens []string) {
	log := logger.FromContext(ctx)

	if s.sender == nil {
		log.Debugw("push_skipped", "reason", "notifier_not_configured", "booking_id", booking.ID.Hex())
		return
	}
	if len(tokens) == 0 {
		log.Infow("push_no_tokens", "audience", audience, "type", ev.Type, "booking_id", booking.ID.Hex())
		return
	}
	if s.deliveries != nil {
		claimed, err := s.deliveries.Claim(ctx, audience+":"+notificationDedupeKey(booking, ev.Type), "push")
		if err != nil {
			log.Warnw("push_dedupe_error", "booking_id", booking.ID.Hex(), "error", err)
		} else if !claimed {
			log.Debugw("push_already_sent", "audience", audience, "type", ev.Type, "booking_id", booking.ID.Hex())
			return
		}
	}

	msg := NotificationMessage{
		Tokens:   tokens,
		Title:    ev.Title,
		Body:     ev.Body,
		Data:     pushData(booking, ev.Type, audience),
		Type:     ev.Type,
		Audience: audience,
	}

	// El envío no debe bloquear la respuesta HTTP ni el job de recordatorios.
	go func() {
		result, err := s.sender.SendPushNotification(context.Background(), msg)
		if err != nil {
			log.Errorw("push_send_failed", "audience", audience, "type", ev.Type,
				"booking_id", booking.ID.Hex(), "tokens_count", len(tokens), "error", err)
			return
		}
		log.Infow("push_sent", "audience", audience, "type", ev.Type,
			"booking_id", booking.ID.Hex(), "tokens_count", len(tokens),
			"success_count", result.SuccessCount, "failure_count", result.FailureCount)

		if len(result.InvalidTokens) > 0 {
			if err := s.devices.DeleteByTokens(context.Background(), result.InvalidTokens); err != nil {
				log.Warnw("push_prune_failed", "error", err, "tokens_count", len(result.InvalidTokens))
			}
		}
	}()
}

// notificationDedupeKey identifica un aviso por tipo y reserva. Para los
// eventos atados al horario (recordatorio y reagendamiento) incluye el inicio:
// así un cambio de hora genera un aviso nuevo sin duplicar las corridas
// sucesivas del job. Confirmaciones y cancelaciones conservan la clave estable
// por reserva para no repetirse si el webhook o el reconciliador reintentan.
func notificationDedupeKey(b *domain.Booking, msgType string) string {
	base := msgType + ":" + b.ID.Hex()
	switch msgType {
	case domain.InboxBookingReminder, domain.InboxBookingRescheduled:
		return base + ":" + b.StartInstant().UTC().Format(time.RFC3339)
	default:
		return base
	}
}

// pushData es el payload que consumen las apps. No incluye datos personales
// del cliente: se muestran en pantalla bloqueada y la app los pide a la API.
func pushData(b *domain.Booking, notificationType, audience string) map[string]string {
	loc := domain.GetSantiagoLocation()
	paymentStatus := "not_required"
	if b.PaymentMethod == "mercadopago" {
		if b.PendingAmount <= 0 {
			paymentStatus = "paid"
		} else {
			paymentStatus = "pending"
		}
	}

	return map[string]string{
		"type":              notificationType,
		"notification_type": notificationType,
		"audience":          audience,
		"booking_id":        b.ID.Hex(),
		"booking_code":      b.BookingCode,
		"sport_center_id":   b.SportCenterID.Hex(),
		"center_name":       b.SportCenterName,
		"court_name":        b.CourtName,
		"date":              b.Date.In(loc).Format("2006-01-02"),
		"time":              fmt.Sprintf("%02d:%02d", b.Hour, b.Minutes),
		"status":            string(b.Status),
		"payment_method":    b.PaymentMethod,
		"payment_status":    paymentStatus,
		"price":             fmt.Sprintf("%.0f", b.ChargedPrice()),
		"click_action":      "FLUTTER_NOTIFICATION_CLICK",
	}
}
