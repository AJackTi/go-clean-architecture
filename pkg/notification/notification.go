package notification

import (
	"context"
	"fmt"

	"github.com/AJackTi/go-clean-architecture/pkg/logger"

	"google.golang.org/api/option"

	firebase "firebase.google.com/go"

	"firebase.google.com/go/messaging"
)

type Notification struct {
	app       *firebase.App
	FCMClient *messaging.Client
}

func New() (*Notification, error) {
	opts := []option.ClientOption{option.WithCredentialsFile("./resource/creds.json")}

	app, err := firebase.NewApp(context.Background(), nil, opts...)
	if err != nil {
		return nil, err
	}

	fcmClient, err := app.Messaging(context.Background())
	if err != nil {
		return nil, err
	}

	return &Notification{
		app:       app,
		FCMClient: fcmClient,
	}, nil
}

func (n *Notification) SendNotification(registrationTokens []string, data map[string]string, notification *messaging.Notification, locKey string, locArgs []string) (int, int, error) {
	message := &messaging.MulticastMessage{
		Tokens:       registrationTokens,
		Data:         data,
		Notification: notification,
		Android: &messaging.AndroidConfig{
			Data: data,
			Notification: &messaging.AndroidNotification{
				BodyLocKey:  locKey,
				BodyLocArgs: locArgs,
			},
		},
		APNS: &messaging.APNSConfig{
			Headers: nil,
			Payload: &messaging.APNSPayload{
				Aps: &messaging.Aps{
					Alert: &messaging.ApsAlert{
						LocKey:  locKey,
						LocArgs: locArgs,
					},
				},
			},
		},
	}
	br, err := n.FCMClient.SendMulticast(context.Background(), message)
	if err != nil {
		logger.Error("Firebase send multi devices", logger.ErrWrap(err))
		return 0, 0, err
	}

	logger.Info(fmt.Sprintf("%d messages were sent successfully\n", br.SuccessCount))
	logger.Error(fmt.Sprintf("%d messages were sent fail\n", br.FailureCount))
	return br.SuccessCount, br.FailureCount, nil
}
