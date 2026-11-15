package app

import (
	"context"
	"crypto/subtle"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

const (
	accessVerificationTTL      = 10 * time.Minute
	accessVerificationAttempts = 5
	accessVerificationCooldown = 60 * time.Second
	accessVerificationWindow   = 15 * time.Minute
	accessVerificationMaxSends = 5
	accessVerificationIPMax    = 10
	accessTokenTTL             = 30 * 24 * time.Hour
	accessResubmitAfter        = 24 * time.Hour
	accessMaxCustomFields      = 20
)

// CenterAccessRepository es la persistencia de solicitudes, códigos y tokens
// de acceso a centros privados.
type CenterAccessRepository interface {
	CreateRequest(ctx context.Context, request *domain.CenterAccessRequest) error
	UpdateRequest(ctx context.Context, request *domain.CenterAccessRequest) error
	FindRequestByCenterAndRUT(ctx context.Context, centerID primitive.ObjectID, rut string) (*domain.CenterAccessRequest, error)
	FindRequestByID(ctx context.Context, id primitive.ObjectID) (*domain.CenterAccessRequest, error)
	ListRequests(ctx context.Context, filter CenterAccessRequestFilter) ([]domain.CenterAccessRequest, int64, error)
	CountRequestsByStatus(ctx context.Context, centerID primitive.ObjectID) (map[string]int64, error)
	CreateVerification(ctx context.Context, verification *domain.CenterAccessVerification) error
	FindVerificationByID(ctx context.Context, id primitive.ObjectID) (*domain.CenterAccessVerification, error)
	IncrementVerificationAttempts(ctx context.Context, id primitive.ObjectID) error
	ConsumeVerification(ctx context.Context, id primitive.ObjectID, at time.Time) error
	CreateToken(ctx context.Context, token *domain.CenterAccessToken) error
	FindTokenByHash(ctx context.Context, hash string) (*domain.CenterAccessToken, error)
	DeleteTokensByRequest(ctx context.Context, requestID primitive.ObjectID) error
}

// CenterAccessRequestFilter filtra la bandeja del administrador.
type CenterAccessRequestFilter = domain.CenterAccessRequestFilter

// CenterAccessLimiter comparte los límites de envío entre instancias.
type CenterAccessLimiter interface {
	AllowAccessRequest(ctx context.Context, ip, key string) (VerificationLimit, error)
}

// CenterAccessEmail reúne los datos comunes de los correos de acceso.
type CenterAccessEmail struct {
	To         string
	Name       string
	CenterName string
	TenantURL  string
}

// CenterAccessDecisionEmail agrega el motivo y el contacto del centro.
type CenterAccessDecisionEmail struct {
	CenterAccessEmail
	Reason       string
	ContactEmail string
}

// CenterAccessMailer envía los correos del flujo de acceso privado. Se separa
// de Mailer para no obligar a los dobles de prueba existentes a implementarlo.
type CenterAccessMailer interface {
	SendAccessRequestReceived(ctx context.Context, data CenterAccessEmail) error
	SendAccessRequestApproved(ctx context.Context, data CenterAccessDecisionEmail) error
	SendAccessRequestRejected(ctx context.Context, data CenterAccessDecisionEmail) error
	SendAccessVerificationCode(ctx context.Context, to, name, centerName, code string, expiresMinutes int) error
}

// CenterAccessRequestInput es el cuerpo del formulario público.
type CenterAccessRequestInput struct {
	RUT     string
	Email   string
	Name    string
	Answers map[string]string
}

// AccessVerificationRequestResult es la respuesta del primer paso de
// verificación.
type AccessVerificationRequestResult struct {
	VerificationID string    `json:"verification_id"`
	ExpiresAt      time.Time `json:"expires_at"`
	ResendIn       int       `json:"resend_in_seconds"`
	MaskedEmail    string    `json:"masked_email"`
}

// AccessVerificationConfirmResult entrega el token de 30 días.
type AccessVerificationConfirmResult struct {
	AccessToken string    `json:"access_token"`
	ExpiresAt   time.Time `json:"expires_at"`
	Applicant   struct {
		Name  string `json:"name,omitempty"`
		Email string `json:"email"`
		RUT   string `json:"rut"`
	} `json:"applicant"`
}

// CenterAccessUseCase implementa el ciclo completo de acceso a centros
// privados: formulario, solicitudes, aprobación, verificación y validación del
// token que exige el checkout.
type CenterAccessUseCase struct {
	repo       CenterAccessRepository
	centerRepo SportCenterRepository
	mailer     CenterAccessMailer
	limiter    *verificationLimiter
	shared     CenterAccessLimiter
	now        func() time.Time
}

func NewCenterAccessUseCase(repo CenterAccessRepository, centerRepo SportCenterRepository, mailer CenterAccessMailer) *CenterAccessUseCase {
	return &CenterAccessUseCase{
		repo:       repo,
		centerRepo: centerRepo,
		mailer:     mailer,
		limiter:    &verificationLimiter{entries: map[string]*verificationLimitEntry{}, ips: map[string]*verificationLimitEntry{}},
		now:        time.Now,
	}
}

// SetLimiter usa un limitador compartido (Redis). Si falla, el servicio vuelve
// al limitador en memoria de la instancia.
func (uc *CenterAccessUseCase) SetLimiter(l CenterAccessLimiter) { uc.shared = l }

func (uc *CenterAccessUseCase) checkLimit(ctx context.Context, ip, key string) VerificationLimit {
	if uc.shared != nil {
		if res, err := uc.shared.AllowAccessRequest(ctx, ip, key); err == nil {
			return res
		}
	}
	return uc.limiter.AllowRequest(ip, key, uc.now())
}

func (uc *CenterAccessUseCase) center(ctx context.Context, centerID primitive.ObjectID) (*domain.SportCenter, error) {
	center, err := uc.centerRepo.FindByID(ctx, centerID)
	if err != nil || center == nil {
		return nil, domain.NewNotFoundError("centro deportivo no encontrado")
	}
	return center, nil
}

// GetForm devuelve la configuración vigente. Si el centro nunca configuró un
// formulario, entrega los campos por defecto.
func (uc *CenterAccessUseCase) GetForm(ctx context.Context, centerID primitive.ObjectID) (*domain.CenterAccessForm, error) {
	center, err := uc.center(ctx, centerID)
	if err != nil {
		return nil, err
	}
	if center.AccessForm == nil {
		return domain.DefaultCenterAccessForm(), nil
	}
	form := *center.AccessForm
	form.Normalize()
	return &form, nil
}

// UpdateForm valida y guarda el formulario del centro.
func (uc *CenterAccessUseCase) UpdateForm(ctx context.Context, centerID primitive.ObjectID, actor string, input *domain.CenterAccessForm) (*domain.CenterAccessForm, error) {
	center, err := uc.center(ctx, centerID)
	if err != nil {
		return nil, err
	}
	form, err := normalizeAccessForm(input)
	if err != nil {
		return nil, err
	}
	form.UpdatedAt = uc.now()
	form.UpdatedBy = actor

	if updater, ok := uc.centerRepo.(interface {
		UpdateAccessForm(ctx context.Context, id primitive.ObjectID, form *domain.CenterAccessForm) error
	}); ok {
		if err := updater.UpdateAccessForm(ctx, centerID, form); err != nil {
			return nil, err
		}
		return form, nil
	}

	center.AccessForm = form
	center.UpdatedAt = uc.now()
	if err := uc.centerRepo.Update(ctx, center); err != nil {
		return nil, err
	}
	return form, nil
}

// SubmitRequest valida el formulario público y crea (o reabre) la solicitud.
func (uc *CenterAccessUseCase) SubmitRequest(ctx context.Context, centerID primitive.ObjectID, input CenterAccessRequestInput, ip string) (*domain.CenterAccessRequest, error) {
	center, err := uc.center(ctx, centerID)
	if err != nil {
		return nil, err
	}
	if !center.IsPrivate {
		return nil, domain.NewBadRequestError("este centro no es privado")
	}
	form := center.AccessForm
	if form == nil || !form.Enabled {
		return nil, domain.NewBadRequestError("el centro no está recibiendo solicitudes de acceso")
	}
	form.Normalize()

	rut := normalizeAccessRUT(input.RUT)
	email := normalizeAccessEmail(input.Email)
	if !validateRUT(rut) {
		return nil, domain.NewBadRequestError("RUT inválido")
	}
	if !isValidEmail(email) {
		return nil, domain.NewBadRequestError("correo inválido")
	}
	answers, err := buildAccessAnswers(form, rut, email, input.Answers)
	if err != nil {
		return nil, err
	}
	if limit := uc.checkLimit(ctx, ip, "form:"+rut); limit != VerificationLimitOK {
		return nil, &domain.AppError{Message: "demasiados intentos, intenta más tarde", StatusCode: 429}
	}

	now := uc.now()
	existing, err := uc.repo.FindRequestByCenterAndRUT(ctx, centerID, rut)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		switch existing.Status {
		case domain.CenterAccessStatusPending:
			return nil, domain.NewConflictError("ya tienes una solicitud pendiente en este centro")
		case domain.CenterAccessStatusApproved:
			return nil, domain.NewConflictError("ya tienes acceso aprobado; verifica tu RUT para reservar")
		case domain.CenterAccessStatusRevoked:
			return nil, domain.NewConflictError("tu acceso fue revocado; contacta al centro")
		case domain.CenterAccessStatusRejected:
			if now.Sub(existing.UpdatedAt) < accessResubmitAfter {
				return nil, domain.NewConflictError("tu solicitud fue rechazada; puedes volver a postular después de 24 horas")
			}
		}
	}

	request := existing
	if request == nil {
		request = &domain.CenterAccessRequest{
			SportCenterID: centerID,
			RUT:           rut,
			CreatedAt:     now,
		}
	}
	request.Email = email
	request.Name = strings.TrimSpace(input.Name)
	request.Answers = answers
	request.Status = domain.CenterAccessStatusPending
	request.Reason = ""
	request.Source = domain.CenterAccessSourceForm
	request.DecidedAt = nil
	request.DecidedBy = ""
	request.DecidedByEmail = ""
	request.UpdatedAt = now

	if existing == nil {
		if err := uc.repo.CreateRequest(ctx, request); err != nil {
			return nil, err
		}
	} else if err := uc.repo.UpdateRequest(ctx, request); err != nil {
		return nil, err
	}

	uc.sendReceivedEmail(center, request)
	return request, nil
}

