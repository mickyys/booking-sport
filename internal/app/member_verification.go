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
	memberTokenTTL             = 30 * time.Minute
)

// MemberVerificationMailer envía el código de confirmación al correo del
// socio. Se mantiene separado de Mailer para no romper implementaciones
// existentes (por ejemplo mocks de tests).
type MemberVerificationMailer interface {
	SendMemberVerificationCode(ctx context.Context, to, name, centerName, code string, expiresMinutes int) error
}

// MemberVerificationService gestiona el flujo RUT + correo + código.
type MemberVerificationService struct {
	repo       MembershipRepository
	centerRepo SportCenterRepository
	mailer     MemberVerificationMailer
	limiter    *verificationLimiter
}

type verificationLimiter struct {
	mu      sync.Mutex
	entries map[string]*verificationLimitEntry
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
		limiter:    &verificationLimiter{entries: map[string]*verificationLimitEntry{}},
	}
}

// VerificationRequestResult es la respuesta del primer paso. Siempre es
// genérica para no revelar si el documento existe.
type VerificationRequestResult struct {
	VerificationID string    `json:"verification_id"`
	ExpiresAt      time.Time `json:"expires_at"`
	ResendIn       int       `json:"resend_in_seconds"`
}

// RequestVerification busca al socio y, si los datos coinciden, envía un
// código de 6 dígitos a su correo registrado.
func (s *MemberVerificationService) RequestVerification(ctx context.Context, centerID primitive.ObjectID, document, email, ip string) (*VerificationRequestResult, error) {
	now := time.Now()
	document = normalizeDocument(document)
	email = normalizeEmail(email)

	result := &VerificationRequestResult{
		VerificationID: primitive.NewObjectID().Hex(),
		ExpiresAt:      now.Add(memberVerificationTTL),
		ResendIn:       int(memberVerificationCooldown.Seconds()),
	}

	if !validateRUT(document) || !isValidEmail(email) {
		return result, nil
	}

	if !s.limiter.allow(document+"|"+ip, now) {
		return result, nil
	}

	member, err := s.repo.FindMemberByDocument(ctx, centerID, document)
	if err != nil || member == nil {
		return result, nil
	}
	if normalizeEmail(member.Email) != email || !member.IsUsable(now) {
		return result, nil
	}

	code, err := generateNumericCode(6)
	if err != nil {
		return result, err
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
		return result, err
	}

	result.VerificationID = verificationID.Hex()
	result.ExpiresAt = verification.ExpiresAt

	if s.mailer != nil {
		center, _ := s.centerRepo.FindByID(ctx, centerID)
		centerName := "ReservaloYA"
		if center != nil && center.Name != "" {
			centerName = center.Name
		}
		// El envío es best-effort: no revelamos fallos de infraestructura en
		// un endpoint público ni bloqueamos el checkout.
		go func() {
			_ = s.mailer.SendMemberVerificationCode(context.Background(), member.Email, member.Name, centerName, code, int(memberVerificationTTL.Minutes()))
		}()
	}

	return result, nil
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
