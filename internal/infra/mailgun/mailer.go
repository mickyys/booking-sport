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
	mg              mg.Mailgun
	from            string
	templateConfirm string
	templateCancel  string
}

func NewMailgunMailer(apiKey, domain, from, templateConfirm, templateCancel string) *MailgunMailer {
	mgClient := mg.NewMailgun(domain, apiKey)
	// Forzar el nombre "ReservaloYA" en el campo From
	fromName := "ReservaloYA"
	fromAddress := from
	if from == "" {
		fromAddress = "reservas@reservaloya.cl"
	}
	fullFrom := fmt.Sprintf("%s <%s>", fromName, fromAddress)
	return &MailgunMailer{mg: mgClient, from: fullFrom, templateConfirm: templateConfirm, templateCancel: templateCancel}
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

	// Formatear hora usando únicamente `booking.Hour` (sin minutos).
	timeStr := fmt.Sprintf("%02d:00", booking.Hour)
	// Añadir sufijo " hrs" tal como se solicita (ej. "16:00 hrs").
	timeWithSuffix := fmt.Sprintf("%s hrs", timeStr)

	subject := fmt.Sprintf("Reserva confirmada - %s", booking.SportCenterName)

	message := m.mg.NewMessage(m.from, subject, "", to)
	if m.templateConfirm != "" {
		message.SetTemplate(m.templateConfirm)
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
			"date":          booking.Date.Format("02-01-2006"),
			"hour":          timeWithSuffix,
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
		// Usar booking.Date para formatear minutos si existen
		body := fmt.Sprintf("Tu reserva %s en %s (cancha %s) para %s a las %s ha sido confirmada.", booking.BookingCode, booking.SportCenterName, booking.CourtName, booking.Date.Format("2006-01-02"), timeWithSuffix)
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

func (m *MailgunMailer) SendBookingCancellation(ctx context.Context, booking *domain.Booking) error {
	var to string
	if booking.GuestDetails != nil && booking.GuestDetails.Email != "" {
		to = booking.GuestDetails.Email
	}
	if to == "" {
		return fmt.Errorf("no recipient email for booking %s", booking.BookingCode)
	}

	timeStr := fmt.Sprintf("%02d:00", booking.Hour)
	timeWithSuffix := fmt.Sprintf("%s hrs", timeStr)

	subject := fmt.Sprintf("Reserva cancelada - %s", booking.SportCenterName)

	message := m.mg.NewMessage(m.from, subject, "", to)
	if m.templateCancel != "" {
		message.SetTemplate(m.templateCancel)
		frontendURL := os.Getenv("URL_FRONTEND")
		if frontendURL == "" {
			frontendURL = "http://localhost:5173"
		}

		vars := map[string]interface{}{
			"booking_code":  booking.BookingCode,
			"center_name":   booking.SportCenterName,
			"court_name":    booking.CourtName,
			"date":          booking.Date.Format("02-01-2006"),
			"hour":          timeWithSuffix,
			"price":         booking.FinalPrice,
			"customer_name": booking.CustomerName,
		}
		if b, err := json.Marshal(vars); err == nil {
			message.AddHeader("X-Mailgun-Variables", string(b))
		} else {
			log.Printf("[MAILGUN] error marshaling template variables (cancel): %v\n", err)
		}
	} else {
		body := fmt.Sprintf("Tu reserva %s en %s (cancha %s) para %s a las %s ha sido cancelada.", booking.BookingCode, booking.SportCenterName, booking.CourtName, booking.Date.Format("2006-01-02"), timeWithSuffix)
		message.SetHtml(body)
	}

	sendCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	_, id, err := m.mg.Send(sendCtx, message)
	if err != nil {
		return fmt.Errorf("mailgun send error: %w", err)
	}
	log.Printf("[MAILGUN] sent cancel message id=%s to=%s", id, to)
	return nil
}

func (m *MailgunMailer) SendContactEmail(ctx context.Context, to string, name string, email string, sportCenterName string, messageBody string) error {
	subject := fmt.Sprintf("Nuevo contacto de Centro Deportivo: %s", sportCenterName)
	body := fmt.Sprintf(`
		<h3>Nuevo mensaje de contacto</h3>
		<p><strong>Nombre:</strong> %s</p>
		<p><strong>Email:</strong> %s</p>
		<p><strong>Centro Deportivo:</strong> %s</p>
		<p><strong>Mensaje:</strong></p>
		<p>%s</p>
	`, name, email, sportCenterName, messageBody)

	message := m.mg.NewMessage(m.from, subject, "", to)
	message.SetHtml(body)

	sendCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	_, id, err := m.mg.Send(sendCtx, message)
	if err != nil {
		return fmt.Errorf("mailgun send contact error: %w", err)
	}
	log.Printf("[MAILGUN] sent contact email id=%s to=%s", id, to)
	return nil
}