// CreateManualRequest registra a una persona ya aprobada por el administrador.
func (uc *CenterAccessUseCase) CreateManualRequest(ctx context.Context, centerID primitive.ObjectID, actorID, actorEmail string, input CenterAccessRequestInput) (*domain.CenterAccessRequest, error) {
	center, err := uc.center(ctx, centerID)
	if err != nil {
		return nil, err
	}
	rut := normalizeAccessRUT(input.RUT)
	email := normalizeAccessEmail(input.Email)
	if !validateRUT(rut) {
		return nil, domain.NewBadRequestError("RUT inválido")
	}
	if !isValidEmail(email) {
		return nil, domain.NewBadRequestError("correo inválido")
	}

	now := uc.now()
	request, err := uc.repo.FindRequestByCenterAndRUT(ctx, centerID, rut)
	if err != nil {
		return nil, err
	}
	if request != nil && request.Status == domain.CenterAccessStatusApproved {
		return nil, domain.NewConflictError("ya existe un acceso aprobado con ese RUT")
	}
	if request == nil {
		request = &domain.CenterAccessRequest{
			SportCenterID: centerID,
			RUT:           rut,
			CreatedAt:     now,
		}
	}
	request.Email = email
	request.Name = strings.TrimSpace(input.Name)
	request.Answers = []domain.CenterAccessAnswer{
		{Key: domain.CenterAccessFieldRUTKey, Label: "RUT", Value: rut},
		{Key: domain.CenterAccessFieldEmailKey, Label: "Correo electrónico", Value: email},
	}
	if strings.TrimSpace(request.Name) != "" {
		request.Answers = append(request.Answers, domain.CenterAccessAnswer{Key: "name", Label: "Nombre", Value: request.Name})
	}
	request.Status = domain.CenterAccessStatusApproved
	request.Reason = ""
	request.Source = domain.CenterAccessSourceAdmin
	request.DecidedAt = &now
	request.DecidedBy = actorID
	request.DecidedByEmail = actorEmail
	request.UpdatedAt = now

	if request.ID.IsZero() {
		if err := uc.repo.CreateRequest(ctx, request); err != nil {
			return nil, err
		}
	} else if err := uc.repo.UpdateRequest(ctx, request); err != nil {
		return nil, err
	}

	uc.sendApprovedEmail(center, request)
	return request, nil
}

