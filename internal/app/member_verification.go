package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

const (
	memberVerificationTTL      = 10 * time.Minute
	memberVerificationAttempts = 5
	memberVerificationCooldown = 60 * time.Second
	memberVerificationWindow   = 15 * time.Minute
	memberVerificationMaxSends = 5
	memberVerificationIPMax    = 10
	memberTokenTTL             = 30 * time.Minute
)

// MemberVerificationMailer envía el código de confirmación al correo del
// socio. Se mantiene separado de Mailer para no romper implementaciones
// existentes (por ejemplo mocks de tests).
type MemberVerificationMailer interface {
	SendMemberVerificationCode(ctx context.Context, to, name, centerName, code string, expiresMinutes int) error
}

// VerificationLimit es el resultado de evaluar los límites de solicitud.
type VerificationLimit int

const (
	VerificationLimitOK VerificationLimit = iota
	VerificationLimitIP
	VerificationLimitCooldown
	VerificationLimitDocument
)

// MemberVerificationLimiter lleva la cuenta de intentos de solicitud de
// código. La implementación en Redis lo comparte entre instancias.
type MemberVerificationLimiter interface {
	AllowVerificationRequest(ctx context.Context, ip, document string) (VerificationLimit, error)
}

// MemberVerificationService gestiona el flujo RUT + correo + código.
type MemberVerificationService struct {
	repo       MembershipRepository
	centerRepo SportCenterRepository
	mailer     MemberVerificationMailer
	limiter    *verificationLimiter
	shared     MemberVerificationLimiter
}

// SetLimiter usa un limitador compartido (Redis). Si falla, el servicio
// vuelve al limitador en memoria de la instancia.
func (s *MemberVerificationService) SetLimiter(l MemberVerificationLimiter) { s.shared = l }

func (s *MemberVerificationService) checkLimit(ctx context.Context, ip, document string, now time.Time) VerificationLimit {
	if s.shared != nil {
		if res, err := s.shared.AllowVerificationRequest(ctx, ip, document); err == nil {
			return res
		}
	}
	return s.limiter.AllowRequest(ip, document, now)
}

// AllowRequest aplica los límites en memoria (por IP y por RUT+IP).
func (l *verificationLimiter) AllowRequest(ip, document string, now time.Time) VerificationLimit {
	if !l.allowIP(ip, now) {
		return VerificationLimitIP
	}
	if !l.allow(document+"|"+ip, now) {
		return VerificationLimitCooldown
	}
	return VerificationLimitOK
}

type verificationLimiter struct {
	mu      sync.Mutex
	entries map[string]*verificationLimitEntry
	ips     map[string]*verificationLimitEntry
}

type verificationLimitEntry struct {
	lastSent time.Time
	window   time.Time
	count    int
}

func NewMemberVerificationService(repo MembershipRepository, centerRepo SportCenterRepository, mailer MemberVerificationMailer) *MemberVerificationService {
	return &MemberVerificationService{
		repo:       repo,
		centerRepo: centerRepo,
		mailer:     mailer,
		limiter:    &verificationLimiter{entries: map[string]*verificationLimitEntry{}, ips: map[string]*verificationLimitEntry{}},
	}
}

// VerificationRequestResult es la respuesta del primer paso. Incluye el
// correo ofuscado al que se envió el código.
type VerificationRequestResult struct {
	VerificationID string    `json:"verification_id"`
	ExpiresAt      time.Time `json:"expires_at"`
	ResendIn       int       `json:"resend_in_seconds"`
	MaskedEmail    string    `json:"masked_email"`
}

// RequestVerification busca al socio por RUT y envía un código de 6 dígitos
// a su correo registrado. No recibe correo: siempre se usa el configurado.
func (s *MemberVerificationService) RequestVerification(ctx context.Context, centerID primitive.ObjectID, document, ip string) (*VerificationRequestResult, error) {
	now := time.Now()
	document = normalizeDocument(document)

	if !validateRUT(document) {
		return nil, domain.NewBadRequestError("RUT inválido")
	}
	switch s.checkLimit(ctx, ip, document, now) {
	case VerificationLimitIP, VerificationLimitDocument:
		return nil, &domain.AppError{Message: "demasiados intentos, intenta más tarde", StatusCode: 429}
	case VerificationLimitCooldown:
		return nil, &domain.AppError{Message: "espera un momento antes de solicitar otro código", StatusCode: 429}
	}

	member, err := s.repo.FindMemberByDocument(ctx, centerID, document)
	if err != nil || member == nil || !member.IsUsable(now) {
		return nil, domain.NewNotFoundError("No encontramos un socio activo con ese RUT")
	}
	if !isValidEmail(normalizeEmail(member.Email)) {
		return nil, &domain.AppError{Message: "Tu cuenta no tiene correo registrado, contacta al centro", StatusCode: 422}
	}

	code, err := generateNumericCode(6)
	if err != nil {
		return nil, err
	}

	verificationID := primitive.NewObjectID()
	verification := &domain.MemberVerification{
		ID:            verificationID,
		SportCenterID: centerID,
		MemberID:      member.ID,
		CodeHash:      hashVerificationCode(verificationID, code),
		ExpiresAt:     now.Add(memberVerificationTTL),
		RequestIP:     ip,
		CreatedAt:     now,
	}
	if err := s.repo.CreateVerification(ctx, verification); err != nil {
		return nil, err
	}

	result := &VerificationRequestResult{
		VerificationID: verificationID.Hex(),
		ExpiresAt:      verification.ExpiresAt,
		ResendIn:       int(memberVerificationCooldown.Seconds()),
		MaskedEmail:    maskEmail(member.Email),
	}

	if s.mailer != nil {
		center, _ := s.centerRepo.FindByID(ctx, centerID)
		centerName := "ReservaloYA"
		if center != nil && center.Name != "" {
			centerName = center.Name
		}
		// El envío es best-effort: no bloqueamos el checkout por fallos de
		// infraestructura del correo.
		go func() {
			_ = s.mailer.SendMemberVerificationCode(context.Background(), member.Email, member.Name, centerName, code, int(memberVerificationTTL.Minutes()))
		}()
	}

	return result, nil
}

