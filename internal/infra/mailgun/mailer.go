package mailgun

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/template"
	"time"

	"github.com/hamp/booking-sport/internal/app"
	"github.com/hamp/booking-sport/internal/domain"
	"github.com/hamp/booking-sport/pkg/logger"
	mg "github.com/mailgun/mailgun-go/v4"
)

func formatNumber(n float64) string {
	s := fmt.Sprintf("%.0f", n)
	var sb strings.Builder
	neg := s[0] == '-'
	if neg {
		sb.WriteByte(s[0])
		s = s[1:]
	}
	for i := 0; i < len(s); i++ {
		if i > 0 && (len(s)-i)%3 == 0 {
			sb.WriteByte('.')
		}
		sb.WriteByte(s[i])
	}
	return sb.String()
}

type MailgunMailer struct {
	mg                    mg.Mailgun
	from                  string
	templateConfirm       string
	templateConfirmPaid   string
	templateCancel        string
	templateWelcomeCenter string
	templateFacility      string
	onSent                func(ctx context.Context, bookingCode, messageID string)
}

// SetOnSent registra un callback que se invoca cuando Mailgun acepta un correo
// de confirmación (reserva o espacio). Sirve para dejar la marca en la reserva.
func (m *MailgunMailer) SetOnSent(fn func(ctx context.Context, bookingCode, messageID string)) {
	m.onSent = fn
}

func (m *MailgunMailer) markSent(bookingCode, messageID string) {
	if m.onSent == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	m.onSent(ctx, bookingCode, messageID)
}

func NewMailgunMailer(apiKey, domain, from, templateConfirm, templateConfirmPaid, templateCancel, templateWelcomeCenter string) *MailgunMailer {
	mgClient := mg.NewMailgun(domain, apiKey)
	// Forzar el nombre "ReservaloYA" en el campo From
	fromName := "ReservaloYA"
	fromAddress := from
	if from == "" {
		fromAddress = "reservas@reservaloya.cl"
	}
	fullFrom := fmt.Sprintf("%s <%s>", fromName, fromAddress)
	return &MailgunMailer{mg: mgClient, from: fullFrom, templateConfirm: templateConfirm, templateConfirmPaid: templateConfirmPaid, templateCancel: templateCancel, templateWelcomeCenter: templateWelcomeCenter}
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
	isPaid := pendingAmount <= 0 || paidAmount >= booking.ChargedPrice()
	paymentMessage := "Recuerda que el saldo pendiente debe ser pagado en el recinto antes de utilizar la cancha."
	if isPaid {
		paymentMessage = "Tu reserva se encuentra completamente pagada. No necesitas realizar pagos adicionales."
	}

	// Política de cancelación
	policyMessage := fmt.Sprintf("Puedes cancelar hasta %d horas antes de la reserva para recibir reembolso completo. Si cancelas con menos de %d horas, se retendrá el %d%% del monto como cargo por cancelación.", cancellationHours, cancellationHours, retentionPercent)

	loc := domain.GetSantiagoLocation()

	timeStr := fmt.Sprintf("%02d:00", booking.Hour)
	timeWithSuffix := fmt.Sprintf("%s hrs", timeStr)

	dateStr := booking.Date.In(loc).Format("02-01-2006")

	subject := fmt.Sprintf("Reserva confirmada - %s", booking.SportCenterName)

	message := m.mg.NewMessage(m.from, subject, "", to)

	templateToUse := m.templateConfirm
	if paidAmount > 0 && m.templateConfirmPaid != "" {
		templateToUse = m.templateConfirmPaid
	}

	if templateToUse != "" {
		message.SetTemplate(templateToUse)
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
			"price":           fmt.Sprintf("$%s", formatNumber(booking.ChargedPrice())),
			"customer_name":   booking.CustomerName,
			"link_cancel":     cancelURL,
			"amount":          fmt.Sprintf("$%s", formatNumber(paidAmount)),
			"amount_pending":  fmt.Sprintf("$%s", formatNumber(pendingAmount)),
			"payment_message": paymentMessage,
			"policy_message":  policyMessage,
		}
		if b, err := json.Marshal(vars); err != nil {
			logger.GetLogger().Warnw("mailgun_template_vars_error",
				"msg", fmt.Sprintf("Error al serializar variables de template de confirmación para booking %s", booking.BookingCode),
				"error", err,
			)
		} else {
			message.AddHeader("X-Mailgun-Variables", string(b))
		}
	} else {
		body := fmt.Sprintf("Tu reserva %s en %s (cancha %s) para el %s a las %s ha sido confirmada.\n\n",
			booking.BookingCode, booking.SportCenterName, booking.CourtName, dateStr, timeWithSuffix)
		body += fmt.Sprintf("Monto pagado: $%.0f\n\n", booking.ChargedPrice())
		body += fmt.Sprintf("Política de cancelación: Puedes cancelar hasta %d horas antes para recibir reembolso completo. ",
			cancellationHours)
		body += fmt.Sprintf("Si cancelas con menos de %d horas, se retendrá el %d%% como cargo por cancelación.\n",
			cancellationHours, retentionPercent)
		message.SetHtml(body)
	}

	sendCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	_, id, err := m.mg.Send(sendCtx, message)
	if err != nil {
		logger.GetLogger().Errorw("mailgun_send_failed",
			"msg", fmt.Sprintf("Error al enviar correo de confirmación a %s para booking %s",
				logger.MaskEmail(to), booking.BookingCode),
			"error", err,
			"to", logger.MaskEmail(to),
			"booking_code", booking.BookingCode,
			"template", templateToUse,
		)
		return fmt.Errorf("mailgun send error: %w", err)
	}

	logger.GetLogger().Infow("mailgun_email_sent",
		"msg", fmt.Sprintf("Correo de confirmación enviado a %s para booking %s",
			logger.MaskEmail(to), booking.BookingCode),
		"to", logger.MaskEmail(to),
		"booking_code", booking.BookingCode,
		"message_id", id,
		"template", templateToUse,
	)
	m.markSent(booking.BookingCode, id)
	return nil
}

