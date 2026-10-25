package app

import (
	"context"
	"testing"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type captureVerificationMailer struct {
	codes chan string
}

func newCaptureVerificationMailer() *captureVerificationMailer {
	return &captureVerificationMailer{codes: make(chan string, 4)}
}

func (m *captureVerificationMailer) SendMemberVerificationCode(_ context.Context, _, _, _, code string, _ int) error {
	m.codes <- code
	return nil
}

func newVerificationFixture(t *testing.T) (*MemberVerificationService, *pricingMembershipRepo, *captureVerificationMailer, *domain.Member) {
	t.Helper()
	centerID := primitive.NewObjectID()
	repo := newPricingMembershipRepo()
	plan := &domain.MembershipPlan{
		ID:            primitive.NewObjectID(),
		SportCenterID: centerID,
		Name:          "Socio Full",
		Discount:      domain.DiscountValue{Type: domain.DiscountTypePercent, Value: 10},
		Scope:         domain.DiscountScope{Mode: domain.DiscountScopeCategory, Categories: []domain.SpaceCategory{domain.SpaceCategoryCancha}},
		UsageLimit:    domain.UsageLimit{Period: domain.DiscountPeriodTotal},
		Active:        true,
	}
	repo.plans[plan.ID] = plan
	member := &domain.Member{
		ID:             primitive.NewObjectID(),
		SportCenterID:  centerID,
		PlanID:         plan.ID,
		Name:           "Ana Socia",
		Email:          "ana@example.com",
		DocumentNumber: "12345678-5",
		Status:         domain.MemberStatusActive,
	}
	repo.members[member.ID] = member

	mailer := newCaptureVerificationMailer()
	service := NewMemberVerificationService(repo, &mockCenterRepoForRecurring{}, mailer)
	return service, repo, mailer, member
}

func waitVerificationCode(t *testing.T, mailer *captureVerificationMailer) string {
	t.Helper()
	select {
	case code := <-mailer.codes:
		return code
	case <-time.After(2 * time.Second):
		t.Fatal("no se envió el código de verificación")
		return ""
	}
}

func TestMemberVerificationHappyPath(t *testing.T) {
	service, repo, mailer, member := newVerificationFixture(t)
	ctx := context.Background()

	result, err := service.RequestVerification(ctx, member.SportCenterID, "12.345.678-5", "ANA@example.com", "10.0.0.1")
	if err != nil {
		t.Fatalf("RequestVerification() error = %v", err)
	}
	if result.VerificationID == "" {
		t.Fatal("verification id is empty")
	}
	if len(repo.verifs) != 1 {
		t.Fatalf("verifications stored = %d, want 1", len(repo.verifs))
	}
	code := waitVerificationCode(t, mailer)
	if len(code) != 6 {
		t.Fatalf("code = %q, want 6 digits", code)
	}

	confirmed, err := service.ConfirmVerification(ctx, result.VerificationID, code)
	if err != nil {
		t.Fatalf("ConfirmVerification() error = %v", err)
	}
	if confirmed.MemberToken == "" {
		t.Fatal("member token is empty")
	}
	if confirmed.Member == nil || confirmed.Member.Name != "Ana Socia" {
		t.Fatalf("member = %+v, want Ana Socia", confirmed.Member)
	}
	token, err := repo.FindMemberTokenByHash(ctx, hashToken(confirmed.MemberToken))
	if err != nil || token == nil {
		t.Fatalf("token not stored hashed: %v", err)
	}
	if token.MemberID != member.ID {
		t.Fatalf("token member = %s, want %s", token.MemberID.Hex(), member.ID.Hex())
	}

	// El código es de un solo uso.
	if _, err := service.ConfirmVerification(ctx, result.VerificationID, code); err == nil {
		t.Fatal("reusing a consumed verification should fail")
	}
}

func TestMemberVerificationWrongCodeIncrementsAttempts(t *testing.T) {
	service, repo, _, member := newVerificationFixture(t)
	ctx := context.Background()

	result, err := service.RequestVerification(ctx, member.SportCenterID, member.DocumentNumber, member.Email, "10.0.0.2")
	if err != nil {
		t.Fatalf("RequestVerification() error = %v", err)
	}
	verificationID, _ := primitive.ObjectIDFromHex(result.VerificationID)

	if _, err := service.ConfirmVerification(ctx, result.VerificationID, "000000"); err == nil {
		t.Fatal("wrong code should fail")
	}
	if repo.verifs[verificationID].Attempts != 1 {
		t.Fatalf("attempts = %d, want 1", repo.verifs[verificationID].Attempts)
	}
}

func TestMemberVerificationBlocksAfterMaxAttempts(t *testing.T) {
	service, repo, _, member := newVerificationFixture(t)
	ctx := context.Background()

	result, err := service.RequestVerification(ctx, member.SportCenterID, member.DocumentNumber, member.Email, "10.0.0.3")
	if err != nil {
		t.Fatalf("RequestVerification() error = %v", err)
	}
	verificationID, _ := primitive.ObjectIDFromHex(result.VerificationID)
	repo.verifs[verificationID].Attempts = memberVerificationAttempts

	if _, err := service.ConfirmVerification(ctx, result.VerificationID, "123456"); err == nil {
		t.Fatal("verification with max attempts should fail")
	}
}

func TestMemberVerificationGenericWhenMemberNotFound(t *testing.T) {
	service, repo, mailer, member := newVerificationFixture(t)
	ctx := context.Background()

	result, err := service.RequestVerification(ctx, member.SportCenterID, "11111111-1", "otro@example.com", "10.0.0.4")
	if err != nil {
		t.Fatalf("RequestVerification() error = %v", err)
	}
	if result.VerificationID == "" {
		t.Fatal("generic response should still include an id")
	}
	if len(repo.verifs) != 0 {
		t.Fatalf("verifications stored = %d, want 0", len(repo.verifs))
	}
	select {
	case code := <-mailer.codes:
		t.Fatalf("unexpected verification code sent: %q", code)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestMemberVerificationRejectsEmailMismatch(t *testing.T) {
	service, repo, _, member := newVerificationFixture(t)
	ctx := context.Background()

	if _, err := service.RequestVerification(ctx, member.SportCenterID, member.DocumentNumber, "otro@example.com", "10.0.0.5"); err != nil {
		t.Fatalf("RequestVerification() error = %v", err)
	}
	if len(repo.verifs) != 0 {
		t.Fatalf("verifications stored = %d, want 0 for email mismatch", len(repo.verifs))
	}
}

func TestMemberVerificationRateLimit(t *testing.T) {
	service, repo, _, member := newVerificationFixture(t)
	ctx := context.Background()

	if _, err := service.RequestVerification(ctx, member.SportCenterID, member.DocumentNumber, member.Email, "10.0.0.6"); err != nil {
		t.Fatalf("first request error = %v", err)
	}
	if _, err := service.RequestVerification(ctx, member.SportCenterID, member.DocumentNumber, member.Email, "10.0.0.6"); err != nil {
		t.Fatalf("second request error = %v", err)
	}
	if len(repo.verifs) != 1 {
		t.Fatalf("verifications stored = %d, want 1 (cooldown should block the second)", len(repo.verifs))
	}
}
