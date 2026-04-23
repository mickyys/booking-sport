package infra

import (
	"context"
	"fmt"
	"log"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/messaging"
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

func (s *FirebaseNotificationService) SendPushNotification(ctx context.Context, tokens []string, title, body string, data map[string]string) error {
	if len(tokens) == 0 {
		return nil
	}

	// FCM permits up to 500 tokens in a single multicast message
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
			},
		},
		APNS: &messaging.APNSConfig{
			Payload: &messaging.APNSPayload{
				Aps: &messaging.Aps{
					Sound: "default",
				},
			},
		},
		Webpush: &messaging.WebpushConfig{
			Notification: &messaging.WebpushNotification{
				Title: title,
				Body:  body,
				Icon:  "/logo/favicon-32x32.png",
			},
		},
	}

	log.Printf("[FCM] Sending multicast message to %d tokens. Title: %s", len(tokens), title)
	response, err := s.client.SendEachForMulticast(ctx, message)
	if err != nil {
		return fmt.Errorf("error sending multicast message: %v", err)
	}

	log.Printf("[FCM] response: %+v", response)

	if response.FailureCount > 0 {
		log.Printf("Failed to send %d push notifications out of %d", response.FailureCount, len(tokens))
		for idx, resp := range response.Responses {
			if !resp.Success {
				log.Printf("Token %s failed: %v", tokens[idx], resp.Error)
			}
		}
	}

	return nil
}