// ListRequests lista la bandeja del centro.
func (uc *CenterAccessUseCase) ListRequests(ctx context.Context, filter CenterAccessRequestFilter) ([]domain.CenterAccessRequest, int64, error) {
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.Limit < 1 || filter.Limit > 100 {
		filter.Limit = 20
	}
	return uc.repo.ListRequests(ctx, filter)
}

// Counts devuelve los contadores por estado del centro.
func (uc *CenterAccessUseCase) Counts(ctx context.Context, centerID primitive.ObjectID) (map[string]int64, error) {
	return uc.repo.CountRequestsByStatus(ctx, centerID)
}

// GetRequest devuelve una solicitud del centro.
func (uc *CenterAccessUseCase) GetRequest(ctx context.Context, centerID, id primitive.ObjectID) (*domain.CenterAccessRequest, error) {
	request, err := uc.repo.FindRequestByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if request == nil || request.SportCenterID != centerID {
		return nil, domain.NewNotFoundError("solicitud no encontrada")
	}
	return request, nil
}

// SetRequestStatus aprueba, rechaza o revoca una solicitud.
func (uc *CenterAccessUseCase) SetRequestStatus(ctx context.Context, centerID, id primitive.ObjectID, status, reason, actorID, actorEmail string) (*domain.CenterAccessRequest, error) {
	request, err := uc.GetRequest(ctx, centerID, id)
	if err != nil {
		return nil, err
	}
	status = strings.TrimSpace(strings.ToLower(status))
	reason = strings.TrimSpace(reason)
	if !validAccessTransition(request.Status, status) {
		return nil, domain.NewBadRequestError("no se puede cambiar la solicitud a ese estado")
	}
	if (status == domain.CenterAccessStatusRejected || status == domain.CenterAccessStatusRevoked) && len(reason) < 5 {
		return nil, domain.NewBadRequestError("el motivo es obligatorio (mínimo 5 caracteres)")
	}

	now := uc.now()
	request.Status = status
	request.Reason = reason
	request.DecidedAt = &now
	request.DecidedBy = actorID
	request.DecidedByEmail = actorEmail
	request.UpdatedAt = now
	if err := uc.repo.UpdateRequest(ctx, request); err != nil {
		return nil, err
	}

	if status == domain.CenterAccessStatusRevoked {
		_ = uc.repo.DeleteTokensByRequest(ctx, request.ID)
	}
	center, _ := uc.centerRepo.FindByID(ctx, centerID)
	switch status {
	case domain.CenterAccessStatusApproved:
		uc.sendApprovedEmail(center, request)
	case domain.CenterAccessStatusRejected:
		uc.sendRejectedEmail(center, request)
	}
	return request, nil
}