// SetFacilityTemplate define la plantilla Mailgun de reservas de espacios por
// día completo. Si está vacía se usa la plantilla de confirmación general.
func (m *MailgunMailer) SetFacilityTemplate(name string) { m.templateFacility = name }

var facilityTypeLabels = map[string]string{"quincho": "Quincho", "sede": "Sede", "piscina": "Piscina"}

func facilityDateLabel(d string) string {
	t, err := time.ParseInLocation("2006-01-02", d, domain.GetSantiagoLocation())
	if err != nil {
		return d
	}
	return t.Format("02-01-2006")
}

// SendFacilityBookingConfirmation confirma una reserva de quincho, sede o
// piscina (uno o más días completos) en un único correo.
func (m *MailgunMailer) SendFacilityBookingConfirmation(ctx context.Context, data app.FacilityConfirmationEmail) error {
	if strings.TrimSpace(data.To) == "" {
		return fmt.Errorf("no recipient email for booking %s", data.BookingCode)
	}
	label := facilityTypeLabels[data.SpaceType]
	if label == "" {
		label = "Espacio"
	}
	dates := make([]string, 0, len(data.Dates))
	for _, d := range data.Dates {
		dates = append(dates, facilityDateLabel(d))
	}
	datesStr := strings.Join(dates, ", ")

	paymentMessage := "Recuerda que el saldo pendiente debe ser pagado en el recinto antes de utilizar el espacio."
	if data.Pending <= 0 {
		paymentMessage = "Tu reserva se encuentra completamente pagada. No necesitas realizar pagos adicionales."
	}
	policyMessage := "Para cancelar o modificar tu reserva, contacta directamente con el centro."

	message := m.mg.NewMessage(m.from, fmt.Sprintf("Reserva confirmada - %s", data.CenterName), "", data.To)
	tpl := m.templateFacility
	if tpl == "" {
		tpl = m.templateConfirm
		if data.Paid > 0 && m.templateConfirmPaid != "" {
			tpl = m.templateConfirmPaid
		}
	}
	spaceFull := fmt.Sprintf("%s %s", label, data.SpaceName)
	if tpl != "" {
		message.SetTemplate(tpl)
		vars := map[string]interface{}{
			"booking_code":    data.BookingCode,
			"center_name":     data.CenterName,
			"court_name":      spaceFull,
			"space_name":      data.SpaceName,
			"space_type":      label,
			"date":            datesStr,
			"dates":           datesStr,
			"hour":            "Día completo",
			"guests":          data.Guests,
			"price":           fmt.Sprintf("$%s", formatNumber(data.Total)),
			"customer_name":   data.CustomerName,
			"amount":          fmt.Sprintf("$%s", formatNumber(data.Paid)),
			"amount_pending":  fmt.Sprintf("$%s", formatNumber(data.Pending)),
			"payment_message": paymentMessage,
			"policy_message":  policyMessage,
		}
		if b, err := json.Marshal(vars); err == nil {
			message.AddHeader("X-Mailgun-Variables", string(b))
		}
	} else {
		body := fmt.Sprintf("Tu reserva %s en %s (%s) para %s (día completo, %d personas) ha sido confirmada.\n\n",
			data.BookingCode, data.CenterName, spaceFull, datesStr, data.Guests)
		body += fmt.Sprintf("Total: $%s\nPagado: $%s\nSaldo: $%s\n\n%s\n%s\n",
			formatNumber(data.Total), formatNumber(data.Paid), formatNumber(data.Pending), paymentMessage, policyMessage)
		message.SetHtml(strings.ReplaceAll(body, "\n", "<br>"))
	}

	sendCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if _, id, err := m.mg.Send(sendCtx, message); err != nil {
		return fmt.Errorf("mailgun send error: %w", err)
	} else {
		logger.GetLogger().Infow("mailgun_email_sent",
			"msg", fmt.Sprintf("Correo de confirmación de espacio enviado a %s para %s", logger.MaskEmail(data.To), data.BookingCode),
			"booking_code", data.BookingCode, "message_id", id, "template", tpl)
		m.markSent(data.BookingCode, id)
	}
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

	loc := domain.GetSantiagoLocation()

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
			"price":         booking.ChargedPrice(),
			"customer_name": booking.CustomerName,
		}
		if b, err := json.Marshal(vars); err == nil {
			message.AddHeader("X-Mailgun-Variables", string(b))
		} else {
			logger.GetLogger().Warnw("mailgun_template_vars_error",
				"msg", fmt.Sprintf("Error al serializar variables de template de cancelación para booking %s", booking.BookingCode),
				"error", err,
			)
		}
	} else {
		body := fmt.Sprintf("Tu reserva %s en %s (cancha %s) para %s a las %s ha sido cancelada.", booking.BookingCode, booking.SportCenterName, booking.CourtName, booking.Date.In(loc).Format("2006-01-02"), timeWithSuffix)
		message.SetHtml(body)
	}

	sendCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	_, id, err := m.mg.Send(sendCtx, message)
	if err != nil {
		logger.GetLogger().Errorw("mailgun_cancel_email_failed",
			"msg", fmt.Sprintf("Error al enviar correo de cancelación a %s para booking %s",
				logger.MaskEmail(to), booking.BookingCode),
			"error", err,
			"to", logger.MaskEmail(to),
			"booking_code", booking.BookingCode,
		)
		return fmt.Errorf("mailgun send error: %w", err)
	}

	logger.GetLogger().Infow("mailgun_cancel_email_sent",
		"msg", fmt.Sprintf("Correo de cancelación enviado a %s para booking %s",
			logger.MaskEmail(to), booking.BookingCode),
		"to", logger.MaskEmail(to),
		"booking_code", booking.BookingCode,
		"message_id", id,
	)
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
		logger.GetLogger().Errorw("mailgun_contact_email_failed",
			"msg", fmt.Sprintf("Error al enviar correo de contacto a %s desde %s",
				logger.MaskEmail(to), logger.MaskEmail(email)),
			"error", err,
			"to", logger.MaskEmail(to),
			"from", logger.MaskEmail(email),
		)
		return fmt.Errorf("mailgun send contact error: %w", err)
	}

	logger.GetLogger().Infow("mailgun_contact_email_sent",
		"msg", fmt.Sprintf("Correo de contacto enviado a %s desde %s",
			logger.MaskEmail(to), logger.MaskEmail(email)),
		"to", logger.MaskEmail(to),
		"from", logger.MaskEmail(email),
		"message_id", id,
	)
	return nil
}

