package app

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"github.com/hamp/booking-sport/pkg/logger"
	"github.com/hamp/booking-sport/pkg/mercadopago"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

const (
	facilityBookingMaxDaysAhead = 365
	facilityBookingMaxDates     = 62
	facilityBookingHoldTTL      = 15 * time.Minute

	FacilityPaymentOnline = "mercadopago"
	FacilityPaymentVenue  = "venue"
)

type FacilityBookingRepository interface {
	CreateMany(ctx context.Context, bookings []*domain.FacilityBooking) error
	ExpireStale(ctx context.Context, spaceID primitive.ObjectID, now time.Time) error
	FindByCode(ctx context.Context, code string) ([]domain.FacilityBooking, error)
	FindByMPPaymentID(ctx context.Context, paymentID string) ([]domain.FacilityBooking, error)
	FindByID(ctx context.Context, id primitive.ObjectID) (*domain.FacilityBooking, error)
	ListActiveDates(ctx context.Context, spaceID primitive.ObjectID, from, to string, now time.Time) ([]string, error)
	ListByCenter(ctx context.Context, centerID primitive.ObjectID) ([]domain.FacilityBooking, error)
	SetPreference(ctx context.Context, code, preferenceID string) error
	SetPaymentID(ctx context.Context, code, paymentID string) error
	Confirm(ctx context.Context, code string, paidPerDay float64) error
	CancelByCode(ctx context.Context, code, cancelledBy string, status domain.BookingStatus) error
	CancelByID(ctx context.Context, id primitive.ObjectID, cancelledBy string) error
}

// FacilityConfirmationEmail reúne los datos del correo de confirmación de una
// reserva de espacio por día completo (una compra, uno o más días).
type FacilityConfirmationEmail struct {
	To            string
	CustomerName  string
	BookingCode   string
	CenterName    string
	SpaceName     string
	SpaceType     string
	Dates         []string // yyyy-mm-dd
	Guests        int
	Total         float64
	Paid          float64
	Pending       float64
	PaymentMethod string
}

// FacilityMailer envía el correo de confirmación de espacios por día completo.
type FacilityMailer interface {
	SendFacilityBookingConfirmation(ctx context.Context, data FacilityConfirmationEmail) error
}

// FacilityBookingUseCase gestiona las reservas por día de sedes y piscinas.
type FacilityBookingUseCase struct {
	repo       FacilityBookingRepository
	spaceRepo  SpaceRepository
	centerRepo SportCenterRepository
	schedule   *ScheduleUseCase
	mpClient   *mercadopago.Client
	mailer     FacilityMailer
	async      func(func())
	now        func() time.Time
}

func NewFacilityBookingUseCase(
	repo FacilityBookingRepository,
	spaceRepo SpaceRepository,
	centerRepo SportCenterRepository,
	schedule *ScheduleUseCase,
	mpClient *mercadopago.Client,
) *FacilityBookingUseCase {
	return &FacilityBookingUseCase{
		repo:       repo,
		spaceRepo:  spaceRepo,
		centerRepo: centerRepo,
		schedule:   schedule,
		mpClient:   mpClient,
		now:        time.Now,
		async:      func(f func()) { go f() },
	}
}

// SetMailer habilita el correo de confirmación (opcional).
func (uc *FacilityBookingUseCase) SetMailer(m FacilityMailer) { uc.mailer = m }

// notifyConfirmed envía un único correo por compra con todos los días. Un
// fallo de envío se registra pero nunca afecta a la reserva.
func (uc *FacilityBookingUseCase) notifyConfirmed(docs []domain.FacilityBooking) {
	if uc.mailer == nil || len(docs) == 0 || uc.async == nil {
		return
	}
	first := docs[0]
	if strings.TrimSpace(first.CustomerEmail) == "" {
		return
	}
	data := FacilityConfirmationEmail{
		To:            first.CustomerEmail,
		CustomerName:  first.CustomerName,
		BookingCode:   first.BookingCode,
		CenterName:    first.SportCenterName,
		SpaceName:     first.SpaceName,
		SpaceType:     string(first.SpaceType),
		Guests:        first.Guests,
		PaymentMethod: first.PaymentMethod,
	}
	for _, d := range docs {
		data.Dates = append(data.Dates, d.LocalDate)
		data.Total += d.Price
		data.Paid += d.PaidAmount
		data.Pending += d.PendingAmount
	}
	sort.Strings(data.Dates)
	mailer := uc.mailer
	uc.async(func() {
		if err := mailer.SendFacilityBookingConfirmation(context.Background(), data); err != nil {
			logger.FromContext(context.Background()).Errorw("mail_facility_confirmation_error",
				"msg", fmt.Sprintf("Error al enviar correo de confirmación de la reserva %s", data.BookingCode),
				"booking_code", data.BookingCode,
				"error", err,
			)
		}
	})
}