func validAccessTransition(from, to string) bool {
	switch to {
	case domain.CenterAccessStatusApproved:
		return from == domain.CenterAccessStatusPending || from == domain.CenterAccessStatusRejected || from == domain.CenterAccessStatusRevoked
	case domain.CenterAccessStatusRejected:
		return from == domain.CenterAccessStatusPending || from == domain.CenterAccessStatusApproved
	case domain.CenterAccessStatusRevoked:
		return from == domain.CenterAccessStatusApproved
	}
	return false
}

// RequestVerification envía un código al correo de una solicitud aprobada.
func (uc *CenterAccessUseCase) RequestVerification(ctx context.Context, centerID primitive.ObjectID, rut, ip string) (*AccessVerificationRequestResult, error) {
	rut = normalizeAccessRUT(rut)
	if !validateRUT(rut) {
		return nil, domain.NewBadRequestError("RUT inválido")
	}
	switch uc.checkLimit(ctx, ip, "verify:"+rut) {
	case VerificationLimitIP, VerificationLimitDocument:
		return nil, &domain.AppError{Message: "demasiados intentos, intenta más tarde", StatusCode: 429}
	case VerificationLimitCooldown:
		return nil, &domain.AppError{Message: "espera un momento antes de solicitar otro código", StatusCode: 429}
	}

	request, err := uc.repo.FindRequestByCenterAndRUT(ctx, centerID, rut)
	if err != nil || request == nil || request.Status != domain.CenterAccessStatusApproved {
		return nil, domain.NewNotFoundError("no encontramos una solicitud aprobada con ese RUT")
	}
	if !isValidEmail(request.Email) {
		return nil, &domain.AppError{Message: "tu solicitud no tiene un correo válido; contacta al centro", StatusCode: 422}
	}

	code, err := generateNumericCode(6)
	if err != nil {
		return nil, err
	}
	now := uc.now()
	verification := &domain.CenterAccessVerification{
		SportCenterID: centerID,
		RequestID:     request.ID,
		CodeHash:      hashVerificationCode(request.ID, code),
		ExpiresAt:     now.Add(accessVerificationTTL),
		RequestIP:     ip,
		CreatedAt:     now,
	}
	if err := uc.repo.CreateVerification(ctx, verification); err != nil {
		return nil, err
	}

	if uc.mailer != nil {
		center, _ := uc.centerRepo.FindByID(ctx, centerID)
		centerName := "ReservaloYA"
		if center != nil && center.Name != "" {
			centerName = center.Name
		}
		name := request.Name
		if name == "" {
			name = "solicitante"
		}
		go func() {
			_ = uc.mailer.SendAccessVerificationCode(context.Background(), request.Email, name, centerName, code, int(accessVerificationTTL.Minutes()))
		}()
	}

	return &AccessVerificationRequestResult{
		VerificationID: verification.ID.Hex(),
		ExpiresAt:      verification.ExpiresAt,
		ResendIn:       int(accessVerificationCooldown.Seconds()),
		MaskedEmail:    maskEmail(request.Email),
	}, nil
}