// logoReservaloYA es el logo oficial usado en el encabezado del correo.
const logoReservaloYA = "https://res.cloudinary.com/dn2q4cqi2/image/upload/v1774231856/reservaloya/hgv0velyekp4jnjsiqmz.png"

// welcomeCenterTemplateFile es la plantilla del correo de bienvenida por
// creación de centro deportivo (diseño de referencia de ReservaloYA). Va
// embebida en el binario: se edita en templates/welcome_center.html sin
// tocar código Go.
//
//go:embed templates/welcome_center.html
var welcomeCenterFS embed.FS

var welcomeCenterTemplate = template.Must(template.ParseFS(welcomeCenterFS, "templates/welcome_center.html"))

// welcomeTemplateData son los valores que recibe la plantilla. La plantilla
// se procesa con text/template (sin escapado contextual) porque su contenido
// es HTML de correo controlado por nosotros.
type welcomeTemplateData struct {
	LogoURL        string
	HomeURL        string
	GreetingName   string // nombre del usuario; fallback al correo
	Email          string
	CenterName     string
	Address        string
	TenantURL      string
	NextStepText   string
	ButtonLabel    string
	ActionURL      string
	ShowExpiration bool // solo cuentas nuevas (ticket de un solo uso)
	Title          string
	Preheader      string
	Intro          string
	CenterLabel    string
}

