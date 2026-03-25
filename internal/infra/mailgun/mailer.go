package mailgun

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	mg "github.com/mailgun/mailgun-go/v4"
)

type MailgunMailer struct {
	mg       mg.Mailgun
	from     string
	template string
}

func NewMailgunMailer(apiKey, domain, from, templateName string) *MailgunMailer {
	mgClient := mg.NewMailgun(domain, apiKey)
	return &MailgunMailer{mg: mgClient, from: from, template: templateName}
}

func (m *MailgunMailer) SendBookingConfirmation(ctx context.Context, booking *domain.Booking) error {
	var to string
	if booking.GuestDetails != nil && booking.GuestDetails.Email != "" {
		to = booking.GuestDetails.Email
	}
	if to == "" {
		// No hay destinatario
		return fmt.Errorf("no recipient email for booking %s", booking.BookingCode)
	}

	subject := fmt.Sprintf("Reserva confirmada - %s", booking.SportCenterName)

	message := m.mg.NewMessage(m.from, subject, "", to)
	if m.template != "" {
		message.SetTemplate(m.template)
		// Variables de plantilla
		// Construir URL pública de cancelación usando la URL del frontend
		frontendURL := os.Getenv("URL_FRONTEND")
		if frontendURL == "" {
			frontendURL = "http://localhost:5173"
		}

		cancelURL := fmt.Sprintf("%s/booking/cancel?code=%s", frontendURL, booking.BookingCode)

		vars := map[string]interface{}{
			"booking_code":  booking.BookingCode,
			"center_name":   booking.SportCenterName,
			"court_name":    booking.CourtName,
			"date":          booking.Date.Format(time.RFC3339),
			"hour":          booking.Hour,
			"price":         booking.FinalPrice,
			"customer_name": booking.CustomerName,
			"link_cancel":   cancelURL,
		}
		if b, err := json.Marshal(vars); err == nil {
			message.AddHeader("X-Mailgun-Variables", string(b))
		} else {
			log.Printf("[MAILGUN] error marshaling template variables: %v\n", err)
		}
	} else {
		// fallback simple body
		body := fmt.Sprintf("Tu reserva %s en %s (cancha %s) para %s a las %02d:00 ha sido confirmada.", booking.BookingCode, booking.SportCenterName, booking.CourtName, booking.Date.Format("2006-01-02"), booking.Hour)
		message.SetHtml(body)
	}

	// Send with timeout
	sendCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	_, id, err := m.mg.Send(sendCtx, message)
	if err != nil {
		return fmt.Errorf("mailgun send error: %w", err)
	}
	log.Printf("[MAILGUN] sent message id=%s to=%s", id, to)
	return nil
}