// ConfirmVerification valida el código y emite el token de 30 días.
func (uc *CenterAccessUseCase) ConfirmVerification(ctx context.Context, verificationID, code string) (*AccessVerificationConfirmResult, error) {
	id, err := primitive.ObjectIDFromHex(strings.TrimSpace(verificationID))
	if err != nil {
		return nil, domain.NewBadRequestError("código inválido")
	}
	verification, err := uc.repo.FindVerificationByID(ctx, id)
	if err != nil || verification == nil {
		return nil, domain.NewBadRequestError("código inválido")
	}
	now := uc.now()
	if verification.ConsumedAt != nil || now.After(verification.ExpiresAt) || verification.Attempts >= accessVerificationAttempts {
		return nil, domain.NewBadRequestError("el código expiró, solicita uno nuevo")
	}
	expected := hashVerificationCode(verification.RequestID, strings.TrimSpace(code))
	if subtle.ConstantTimeCompare([]byte(expected), []byte(verification.CodeHash)) != 1 {
		_ = uc.repo.IncrementVerificationAttempts(ctx, verification.ID)
		return nil, domain.NewBadRequestError("código incorrecto")
	}
	if err := uc.repo.ConsumeVerification(ctx, verification.ID, now); err != nil {
		return nil, err
	}

	request, err := uc.repo.FindRequestByID(ctx, verification.RequestID)
	if err != nil || request == nil || request.Status != domain.CenterAccessStatusApproved {
		return nil, domain.NewBadRequestError("tu acceso no está vigente")
	}

	token, err := generateRandomToken()
	if err != nil {
		return nil, err
	}
	accessToken := &domain.CenterAccessToken{
		SportCenterID: verification.SportCenterID,
		RequestID:     request.ID,
		TokenHash:     hashToken(token),
		ExpiresAt:     now.Add(accessTokenTTL),
		CreatedAt:     now,
	}
	if err := uc.repo.CreateToken(ctx, accessToken); err != nil {
		return nil, err
	}
	if request.VerifiedAt == nil {
		request.VerifiedAt = &now
	}
	request.LastVerifiedAt = &now
	request.UpdatedAt = now
	_ = uc.repo.UpdateRequest(ctx, request)

	return &AccessVerificationConfirmResult{
		AccessToken: token,
		ExpiresAt:   accessToken.ExpiresAt,
		Applicant: struct {
			Name  string `json:"name,omitempty"`
			Email string `json:"email"`
			RUT   string `json:"rut"`
		}{Name: request.Name, Email: request.Email, RUT: request.RUT},
	}, nil
}

// ValidateToken comprueba que el token pertenezca al centro y que la solicitud
// siga aprobada. Si no, devuelve un error 403 con código estable.
func (uc *CenterAccessUseCase) ValidateToken(ctx context.Context, centerID primitive.ObjectID, token string) (*domain.CenterAccessRequest, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, domain.NewAccessRequiredError()
	}
	stored, err := uc.repo.FindTokenByHash(ctx, hashToken(token))
	if err != nil || stored == nil || stored.SportCenterID != centerID || uc.now().After(stored.ExpiresAt) {
		return nil, domain.NewAccessInvalidError()
	}
	request, err := uc.repo.FindRequestByID(ctx, stored.RequestID)
	if err != nil || request == nil || request.SportCenterID != centerID || request.Status != domain.CenterAccessStatusApproved {
		return nil, domain.NewAccessInvalidError()
	}
	return request, nil
}