// SendCenterOnboardWelcome envía el correo de bienvenida por cada creación de
// centro deportivo. Incluye información del usuario (nombre/correo) y del
// centro (nombre, dirección, URL pública del subdominio). Para cuentas nuevas
// agrega el enlace de Auth0 para configurar la clave; para cuentas existentes
// enlaza directo al panel.
func (m *MailgunMailer) SendCenterOnboardWelcome(ctx context.Context, data app.CenterWelcomeEmail) error {
	if data.To == "" {
		return fmt.Errorf("no recipient email for center welcome")
	}

	subject := fmt.Sprintf("¡Bienvenido a ReservaloYA! - %s", data.CenterName)

	loginURL := data.LoginURL
	if loginURL == "" {
		loginURL = strings.TrimRight(os.Getenv("URL_FRONTEND"), "/") + "/admin"
	}
	isNewAccount := data.SetupURL != ""
	actionURL := loginURL
	buttonLabel := "Ir a mi panel"
	nextStepText := "Ingresa al panel para gestionar tus espacios, horarios y reservas."
	if isNewAccount {
		actionURL = data.SetupURL
		buttonLabel = "Configurar mi cuenta"
		nextStepText = "Para comenzar, configura tu contraseña y activa tu cuenta."
	}

	message := m.mg.NewMessage(m.from, subject, "", data.To)
	if m.templateWelcomeCenter != "" {
		message.SetTemplate(m.templateWelcomeCenter)
		vars := map[string]interface{}{
			"user_name":    data.UserName,
			"email":        data.To,
			"center_name":  data.CenterName,
			"address":      data.Address,
			"site_url":     data.TenantURL,
			"login_url":    loginURL,
			"setup_url":    data.SetupURL,
			"action_url":   actionURL,
			"button_label": buttonLabel,
		}
		if b, err := json.Marshal(vars); err != nil {
			logger.GetLogger().Warnw("mailgun_template_vars_error",
				"msg", fmt.Sprintf("Error al serializar variables de template de bienvenida para centro %q", data.CenterName),
				"error", err,
			)
		} else {
			message.AddHeader("X-Mailgun-Variables", string(b))
		}
	} else {
		greetingName := data.UserName
		if greetingName == "" {
			greetingName = data.To
		}
		homeURL := strings.TrimRight(os.Getenv("URL_FRONTEND"), "/")
		if homeURL == "" {
			homeURL = "http://localhost:3000"
		}
		var body bytes.Buffer
		err := welcomeCenterTemplate.Execute(&body, welcomeTemplateData{
			LogoURL:        logoReservaloYA,
			HomeURL:        homeURL,
			GreetingName:   greetingName,
			Email:          data.To,
			CenterName:     data.CenterName,
			Address:        data.Address,
			TenantURL:      data.TenantURL,
			NextStepText:   nextStepText,
			ButtonLabel:    buttonLabel,
			ActionURL:      actionURL,
			ShowExpiration: isNewAccount,
			Title:          "¡Bienvenido a ReservaloYA!",
			Preheader:      "Tu centro deportivo está listo en ReservaloYA.",
			Intro:          "Registramos con éxito tu centro deportivo en ReservaloYA. Estos son tus datos de acceso y los de tu centro:",
			CenterLabel:    "Tu centro deportivo",
		})
		if err != nil {
			logger.GetLogger().Errorw("mailgun_welcome_template_error",
				"msg", fmt.Sprintf("Error al ejecutar plantilla de bienvenida para centro %s", data.CenterName),
				"error", err,
			)
			return fmt.Errorf("mailgun welcome template error: %w", err)
		}
		message.SetHtml(body.String())
	}

	sendCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	_, id, err := m.mg.Send(sendCtx, message)
	if err != nil {
		logger.GetLogger().Errorw("mailgun_welcome_email_failed",
			"msg", fmt.Sprintf("Error al enviar correo de bienvenida a %s para centro %s",
				logger.MaskEmail(data.To), data.CenterName),
			"error", err,
			"to", logger.MaskEmail(data.To),
			"template", m.templateWelcomeCenter,
		)
		return fmt.Errorf("mailgun send error: %w", err)
	}

	logger.GetLogger().Infow("mailgun_welcome_email_sent",
		"msg", fmt.Sprintf("Correo de bienvenida enviado a %s para centro %s",
			logger.MaskEmail(data.To), data.CenterName),
		"to", logger.MaskEmail(data.To),
		"message_id", id,
	)
	return nil
}

