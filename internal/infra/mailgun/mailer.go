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
	mg                  mg.Mailgun
	from                string
	templateConfirm     string
	templateConfirmPaid string
	templateCancel      string
}

func NewMailgunMailer(apiKey, domain, from, templateConfirm, templateConfirmPaid, templateCancel string) *MailgunMailer {
	mgClient := mg.NewMailgun(domain, apiKey)
	// Forzar el nombre "ReservaloYA" en el campo From
	fromName := "ReservaloYA"
	fromAddress := from
	if from == "" {
		fromAddress = "reservas@reservaloya.cl"
	}
	fullFrom := fmt.Sprintf("%s <%s>", fromName, fromAddress)
	return &MailgunMailer{mg: mgClient, from: fullFrom, templateConfirm: templateConfirm, templateConfirmPaid: templateConfirmPaid, templateCancel: templateCancel}
}

func (m *MailgunMailer) SendBookingConfirmation(ctx context.Context, booking *domain.Booking, cancellationHours, retentionPercent int, paidAmount, pendingAmount float64) error {
	var to string
	if booking.GuestDetails != nil && booking.GuestDetails.Email != "" {
		to = booking.GuestDetails.Email
	}
	if to == "" {
		// No hay destinatario
		return fmt.Errorf("no recipient email for booking %s", booking.BookingCode)
	}

	// Valores por defecto si no se proporcionan
	if cancellationHours == 0 {
		cancellationHours = 3
	}
	if retentionPercent == 0 {
		retentionPercent = 10
	}

	// Determinar si está completamente pagado
	isPaid := pendingAmount <= 0 || paidAmount >= booking.FinalPrice
	paymentMessage := "Recuerda que el saldo pendiente debe ser pagado en el recinto antes de utilizar la cancha."
	if isPaid {
		paymentMessage = "Tu reserva se encuentra completamente pagada. No necesitas realizar pagos adicionales."
	}

	// Política de cancelación
	policyMessage := fmt.Sprintf("Puedes cancelar hasta %d horas antes de la reserva para recibir reembolso completo. Si cancelas con menos de %d horas, se retendrá el %d%% del monto como cargo por cancelación.", cancellationHours, cancellationHours, retentionPercent)

	// Cargar zona horaria de Santiago
	loc, err := time.LoadLocation("America/Santiago")
	if err != nil {
		log.Printf("[MAILGUN] error loading location America/Santiago: %v, using UTC\n", err)
		loc = time.UTC
	}

	// Formatear hora usando únicamente `booking.Hour` (sin minutos).
	timeStr := fmt.Sprintf("%02d:00", booking.Hour)
	// Añadir sufijo " hrs" tal como se solicita (ej. "16:00 hrs").
	timeWithSuffix := fmt.Sprintf("%s hrs", timeStr)

	// Formatear fecha
	dateStr := booking.Date.In(loc).Format("02-01-2006")

	subject := fmt.Sprintf("Reserva confirmada - %s", booking.SportCenterName)

	message := m.mg.NewMessage(m.from, subject, "", to)

	// Seleccionar template: usar paid si hay monto pagado mayor a 0
	templateToUse := m.templateConfirm
	if paidAmount > 0 && m.templateConfirmPaid != "" {
		templateToUse = m.templateConfirmPaid
	}

	if templateToUse != "" {
		message.SetTemplate(templateToUse)
		// Variables de plantilla
		// Construir URL pública de cancelación usando la URL del frontend
		frontendURL := os.Getenv("URL_FRONTEND")
		if frontendURL == "" {
			frontendURL = "http://localhost:5173"
		}

		cancelURL := fmt.Sprintf("%s/booking/cancel?code=%s", frontendURL, booking.BookingCode)

		vars := map[string]interface{}{
			"booking_code":    booking.BookingCode,
			"center_name":     booking.SportCenterName,
			"court_name":      booking.CourtName,
			"date":            dateStr,
			"hour":            timeWithSuffix,
			"price":           booking.FinalPrice,
			"customer_name":   booking.CustomerName,
			"link_cancel":     cancelURL,
			"amount":          paidAmount,
			"amount_pending":  pendingAmount,
			"payment_message": paymentMessage,
			"policy_message":  policyMessage,
		}
		if b, err := json.Marshal(vars); err == nil {
			message.AddHeader("X-Mailgun-Variables", string(b))
		} else {
			log.Printf("[MAILGUN] error marshaling template variables: %v\n", err)
		}
	} else {
		// fallback simple body con política de cancelación
		body := fmt.Sprintf("Tu reserva %s en %s (cancha %s) para el %s a las %s ha sido confirmada.\n\n",
			booking.BookingCode, booking.SportCenterName, booking.CourtName, dateStr, timeWithSuffix)
		body += fmt.Sprintf("Monto pagado: $%.0f\n\n", booking.FinalPrice)
		body += fmt.Sprintf("Política de cancelación: Puedes cancelar hasta %d horas antes para recibir reembolso completo. ",
			cancellationHours)
		body += fmt.Sprintf("Si cancelas con menos de %d horas, se retendrá el %d%% como cargo por cancelación.\n",
			cancellationHours, retentionPercent)
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

	// Cargar zona horaria de Santiago
	loc, err := time.LoadLocation("America/Santiago")
	if err != nil {
		log.Printf("[MAILGUN] error loading location America/Santiago: %v, using UTC\n", err)
		loc = time.UTC
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
			"date":          booking.Date.In(loc).Format("02-01-2006"),
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
		body := fmt.Sprintf("Tu reserva %s en %s (cancha %s) para %s a las %s ha sido cancelada.", booking.BookingCode, booking.SportCenterName, booking.CourtName, booking.Date.In(loc).Format("2006-01-02"), timeWithSuffix)
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

func (m *MailgunMailer) SendContactEmail(ctx context.Context, to string, name string, email string, phone string, sportCenterName string, messageBody string) error {
	subject := fmt.Sprintf("Nuevo contacto de Centro Deportivo: %s", sportCenterName)
	body := fmt.Sprintf(`
		<h3>Nuevo mensaje de contacto</h3>
		<p><strong>Nombre:</strong> %s</p>
		<p><strong>Email:</strong> %s</p>
		<p><strong>Teléfono:</strong> %s</p>
		<p><strong>Centro Deportivo:</strong> %s</p>
		<p><strong>Mensaje:</strong></p>
		<p>%s</p>
	`, name, email, phone, sportCenterName, messageBody)

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