// maskEmail ofusca un correo: "hector@gmail.com" -> "h*****@g****.com".
func maskEmail(email string) string {
	email = strings.TrimSpace(email)
	at := strings.LastIndex(email, "@")
	if at <= 0 || at == len(email)-1 {
		return ""
	}
	local, host := email[:at], email[at+1:]
	domainName, tld := host, ""
	if dot := strings.LastIndex(host, "."); dot > 0 {
		domainName, tld = host[:dot], host[dot:]
	}
	return maskPart(local) + "@" + maskPart(domainName) + tld
}

func maskPart(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return ""
	}
	n := len(r) - 1
	if n < 1 {
		n = 1
	}
	return string(r[0]) + strings.Repeat("*", n)
}

// VerificationConfirmResult contiene el token temporal que acredita al socio.
type VerificationConfirmResult struct {
	MemberToken string              `json:"member_token"`
	ExpiresAt   time.Time           `json:"expires_at"`
	Member      *domain.QuoteMember `json:"member"`
}

// ConfirmVerification valida el código y emite un token de socio.
func (s *MemberVerificationService) ConfirmVerification(ctx context.Context, verificationID string, code string) (*VerificationConfirmResult, error) {
	id, err := primitive.ObjectIDFromHex(strings.TrimSpace(verificationID))
	if err != nil {
		return nil, domain.NewBadRequestError("código inválido")
	}
	verification, err := s.repo.FindVerificationByID(ctx, id)
	if err != nil || verification == nil {
		return nil, domain.NewBadRequestError("código inválido")
	}
	now := time.Now()
	if verification.ConsumedAt != nil || now.After(verification.ExpiresAt) || verification.Attempts >= memberVerificationAttempts {
		return nil, domain.NewBadRequestError("el código expiró, solicita uno nuevo")
	}

	expected := hashVerificationCode(verification.ID, strings.TrimSpace(code))
	if subtle.ConstantTimeCompare([]byte(expected), []byte(verification.CodeHash)) != 1 {
		_ = s.repo.IncrementVerificationAttempts(ctx, verification.ID)
		return nil, domain.NewBadRequestError("código incorrecto")
	}

	if err := s.repo.ConsumeVerification(ctx, verification.ID, now); err != nil {
		return nil, err
	}

	member, err := s.repo.FindMemberByID(ctx, verification.MemberID)
	if err != nil || member == nil || !member.IsUsable(now) {
		return nil, domain.NewBadRequestError("tu membresía no está vigente")
	}
	plan, err := s.repo.FindPlanByID(ctx, member.PlanID)
	if err != nil || plan == nil || !plan.Active {
		return nil, domain.NewBadRequestError("el plan de socio no está activo")
	}

	token, err := generateRandomToken()
	if err != nil {
		return nil, err
	}
	memberToken := &domain.MemberToken{
		SportCenterID: verification.SportCenterID,
		MemberID:      member.ID,
		PlanID:        plan.ID,
		TokenHash:     hashToken(token),
		ExpiresAt:     now.Add(memberTokenTTL),
		CreatedAt:     now,
	}
	if err := s.repo.CreateMemberToken(ctx, memberToken); err != nil {
		return nil, err
	}

	return &VerificationConfirmResult{
		MemberToken: token,
		ExpiresAt:   memberToken.ExpiresAt,
		Member: &domain.QuoteMember{
			ID:       member.ID.Hex(),
			Name:     member.Name,
			PlanID:   plan.ID.Hex(),
			PlanName: plan.Name,
		},
	}, nil
}

func (l *verificationLimiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	entry, ok := l.entries[key]
	if !ok {
		l.entries[key] = &verificationLimitEntry{lastSent: now, window: now, count: 1}
		return true
	}
	if now.Sub(entry.lastSent) < memberVerificationCooldown {
		return false
	}
	if now.Sub(entry.window) > memberVerificationWindow {
		entry.window = now
		entry.count = 0
	}
	if entry.count >= memberVerificationMaxSends {
		return false
	}
	entry.lastSent = now
	entry.count++
	return true
}

// allowIP limita los intentos por IP sin importar el RUT, para frenar la
// enumeración de documentos.
func (l *verificationLimiter) allowIP(ip string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	entry, ok := l.ips[ip]
	if !ok || now.Sub(entry.window) > memberVerificationWindow {
		l.ips[ip] = &verificationLimitEntry{window: now, count: 1}
		return true
	}
	if entry.count >= memberVerificationIPMax {
		return false
	}
	entry.count++
	return true
}

func normalizeDocument(document string) string {
	return strings.ToUpper(cleanRUT(document))
}

func hashVerificationCode(id primitive.ObjectID, code string) string {
	sum := sha256.Sum256([]byte(id.Hex() + ":" + code))
	return hex.EncodeToString(sum[:])
}

func generateNumericCode(length int) (string, error) {
	if length <= 0 {
		return "", fmt.Errorf("invalid code length")
	}
	buf := make([]byte, length)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	digits := make([]byte, length)
	for i, b := range buf {
		digits[i] = '0' + (b % 10)
	}
	return string(digits), nil
}

func generateRandomToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