// SendCenterInvitation envía la invitación para administrar un centro ya
// existente. Siempre usa la plantilla HTML local (no el template alojado en
// Mailgun, que es el de bienvenida por creación de centro). Si la cuenta es
// nueva (SetupURL presente) el botón lleva a crear la contraseña; si ya tenía
// cuenta, enlaza directo al panel.
func (m *MailgunMailer) SendCenterInvitation(ctx context.Context, data app.CenterInvitationEmail) error {
	if data.To == "" {
		return fmt.Errorf("no recipient email for center invitation")
	}

	loginURL := data.LoginURL
	if loginURL == "" {
		loginURL = strings.TrimRight(os.Getenv("URL_FRONTEND"), "/") + "/admin"
	}
	isNewAccount := data.SetupURL != ""
	actionURL := loginURL
	buttonLabel := "Ir al panel"
	nextStepText := "Ingresa con tu cuenta de ReservaloYA para gestionar el centro."
	if isNewAccount {
		actionURL = data.SetupURL
		buttonLabel = "Crear mi contraseña"
		nextStepText = "Aún no tienes una cuenta, así que la creamos por ti. Crea tu contraseña con el botón para activarla; después ingresa con este mismo correo."
	}

	greetingName := data.UserName
	if greetingName == "" {
		greetingName = data.To
	}
	homeURL := strings.TrimRight(os.Getenv("URL_FRONTEND"), "/")
	if homeURL == "" {
		homeURL = "http://localhost:3000"
	}

	var body bytes.Buffer
	err := welcomeCenterTemplate.Execute(&body, welcomeTemplateData{
		LogoURL:        logoReservaloYA,
		HomeURL:        homeURL,
		GreetingName:   greetingName,
		Email:          data.To,
		CenterName:     data.CenterName,
		Address:        data.Address,
		TenantURL:      data.TenantURL,
		NextStepText:   nextStepText,
		ButtonLabel:    buttonLabel,
		ActionURL:      actionURL,
		ShowExpiration: isNewAccount,
		Title:          "Te invitaron a administrar un centro",
		Preheader:      fmt.Sprintf("Te dieron acceso como administrador de %s en ReservaloYA.", data.CenterName),
		Intro:          fmt.Sprintf("Te dieron acceso como administrador de %s en ReservaloYA. Podrás gestionar sus espacios, horarios y reservas.", data.CenterName),
		CenterLabel:    "Centro deportivo",
	})
	if err != nil {
		return fmt.Errorf("mailgun invitation template error: %w", err)
	}

	subject := fmt.Sprintf("Te invitaron a administrar %s en ReservaloYA", data.CenterName)
	message := m.mg.NewMessage(m.from, subject, "", data.To)
	message.SetHtml(body.String())

	sendCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	_, id, err := m.mg.Send(sendCtx, message)
	if err != nil {
		logger.GetLogger().Errorw("mailgun_invitation_email_failed",
			"msg", fmt.Sprintf("Error al enviar invitación a %s para centro %s",
				logger.MaskEmail(data.To), data.CenterName),
			"error", err,
			"to", logger.MaskEmail(data.To),
		)
		return fmt.Errorf("mailgun send error: %w", err)
	}

	logger.GetLogger().Infow("mailgun_invitation_email_sent",
		"msg", fmt.Sprintf("Invitación enviada a %s para centro %s", logger.MaskEmail(data.To), data.CenterName),
		"to", logger.MaskEmail(data.To),
		"message_id", id,
	)
	return nil
}

