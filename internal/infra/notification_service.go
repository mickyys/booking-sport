package infra

import (
	"context"
	"fmt"
	"strings"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/messaging"
	"github.com/hamp/booking-sport/internal/app"
	"github.com/hamp/booking-sport/internal/domain"
	"github.com/hamp/booking-sport/pkg/logger"
	"google.golang.org/api/option"
)

type FirebaseNotificationService struct {
	client *messaging.Client
}

func NewFirebaseNotificationService(ctx context.Context, credentialsFile string) (*FirebaseNotificationService, error) {
	var fbApp *firebase.App
	var err error

	if credentialsFile != "" {
		opt := option.WithCredentialsFile(credentialsFile)
		fbApp, err = firebase.NewApp(ctx, nil, opt)
	} else {
		// Try with default credentials (env GOOGLE_APPLICATION_CREDENTIALS)
		fbApp, err = firebase.NewApp(ctx, nil)
	}

	if err != nil {
		return nil, fmt.Errorf("error initializing firebase app: %v", err)
	}

	client, err := fbApp.Messaging(ctx)
	if err != nil {
		return nil, fmt.Errorf("error getting messaging client: %v", err)
	}

	return &FirebaseNotificationService{
		client: client,
	}, nil
}

// Canales de Android. Deben existir en la app que recibe el aviso: los crea
// `flutter_local_notifications` al arrancar cada app.
const (
	channelAdminBookings  = "reservation_channel_v2"
	channelAdminDefault   = "default_channel"
	channelClientBookings = "reservaloya_bookings"
)

// SendPushNotification envía el aviso y devuelve los tokens que FCM declaró
// inválidos (app desinstalada o token rotado) para que el llamador los pode.
func (s *FirebaseNotificationService) SendPushNotification(ctx context.Context, msg app.NotificationMessage) (app.PushResult, error) {
	result := app.PushResult{}
	if len(msg.Tokens) == 0 {
		return result, nil
	}

	log := logger.FromContext(ctx)

	data := make(map[string]string, len(msg.Data)+2)
	for k, v := range msg.Data {
		data[k] = v
	}
	data["notification_type"] = msg.Type
	data["type"] = msg.Type
	data["audience"] = msg.Audience
	data["title"] = msg.Title
	data["body"] = msg.Body

	centerName := data["center_name"]
	if centerName == "" {
		centerName = "unknown"
	}

	channelID := androidChannelFor(msg.Type, msg.Audience)
	data["channel_id"] = channelID

	log.Infow("push_notification_sending",
		"msg", fmt.Sprintf("Enviando notificación push tipo %s a %d dispositivos (%s) del centro %s",
			msg.Type, len(msg.Tokens), msg.Audience, centerName),
		"notification_type", msg.Type,
		"audience", msg.Audience,
		"center_name", centerName,
		"channel", channelID,
		"tokens_count", len(msg.Tokens),
		"title", msg.Title,
	)

	// La app de clientes va sin bloque `notification` en Android: es un mensaje
	// de datos y la app arma la notificación, porque sólo así puede mostrar el
	// logo a color (Firebase no permite fijar el ícono grande desde el servidor
	// y el ícono de la barra de estado es siempre monocromo). El panel conserva
	// el aviso que dibuja el SDK.
	isClient := msg.Audience == domain.DeviceAppClient

	message := &messaging.MulticastMessage{
		Tokens:  msg.Tokens,
		Data:    data,
		Android: &messaging.AndroidConfig{Priority: "high"},
	}

	if isClient {
		message.APNS = &messaging.APNSConfig{
			Payload: &messaging.APNSPayload{
				Aps: &messaging.Aps{
					Alert: &messaging.ApsAlert{
						Title: msg.Title,
						Body:  msg.Body,
					},
					// El sonido propio vive en el canal de Android; iOS usa el
					// del sistema para no depender de un recurso en el bundle.
					Sound:          "default",
					MutableContent: true,
					ThreadID:       "bookings",
				},
			},
		}
	} else {
		message.Notification = &messaging.Notification{
			Title: msg.Title,
			Body:  msg.Body,
		}
		message.Android.Notification = &messaging.AndroidNotification{
			ClickAction: "FLUTTER_NOTIFICATION_CLICK",
			Icon:        "notification_icon",
			Color:       "#2C3345",
			ChannelID:   channelID,
		}
		message.APNS = &messaging.APNSConfig{
			Payload: &messaging.APNSPayload{
				Aps: &messaging.Aps{
					Sound:          "default",
					MutableContent: true,
					ThreadID:       "bookings",
				},
			},
		}
	}

	response, err := s.client.SendEachForMulticast(ctx, message)
	if err != nil {
		log.Errorw("push_notification_failed",
			"msg", fmt.Sprintf("Error al enviar notificación push tipo %s a %s", msg.Type, centerName),
			"notification_type", msg.Type,
			"audience", msg.Audience,
			"center_name", centerName,
			"tokens_count", len(msg.Tokens),
			"error", err,
		)
		return result, fmt.Errorf("error sending multicast message: %v", err)
	}

	result.SuccessCount = response.SuccessCount
	result.FailureCount = response.FailureCount

	for idx, resp := range response.Responses {
		if resp.Success {
			continue
		}
		log.Warnw("push_notification_token_failed",
			"msg", fmt.Sprintf("Token FCM #%d falló en envío de notificación", idx),
			"notification_type", msg.Type,
			"audience", msg.Audience,
			"error", resp.Error,
		)
		if idx < len(msg.Tokens) && (messaging.IsUnregistered(resp.Error) || messaging.IsInvalidArgument(resp.Error)) {
			result.InvalidTokens = append(result.InvalidTokens, msg.Tokens[idx])
		}
	}

	if response.FailureCount > 0 {
		log.Warnw("push_notification_partial_failure",
			"msg", fmt.Sprintf("Notificación push enviada parcialmente: %d éxito, %d fallos para %s",
				response.SuccessCount, response.FailureCount, centerName),
			"notification_type", msg.Type,
			"audience", msg.Audience,
			"center_name", centerName,
			"tokens_count", len(msg.Tokens),
			"success_count", response.SuccessCount,
			"failure_count", response.FailureCount,
		)
	} else {
		log.Infow("push_notification_success",
			"msg", fmt.Sprintf("Notificación push tipo %s enviada a %d dispositivos de %s",
				msg.Type, response.SuccessCount, centerName),
			"notification_type", msg.Type,
			"audience", msg.Audience,
			"center_name", centerName,
			"tokens_count", len(msg.Tokens),
			"success_count", response.SuccessCount,
		)
	}

	return result, nil
}

// androidChannelFor elige el canal de Android según la app que recibe: el panel
// mantiene su canal histórico y la app de clientes usa uno propio. Ambos los
// crea `flutter_local_notifications` al arrancar.
func androidChannelFor(notificationType, audience string) (channelID string) {
	if audience == domain.DeviceAppClient {
		return channelClientBookings
	}
	if notificationType == "confirmation" || strings.HasPrefix(notificationType, "booking_") {
		return channelAdminBookings
	}
	return channelAdminDefault
}