// EnsureBookingAccess es la puerta que usan los flujos públicos de reserva.
func (uc *CenterAccessUseCase) EnsureBookingAccess(ctx context.Context, center *domain.SportCenter, token string, adminSource bool) error {
	if adminSource || center == nil || !center.IsPrivate {
		return nil
	}
	if strings.TrimSpace(token) == "" {
		return domain.NewAccessRequiredError()
	}
	_, err := uc.ValidateToken(ctx, center.ID, token)
	return err
}

// --- helpers ---

var accessFieldKeyRegex = regexp.MustCompile(`^[a-z0-9_]{1,40}$`)

func normalizeAccessForm(input *domain.CenterAccessForm) (*domain.CenterAccessForm, error) {
	if input == nil {
		return domain.DefaultCenterAccessForm(), nil
	}
	form := &domain.CenterAccessForm{
		Enabled:     input.Enabled,
		Title:       strings.TrimSpace(input.Title),
		Description: strings.TrimSpace(input.Description),
		Fields:      []domain.CenterAccessField{},
	}
	if form.Title == "" {
		form.Title = "Solicitud de acceso"
	}
	if len(form.Title) > 120 {
		return nil, domain.NewBadRequestError("el título no puede superar 120 caracteres")
	}
	if len(form.Description) > 1000 {
		return nil, domain.NewBadRequestError("la descripción no puede superar 1000 caracteres")
	}

	used := map[string]bool{domain.CenterAccessFieldRUTKey: true, domain.CenterAccessFieldEmailKey: true}
	system := domain.DefaultCenterAccessForm().Fields
	form.Fields = append(form.Fields, system...)

	custom := input.Fields
	if len(custom) > accessMaxCustomFields {
		return nil, domain.NewBadRequestError(fmt.Sprintf("el formulario admite hasta %d campos personalizados", accessMaxCustomFields))
	}
	for i, field := range custom {
		if field.Key == domain.CenterAccessFieldRUTKey || field.Key == domain.CenterAccessFieldEmailKey {
			continue
		}
		label := strings.TrimSpace(field.Label)
		if label == "" {
			return nil, domain.NewBadRequestError("todos los campos deben tener una etiqueta")
		}
		if len(label) > 80 {
			return nil, domain.NewBadRequestError("las etiquetas no pueden superar 80 caracteres")
		}
		key := strings.TrimSpace(strings.ToLower(field.Key))
		if key == "" {
			key = accessFieldKeyFromLabel(label, i)
		}
		if !accessFieldKeyRegex.MatchString(key) {
			return nil, domain.NewBadRequestError("la clave del campo solo admite minúsculas, números y guión bajo")
		}
		if used[key] {
			return nil, domain.NewBadRequestError("hay una clave de campo repetida: " + key)
		}
		used[key] = true

		fieldType := strings.TrimSpace(strings.ToLower(field.Type))
		switch fieldType {
		case domain.CenterAccessFieldText, domain.CenterAccessFieldTextarea, domain.CenterAccessFieldEmail, domain.CenterAccessFieldSelect, domain.CenterAccessFieldNumber:
		default:
			return nil, domain.NewBadRequestError("tipo de campo inválido: " + fieldType)
		}
		options := make([]string, 0, len(field.Options))
		if fieldType == domain.CenterAccessFieldSelect {
			seen := map[string]bool{}
			for _, option := range field.Options {
				option = strings.TrimSpace(option)
				if option == "" || seen[option] {
					continue
				}
				if len(option) > 80 {
					return nil, domain.NewBadRequestError("las opciones no pueden superar 80 caracteres")
				}
				seen[option] = true
				options = append(options, option)
			}
			if len(options) == 0 {
				return nil, domain.NewBadRequestError("los campos de lista necesitan al menos una opción")
			}
			if len(options) > 50 {
				return nil, domain.NewBadRequestError("los campos de lista admiten hasta 50 opciones")
			}
		}
		form.Fields = append(form.Fields, domain.CenterAccessField{
			Key:         key,
			Label:       label,
			Type:        fieldType,
			Required:    field.Required,
			Locked:      false,
			Options:     options,
			Placeholder: strings.TrimSpace(field.Placeholder),
			Help:        strings.TrimSpace(field.Help),
		})
	}
	return form, nil
}

