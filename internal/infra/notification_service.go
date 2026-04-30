package infra

import (
	"context"
	"fmt"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/messaging"
	"github.com/hamp/booking-sport/pkg/logger"
	"google.golang.org/api/option"
)

type FirebaseNotificationService struct {
	client *messaging.Client
}

func NewFirebaseNotificationService(ctx context.Context, credentialsFile string) (*FirebaseNotificationService, error) {
	var app *firebase.App
	var err error

	if credentialsFile != "" {
		opt := option.WithCredentialsFile(credentialsFile)
		app, err = firebase.NewApp(ctx, nil, opt)
	} else {
		// Try with default credentials (env GOOGLE_APPLICATION_CREDENTIALS)
		app, err = firebase.NewApp(ctx, nil)
	}

	if err != nil {
		return nil, fmt.Errorf("error initializing firebase app: %v", err)
	}

	client, err := app.Messaging(ctx)
	if err != nil {
		return nil, fmt.Errorf("error getting messaging client: %v", err)
	}

	return &FirebaseNotificationService{
		client: client,
	}, nil
}

func (s *FirebaseNotificationService) SendPushNotification(ctx context.Context, tokens []string, title, body string, data map[string]string, notificationType string) error {
	if len(tokens) == 0 {
		return nil
	}

	log := logger.FromContext(ctx)

	if data == nil {
		data = make(map[string]string)
	}
	data["notification_type"] = notificationType

	centerName := data["center_name"]
	if centerName == "" {
		centerName = "unknown"
	}

	log.Infow("push_notification_sending",
		"notification_type", notificationType,
		"center_name", centerName,
		"tokens_count", len(tokens),
		"title", title,
	)

	message := &messaging.MulticastMessage{
		Tokens: tokens,
		Notification: &messaging.Notification{
			Title: title,
			Body:  body,
		},
		Data: data,
		Android: &messaging.AndroidConfig{
			Priority: "high",
			Notification: &messaging.AndroidNotification{
				ClickAction: "FLUTTER_NOTIFICATION_CLICK",
				Icon:        "notification_icon",
				Color:       "#2C3345",
			},
		},
		APNS: &messaging.APNSConfig{
			Payload: &messaging.APNSPayload{
				Aps: &messaging.Aps{
					Sound:          "default",
					MutableContent: true,
				},
			},
		},
	}

	response, err := s.client.SendEachForMulticast(ctx, message)
	if err != nil {
		log.Errorw("push_notification_failed",
			"notification_type", notificationType,
			"center_name", centerName,
			"tokens_count", len(tokens),
			"error", err,
		)
		return fmt.Errorf("error sending multicast message: %v", err)
	}

	if response.FailureCount > 0 {
		log.Warnw("push_notification_partial_failure",
			"notification_type", notificationType,
			"center_name", centerName,
			"tokens_count", len(tokens),
			"success_count", response.SuccessCount,
			"failure_count", response.FailureCount,
		)
		for idx, resp := range response.Responses {
			if !resp.Success {
				log.Warnw("push_notification_token_failed",
					"token_index", idx,
					"error", resp.Error,
				)
			}
		}
	} else {
		log.Infow("push_notification_success",
			"notification_type", notificationType,
			"center_name", centerName,
			"tokens_count", len(tokens),
			"success_count", response.SuccessCount,
		)
	}

	return nil
}