// memberVerificationFS contiene la plantilla del correo con el código de
// confirmación de socio. Va embebida para no depender del sistema de archivos
// en producción.
//
//go:embed templates/member_verification.html
var memberVerificationFS embed.FS

var memberVerificationTemplate = template.Must(template.ParseFS(memberVerificationFS, "templates/member_verification.html"))

type memberVerificationTemplateData struct {
	CenterName     string
	Name           string
	Code           string
	ExpiresMinutes int
}

// SendMemberVerificationCode envía el código de 6 dígitos al correo del socio.
func (m *MailgunMailer) SendMemberVerificationCode(ctx context.Context, to, name, centerName, code string, expiresMinutes int) error {
	if to == "" {
		return fmt.Errorf("no recipient email for member verification")
	}
	if name == "" {
		name = "socio"
	}
	if centerName == "" {
		centerName = "ReservaloYA"
	}
	subject := fmt.Sprintf("Tu código de socio - %s", centerName)
	message := m.mg.NewMessage(m.from, subject, "", to)

	var body bytes.Buffer
	if err := memberVerificationTemplate.Execute(&body, memberVerificationTemplateData{
		CenterName:     centerName,
		Name:           name,
		Code:           code,
		ExpiresMinutes: expiresMinutes,
	}); err != nil {
		return err
	}
	message.SetHtml(body.String())

	sendCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_, _, err := m.mg.Send(sendCtx, message)
	return err
}