type CreateFacilityBookingInput struct {
	SpaceID       primitive.ObjectID
	Dates         []string
	Guests        int
	CustomerName  string
	CustomerEmail string
	CustomerPhone string
	// PaymentMethod: "mercadopago" o "venue". Vacío usa el valor por defecto del centro.
	PaymentMethod string
	// Partial solicita pagar solo el abono configurado por el centro.
	Partial bool
}

type FacilityBookingResult struct {
	BookingCode string                   `json:"booking_code"`
	Status      domain.BookingStatus     `json:"status"`
	InitPoint   string                   `json:"init_point,omitempty"`
	Total       float64                  `json:"total"`
	AmountToPay float64                  `json:"amount_to_pay"`
	Bookings    []domain.FacilityBooking `json:"bookings"`
}

// normalizeFacilityGuests valida la cantidad de personas declarada. El máximo
// del espacio es solo informativo: no bloquea la reserva.
func normalizeFacilityGuests(guests int) (int, error) {
	if guests == 0 {
		return 1, nil
	}
	if guests < 0 {
		return 0, domain.NewBadRequestError("la cantidad de personas no es válida")
	}
	return guests, nil
}

// resolveFacilityPaymentMethod aplica las mismas modalidades de cobro que las canchas.
func resolveFacilityPaymentMethod(center *domain.SportCenter, requested string) (string, error) {
	mpConfigured := center.MercadoPago != nil && center.MercadoPago.AccessToken != ""
	onlineAllowed := center.PaymentMode != PaymentModePayAtCenter && mpConfigured
	venueAllowed := center.PaymentMode != PaymentModePayOnline

	if requested == "" {
		if center.PaymentMode == PaymentModePayOnline {
			requested = FacilityPaymentOnline
		} else {
			requested = FacilityPaymentVenue
		}
	}
	switch requested {
	case FacilityPaymentOnline:
		if !onlineAllowed {
			return "", domain.NewBadRequestError("el pago online no está disponible en este centro")
		}
	case FacilityPaymentVenue:
		if !venueAllowed {
			return "", domain.NewBadRequestError("este centro solo acepta pago online")
		}
	default:
		return "", domain.NewBadRequestError("método de pago inválido")
	}
	return requested, nil
}