func accessFieldKeyFromLabel(label string, index int) string {
	key := strings.ReplaceAll(generateSlug(label), "-", "_")
	key = strings.Trim(key, "_")
	if key == "" {
		key = fmt.Sprintf("field_%d", index+1)
	}
	if len(key) > 40 {
		key = key[:40]
	}
	return key
}

func buildAccessAnswers(form *domain.CenterAccessForm, rut, email string, input map[string]string) ([]domain.CenterAccessAnswer, error) {
	answers := make([]domain.CenterAccessAnswer, 0, len(form.Fields))
	for _, field := range form.Fields {
		var value string
		switch field.Key {
		case domain.CenterAccessFieldRUTKey:
			value = rut
		case domain.CenterAccessFieldEmailKey:
			value = email
		default:
			value = strings.TrimSpace(input[field.Key])
		}

		if field.Required && value == "" {
			return nil, domain.NewBadRequestError("el campo \"" + field.Label + "\" es obligatorio")
		}
		switch field.Type {
		case domain.CenterAccessFieldEmail:
			if value != "" && !isValidEmail(value) {
				return nil, domain.NewBadRequestError("el campo \"" + field.Label + "\" no es un correo válido")
			}
		case domain.CenterAccessFieldNumber:
			if value != "" {
				if _, err := strconv.ParseFloat(value, 64); err != nil {
					return nil, domain.NewBadRequestError("el campo \"" + field.Label + "\" debe ser numérico")
				}
			}
		case domain.CenterAccessFieldSelect:
			if value != "" && !containsString(field.Options, value) {
				return nil, domain.NewBadRequestError("el valor de \"" + field.Label + "\" no está entre las opciones")
			}
		}
		if len(value) > 4000 {
			return nil, domain.NewBadRequestError("el campo \"" + field.Label + "\" es demasiado largo")
		}
		if field.Type != domain.CenterAccessFieldTextarea && len(value) > 500 {
			return nil, domain.NewBadRequestError("el campo \"" + field.Label + "\" no puede superar 500 caracteres")
		}
		answers = append(answers, domain.CenterAccessAnswer{Key: field.Key, Label: field.Label, Value: value})
	}
	return answers, nil
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func normalizeAccessRUT(rut string) string {
	return strings.ToUpper(cleanRUT(rut))
}

func normalizeAccessEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func accessTenantURL(slug string) string {
	domainName := strings.TrimSpace(os.Getenv("PLATFORM_DOMAIN"))
	if domainName == "" {
		domainName = "reservaloya.cl"
	}
	return fmt.Sprintf("https://%s.%s", strings.TrimSpace(slug), domainName)
}

func (uc *CenterAccessUseCase) sendReceivedEmail(center *domain.SportCenter, request *domain.CenterAccessRequest) {
	if uc.mailer == nil || center == nil {
		return
	}
	data := CenterAccessEmail{
		To:         request.Email,
		Name:       request.Name,
		CenterName: center.Name,
		TenantURL:  accessTenantURL(center.Slug),
	}
	go func() { _ = uc.mailer.SendAccessRequestReceived(context.Background(), data) }()
}

func (uc *CenterAccessUseCase) sendApprovedEmail(center *domain.SportCenter, request *domain.CenterAccessRequest) {
	if uc.mailer == nil || center == nil || request == nil {
		return
	}
	data := CenterAccessDecisionEmail{
		CenterAccessEmail: CenterAccessEmail{
			To:         request.Email,
			Name:       request.Name,
			CenterName: center.Name,
			TenantURL:  accessTenantURL(center.Slug),
		},
		ContactEmail: center.Contact.Email,
	}
	go func() { _ = uc.mailer.SendAccessRequestApproved(context.Background(), data) }()
}

func (uc *CenterAccessUseCase) sendRejectedEmail(center *domain.SportCenter, request *domain.CenterAccessRequest) {
	if uc.mailer == nil || center == nil || request == nil {
		return
	}
	data := CenterAccessDecisionEmail{
		CenterAccessEmail: CenterAccessEmail{
			To:         request.Email,
			Name:       request.Name,
			CenterName: center.Name,
			TenantURL:  accessTenantURL(center.Slug),
		},
		Reason:       request.Reason,
		ContactEmail: center.Contact.Email,
	}
	go func() { _ = uc.mailer.SendAccessRequestRejected(context.Background(), data) }()
}