func (uc *FacilityBookingUseCase) parseDates(raw []string) ([]string, error) {
	if len(raw) == 0 {
		return nil, domain.NewBadRequestError("debes seleccionar al menos un día")
	}
	if len(raw) > facilityBookingMaxDates {
		return nil, domain.NewBadRequestError(fmt.Sprintf("puedes reservar hasta %d días por vez", facilityBookingMaxDates))
	}
	loc := domain.GetSantiagoLocation()
	nowLocal := uc.now().In(loc)
	today := time.Date(nowLocal.Year(), nowLocal.Month(), nowLocal.Day(), 0, 0, 0, 0, loc)
	last := today.AddDate(0, 0, facilityBookingMaxDaysAhead)

	seen := make(map[string]bool, len(raw))
	out := make([]string, 0, len(raw))
	for _, s := range raw {
		d, err := time.ParseInLocation("2006-01-02", s, loc)
		if err != nil {
			return nil, domain.NewBadRequestError(fmt.Sprintf("fecha inválida: %q", s))
		}
		if d.Before(today) {
			return nil, domain.NewBadRequestError("no se puede reservar un día pasado")
		}
		if d.After(last) {
			return nil, domain.NewBadRequestError("la fecha está fuera del rango permitido")
		}
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out, nil
}

func (uc *FacilityBookingUseCase) Create(ctx context.Context, in CreateFacilityBookingInput) (*FacilityBookingResult, error) {
	space, err := uc.spaceRepo.FindByID(ctx, in.SpaceID)
	if err != nil {
		return nil, domain.NewNotFoundError("espacio no encontrado")
	}
	if !space.Enabled || !space.Category.IsFacility() {
		return nil, domain.NewBadRequestError("este espacio no admite reservas por día")
	}
	guests, err := normalizeFacilityGuests(in.Guests)
	if err != nil {
		return nil, err
	}

	name := strings.TrimSpace(in.CustomerName)
	email := strings.TrimSpace(in.CustomerEmail)
	phone := strings.TrimSpace(in.CustomerPhone)
	if name == "" || phone == "" || !strings.Contains(email, "@") {
		return nil, domain.NewBadRequestError("nombre, correo y teléfono son obligatorios")
	}

	dates, err := uc.parseDates(in.Dates)
	if err != nil {
		return nil, err
	}

	center, err := uc.centerRepo.FindByID(ctx, space.SportCenterID)
	if err != nil {
		return nil, domain.NewNotFoundError("centro no encontrado")
	}
	method, err := resolveFacilityPaymentMethod(center, in.PaymentMethod)
	if err != nil {
		return nil, err
	}

	if uc.schedule != nil {
		loc := domain.GetSantiagoLocation()
		for _, d := range dates {
			day, _ := time.ParseInLocation("2006-01-02", d, loc)
			avail, err := uc.schedule.PublicAvailability(ctx, space.ID, day)
			if err != nil {
				return nil, err
			}
			if avail.Closed || avail.Blocked {
				return nil, domain.NewConflictError(fmt.Sprintf("el día %s no está disponible", d))
			}
		}
	}

	price := space.PricePerDay
	if price <= 0 {
		price = space.PriceAllDay
	}
	total := price * float64(len(dates))
	amountToPay := total
	isPartial := false
	if method == FacilityPaymentOnline {
		if total <= 0 {
			return nil, domain.NewBadRequestError("este espacio no tiene precio configurado para pago online")
		}
		if in.Partial && center.PartialPaymentEnabled {
			percent := center.PartialPaymentPercent
			if percent <= 0 || percent > 100 {
				percent = 50
			}
			amountToPay = math.Round(total * float64(percent) / 100)
			isPartial = true
		}
	}

	now := uc.now()
	if err := uc.repo.ExpireStale(ctx, space.ID, now); err != nil {
		return nil, err
	}

	code := domain.FacilityBookingCodePrefix + strings.ToUpper(generateBookingCode())
	status := domain.BookingStatusConfirmed
	var expiresAt *time.Time
	if method == FacilityPaymentOnline {
		status = domain.BookingStatusPending
		exp := now.Add(facilityBookingHoldTTL)
		expiresAt = &exp
	}

	docs := make([]*domain.FacilityBooking, 0, len(dates))
	for _, d := range dates {
		docs = append(docs, &domain.FacilityBooking{
			BookingCode:      code,
			SpaceID:          space.ID,
			SportCenterID:    space.SportCenterID,
			SpaceName:        space.Name,
			SpaceType:        domain.FacilityType(space.Category),
			SportCenterName:  center.Name,
			LocalDate:        d,
			Guests:           guests,
			CustomerName:     name,
			CustomerEmail:    email,
			CustomerPhone:    phone,
			Status:           status,
			Active:           true,
			PaymentMethod:    method,
			Price:            price,
			PendingAmount:    price,
			IsPartialPayment: isPartial,
			ExpiresAt:        expiresAt,
			CreatedAt:        now,
			UpdatedAt:        now,
		})
	}

	if method == FacilityPaymentOnline && len(docs) > 0 {
		rate := centerCommissionRate(center)
		perDay := amountToPay / float64(len(docs))
		for _, d := range docs {
			d.CommissionRate = rate
			d.CommissionAmount = commissionFee(perDay, rate)
		}
	}

	if err := uc.repo.CreateMany(ctx, docs); err != nil {
		if errors.Is(err, domain.ErrBookingSlotTaken) {
			return nil, domain.NewConflictError("uno de los días seleccionados ya fue reservado")
		}
		return nil, err
	}

	result := &FacilityBookingResult{
		BookingCode: code,
		Status:      status,
		Total:       total,
		AmountToPay: amountToPay,
	}

	if method == FacilityPaymentOnline {
		initPoint, prefID, err := uc.createPreference(ctx, center, space, docs, amountToPay, isPartial, expiresAt)
		if err != nil {
			_ = uc.repo.CancelByCode(ctx, code, "system", domain.BookingStatusCancelled)
			return nil, err
		}
		result.InitPoint = initPoint
		_ = uc.repo.SetPreference(ctx, code, prefID)
	} else {
		result.AmountToPay = 0
		confirmed := make([]domain.FacilityBooking, 0, len(docs))
		for _, d := range docs {
			confirmed = append(confirmed, *d)
		}
		uc.notifyConfirmed(confirmed)
	}

	for _, d := range docs {
		result.Bookings = append(result.Bookings, *d)
	}
	return result, nil
}

func (uc *FacilityBookingUseCase) createPreference(
	ctx context.Context,
	center *domain.SportCenter,
	space *domain.Space,
	docs []*domain.FacilityBooking,
	amountToPay float64,
	isPartial bool,
	expiresAt *time.Time,
) (string, string, error) {
	first := docs[0]
	client := mercadopago.NewClient(center.MercadoPago.AccessToken)

	urlFrontend := os.Getenv("URL_FRONTEND")
	notificationURL := os.Getenv("URL_MP_WEBHOOK")
	urlPaymentCallback := os.Getenv("URL_MP_CALLBACK")
	successURL := fmt.Sprintf("%s?code=%s", urlPaymentCallback, first.BookingCode)
	failureURL := fmt.Sprintf("%s/booking/failure", urlFrontend)

	title := fmt.Sprintf("Reserva %s - %s - %d día(s)", space.Name, center.Name, len(docs))
	if isPartial {
		title = "Abono " + title
	}
	payerName, payerSurname := splitFullName(first.CustomerName)

	dates := make([]string, 0, len(docs))
	for _, d := range docs {
		dates = append(dates, d.LocalDate)
	}

	result, err := client.CreatePreference(ctx, mercadopago.PreferenceInput{
		Title:           title,
		Amount:          amountToPay,
		Email:           first.CustomerEmail,
		PayerName:       payerName,
		PayerSurname:    payerSurname,
		ExternalRef:     first.BookingCode,
		SuccessURL:      successURL,
		FailureURL:      failureURL,
		PendingURL:      successURL,
		NotificationURL: notificationURL,
		ExpirationDate:  expiresAt,
		MarketplaceFee:  commissionFee(amountToPay, centerCommissionRate(center)),
		Metadata: map[string]any{
			"booking_code": first.BookingCode,
			"space":        space.Name,
			"center":       center.Name,
			"center_id":    center.ID.Hex(),
			"dates":        strings.Join(dates, ","),
			"guests":       first.Guests,
			"is_partial":   isPartial,
		},
	})
	if err != nil {
		return "", "", fmt.Errorf("error creating mercadopago preference: %w", err)
	}
	return result.InitPoint, result.ID, nil
}

// FacilityPaymentOptions describe cómo puede pagar el cliente una reserva de
// un espacio, según la modalidad del centro (igual que las canchas).
type FacilityPaymentOptions struct {
	Online         bool    `json:"online"`
	Venue          bool    `json:"venue"`
	Default        string  `json:"default"`
	PartialEnabled bool    `json:"partial_enabled"`
	PartialPercent int     `json:"partial_percent"`
	PricePerDay    float64 `json:"price_per_day"`
	Capacity       int     `json:"capacity"`
}

func (uc *FacilityBookingUseCase) PaymentOptions(ctx context.Context, spaceID primitive.ObjectID) (*FacilityPaymentOptions, error) {
	space, err := uc.spaceRepo.FindByID(ctx, spaceID)
	if err != nil {
		return nil, domain.NewNotFoundError("espacio no encontrado")
	}
	center, err := uc.centerRepo.FindByID(ctx, space.SportCenterID)
	if err != nil {
		return nil, domain.NewNotFoundError("centro no encontrado")
	}
	mpConfigured := center.MercadoPago != nil && center.MercadoPago.AccessToken != ""
	opts := &FacilityPaymentOptions{
		Online:      center.PaymentMode != PaymentModePayAtCenter && mpConfigured && space.PricePerDay > 0,
		Venue:       center.PaymentMode != PaymentModePayOnline,
		PricePerDay: space.PricePerDay,
		Capacity:    space.Capacity,
	}
	if center.PartialPaymentEnabled {
		opts.PartialEnabled = true
		opts.PartialPercent = center.PartialPaymentPercent
		if opts.PartialPercent <= 0 || opts.PartialPercent > 100 {
			opts.PartialPercent = 50
		}
	}
	if opts.Online && center.PaymentMode == PaymentModePayOnline {
		opts.Default = FacilityPaymentOnline
	} else if opts.Venue {
		opts.Default = FacilityPaymentVenue
	} else if opts.Online {
		opts.Default = FacilityPaymentOnline
	}
	return opts, nil
}

// GetByCode devuelve todos los días de una reserva.
func (uc *FacilityBookingUseCase) GetByCode(ctx context.Context, code string) ([]domain.FacilityBooking, error) {
	docs, err := uc.repo.FindByCode(ctx, code)
	if err != nil {
		return nil, err
	}
	if len(docs) == 0 {
		return nil, domain.NewNotFoundError("reserva no encontrada")
	}
	return docs, nil
}

// StorePaymentID guarda el id de pago devuelto por Mercado Pago al volver al sitio.
func (uc *FacilityBookingUseCase) StorePaymentID(ctx context.Context, code, paymentID string) error {
	return uc.repo.SetPaymentID(ctx, code, paymentID)
}

// HandlePaymentNotification procesa una notificación de pago de Mercado Pago.
// Devuelve handled=false si el pago no corresponde a una reserva de sede/piscina.
func (uc *FacilityBookingUseCase) HandlePaymentNotification(ctx context.Context, paymentIDStr string) (bool, error) {
	paymentID, err := strconv.Atoi(paymentIDStr)
	if err != nil {
		return false, fmt.Errorf("invalid payment ID: %w", err)
	}
	if uc.mpClient == nil {
		return false, nil
	}

	docs, err := uc.repo.FindByMPPaymentID(ctx, paymentIDStr)
	if err != nil {
		return false, err
	}

	payment, err := uc.mpClient.GetPayment(ctx, paymentID)
	if err != nil {
		if len(docs) > 0 {
			return true, fmt.Errorf("error getting payment %d from mercadopago: %w", paymentID, err)
		}
		return false, nil
	}
	if len(docs) == 0 {
		if !strings.HasPrefix(payment.ExternalReference, domain.FacilityBookingCodePrefix) {
			return false, nil
		}
		docs, err = uc.repo.FindByCode(ctx, payment.ExternalReference)
		if err != nil {
			return true, err
		}
		if len(docs) == 0 {
			return true, fmt.Errorf("facility booking not found for code %s", payment.ExternalReference)
		}
		if err := uc.repo.SetPaymentID(ctx, payment.ExternalReference, paymentIDStr); err != nil {
			return true, err
		}
	}

	code := docs[0].BookingCode
	log := logger.FromContext(ctx)

	switch payment.Status {
	case "approved":
		if docs[0].Status == domain.BookingStatusConfirmed {
			return true, nil
		}
		if !docs[0].IsActive() {
			log.Errorw("facility_late_payment",
				"msg", fmt.Sprintf("Pago aprobado para la reserva %s que ya no está activa; requiere reembolso manual", code),
				"booking_code", code, "payment_id", paymentID)
			return true, nil
		}
		paidPerDay := payment.TransactionAmount / float64(len(docs))
		if err := uc.repo.Confirm(ctx, code, paidPerDay); err != nil {
			return true, err
		}
		if confirmed, err := uc.repo.FindByCode(ctx, code); err == nil {
			uc.notifyConfirmed(confirmed)
		}
		return true, nil
	case "rejected", "cancelled":
		return true, uc.repo.CancelByCode(ctx, code, "payment", domain.BookingStatusCancelled)
	}
	return true, nil
}

// ListForAdmin lista las reservas de sede y piscina del centro del usuario.
func (uc *FacilityBookingUseCase) ListForAdmin(ctx context.Context, userID string) ([]domain.FacilityBooking, error) {
	centers, err := uc.centerRepo.FindByUserID(ctx, userID)
	if err != nil || len(centers) == 0 {
		return []domain.FacilityBooking{}, nil
	}
	docs, err := uc.repo.ListByCenter(ctx, centers[0].ID)
	if err != nil {
		return nil, err
	}
	if docs == nil {
		docs = []domain.FacilityBooking{}
	}
	return docs, nil
}

// CancelByAdmin cancela el día indicado de una reserva y libera la fecha.
func (uc *FacilityBookingUseCase) CancelByAdmin(ctx context.Context, id primitive.ObjectID, userID string) error {
	booking, err := uc.repo.FindByID(ctx, id)
	if err != nil {
		return domain.NewNotFoundError("reserva no encontrada")
	}
	center, err := uc.centerRepo.FindByID(ctx, booking.SportCenterID)
	if err != nil {
		return err
	}
	if !centerOwnsUser(center, userID) {
		return &domain.AppError{Message: "no autorizado", StatusCode: 403}
	}
	return uc.repo.CancelByID(ctx, id, "admin")
}
