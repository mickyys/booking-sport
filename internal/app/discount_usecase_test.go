package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func newDiscountUseCaseFixture() (*DiscountUseCase, *pricingDiscountRepo, primitive.ObjectID) {
	repo := newPricingDiscountRepo()
	centerID := primitive.NewObjectID()
	return NewDiscountUseCase(repo, &mockCenterRepoForRecurring{}), repo, centerID
}

func validDiscountInput() BatchCodeInput {
	return BatchCodeInput{
		Count:      10,
		Prefix:     "VERANO",
		Discount:   domain.DiscountValue{Type: domain.DiscountTypePercent, Value: 15},
		Scope:      domain.DiscountScope{Mode: domain.DiscountScopeAll},
		UsageLimit: domain.UsageLimit{MaxUsesPerUser: 1, Period: domain.DiscountPeriodMonth},
	}
}

func TestCreateBatchGeneratesUniquePrefixedCodes(t *testing.T) {
	uc, repo, centerID := newDiscountUseCaseFixture()
	codes, err := uc.CreateBatch(context.Background(), centerID, validDiscountInput(), "admin@test.cl")
	if err != nil {
		t.Fatalf("CreateBatch() error = %v", err)
	}
	if len(codes) != 10 {
		t.Fatalf("codes = %d, want 10", len(codes))
	}
	seen := map[string]struct{}{}
	batchID := codes[0].BatchID
	for _, code := range codes {
		if !strings.HasPrefix(code.Code, "VERANO-") {
			t.Fatalf("code %q does not use prefix", code.Code)
		}
		if _, ok := seen[code.Code]; ok {
			t.Fatalf("duplicate code generated: %q", code.Code)
		}
		seen[code.Code] = struct{}{}
		if code.BatchID == "" || code.BatchID != batchID {
			t.Fatalf("batch id = %q, want shared %q", code.BatchID, batchID)
		}
		if code.Status != domain.DiscountCodeStatusActive {
			t.Fatalf("status = %q, want active", code.Status)
		}
		if code.UsageLimit.Period != domain.DiscountPeriodMonth {
			t.Fatalf("period = %q, want month", code.UsageLimit.Period)
		}
		if code.Scope.Mode != domain.DiscountScopeAll {
			t.Fatalf("scope = %q, want all", code.Scope.Mode)
		}
	}
	if len(repo.codes) != 10 {
		t.Fatalf("stored codes = %d, want 10", len(repo.codes))
	}
}

func TestCreateBatchValidation(t *testing.T) {
	uc, _, centerID := newDiscountUseCaseFixture()
	ctx := context.Background()

	cases := []struct {
		name  string
		input BatchCodeInput
	}{
		{"zero count", func() BatchCodeInput { in := validDiscountInput(); in.Count = 0; return in }()},
		{"too many", func() BatchCodeInput { in := validDiscountInput(); in.Count = 501; return in }()},
		{"invalid prefix", func() BatchCodeInput { in := validDiscountInput(); in.Prefix = "VER ANO"; return in }()},
		{"percent over 100", func() BatchCodeInput {
			in := validDiscountInput()
			in.Discount = domain.DiscountValue{Type: domain.DiscountTypePercent, Value: 120}
			return in
		}()},
		{"zero discount", func() BatchCodeInput {
			in := validDiscountInput()
			in.Discount = domain.DiscountValue{Type: domain.DiscountTypeFixed, Value: 0}
			return in
		}()},
		{"empty space scope", func() BatchCodeInput {
			in := validDiscountInput()
			in.Scope = domain.DiscountScope{Mode: domain.DiscountScopeSpaces}
			return in
		}()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := uc.CreateBatch(ctx, centerID, tc.input, "admin@test.cl"); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestCreateCodeNormalizesAndGenerates(t *testing.T) {
	uc, repo, centerID := newDiscountUseCaseFixture()
	ctx := context.Background()

	custom, err := uc.CreateCode(ctx, centerID, CreateCodeInput{
		Code:       "  promo-10 ",
		Discount:   domain.DiscountValue{Type: domain.DiscountTypePercent, Value: 10},
		Scope:      domain.DiscountScope{Mode: domain.DiscountScopeAll},
		UsageLimit: domain.UsageLimit{Period: domain.DiscountPeriodTotal},
	}, "admin@test.cl")
	if err != nil {
		t.Fatalf("CreateCode() error = %v", err)
	}
	if custom.Code != "PROMO-10" {
		t.Fatalf("code = %q, want PROMO-10", custom.Code)
	}

	generated, err := uc.CreateCode(ctx, centerID, CreateCodeInput{
		Discount:   domain.DiscountValue{Type: domain.DiscountTypeFixed, Value: 3000},
		Scope:      domain.DiscountScope{Mode: domain.DiscountScopeAll},
		UsageLimit: domain.UsageLimit{Period: domain.DiscountPeriodTotal},
	}, "admin@test.cl")
	if err != nil {
		t.Fatalf("CreateCode() generated error = %v", err)
	}
	if len(generated.Code) != 6 {
		t.Fatalf("generated code = %q, want 6 characters", generated.Code)
	}
	for _, r := range generated.Code {
		if !strings.ContainsRune(discountCodeAlphabet, r) {
			t.Fatalf("generated code %q contains invalid rune %q", generated.Code, r)
		}
	}
	if len(repo.codes) != 2 {
		t.Fatalf("stored codes = %d, want 2", len(repo.codes))
	}
}

func TestCreateCodeAppliesDefaultScope(t *testing.T) {
	uc, _, centerID := newDiscountUseCaseFixture()
	code, err := uc.CreateCode(context.Background(), centerID, CreateCodeInput{
		Code:       "CANCHA",
		Discount:   domain.DiscountValue{Type: domain.DiscountTypePercent, Value: 10},
		UsageLimit: domain.UsageLimit{},
	}, "admin@test.cl")
	if err != nil {
		t.Fatalf("CreateCode() error = %v", err)
	}
	if code.Scope.Mode != domain.DiscountScopeCategory {
		t.Fatalf("scope mode = %q, want category", code.Scope.Mode)
	}
	if len(code.Scope.Categories) != 1 || code.Scope.Categories[0] != domain.SpaceCategoryCancha {
		t.Fatalf("scope categories = %v, want [cancha]", code.Scope.Categories)
	}
	if code.UsageLimit.Period != domain.DiscountPeriodTotal {
		t.Fatalf("period = %q, want total", code.UsageLimit.Period)
	}
}

func TestVoidCodeRespectsCenterOwnership(t *testing.T) {
	uc, repo, centerID := newDiscountUseCaseFixture()
	ctx := context.Background()
	code, err := uc.CreateCode(ctx, centerID, CreateCodeInput{
		Code:       "MIO",
		Discount:   domain.DiscountValue{Type: domain.DiscountTypePercent, Value: 10},
		Scope:      domain.DiscountScope{Mode: domain.DiscountScopeAll},
		UsageLimit: domain.UsageLimit{Period: domain.DiscountPeriodTotal},
	}, "admin@test.cl")
	if err != nil {
		t.Fatalf("CreateCode() error = %v", err)
	}

	if err := uc.VoidCode(ctx, primitive.NewObjectID(), code.ID); err == nil {
		t.Fatal("voiding a code from another center should fail")
	}
	if repo.codes[code.ID].Status != domain.DiscountCodeStatusActive {
		t.Fatalf("status changed to %q", repo.codes[code.ID].Status)
	}

	if err := uc.VoidCode(ctx, centerID, code.ID); err != nil {
		t.Fatalf("VoidCode() error = %v", err)
	}
	if repo.codes[code.ID].Status != domain.DiscountCodeStatusVoided {
		t.Fatalf("status = %q, want voided", repo.codes[code.ID].Status)
	}
}

func TestVoidBatch(t *testing.T) {
	uc, repo, centerID := newDiscountUseCaseFixture()
	ctx := context.Background()
	codes, err := uc.CreateBatch(ctx, centerID, validDiscountInput(), "admin@test.cl")
	if err != nil {
		t.Fatalf("CreateBatch() error = %v", err)
	}

	if err := uc.VoidBatch(ctx, centerID, "  "); err == nil {
		t.Fatal("empty batch id should fail")
	}
	if err := uc.VoidBatch(ctx, centerID, "no-existe"); err == nil {
		t.Fatal("unknown batch should fail")
	}
	if err := uc.VoidBatch(ctx, centerID, codes[0].BatchID); err != nil {
		t.Fatalf("VoidBatch() error = %v", err)
	}
	for _, code := range repo.codes {
		if code.Status != domain.DiscountCodeStatusVoided {
			t.Fatalf("code %q status = %q, want voided", code.Code, code.Status)
		}
	}
}

func TestListCodesIncludesDerivedStatus(t *testing.T) {
	uc, repo, centerID := newDiscountUseCaseFixture()
	ctx := context.Background()
	active, _ := uc.CreateCode(ctx, centerID, CreateCodeInput{
		Code:       "ACTIVO",
		Discount:   domain.DiscountValue{Type: domain.DiscountTypePercent, Value: 10},
		Scope:      domain.DiscountScope{Mode: domain.DiscountScopeAll},
		UsageLimit: domain.UsageLimit{Period: domain.DiscountPeriodTotal},
	}, "admin@test.cl")
	past := time.Now().Add(-time.Hour)
	repo.codes[active.ID].ValidTo = &past

	views, err := uc.ListCodes(ctx, centerID)
	if err != nil {
		t.Fatalf("ListCodes() error = %v", err)
	}
	if len(views) != 1 || views[0].DerivedStatus != domain.DiscountCodeDerivedExpired {
		t.Fatalf("views = %+v, want one expired code", views)
	}
}

func TestListUsagesFiltersByCode(t *testing.T) {
	uc, repo, centerID := newDiscountUseCaseFixture()
	ctx := context.Background()
	code, _ := uc.CreateCode(ctx, centerID, CreateCodeInput{
		Code:       "USADO",
		Discount:   domain.DiscountValue{Type: domain.DiscountTypePercent, Value: 10},
		Scope:      domain.DiscountScope{Mode: domain.DiscountScopeAll},
		UsageLimit: domain.UsageLimit{Period: domain.DiscountPeriodTotal},
	}, "admin@test.cl")
	bookingID := primitive.NewObjectID()
	repo.usages[bookingID] = &domain.DiscountUsage{
		ID:            primitive.NewObjectID(),
		SportCenterID: centerID,
		Source:        domain.DiscountSourceCode,
		SourceID:      code.ID,
		Code:          code.Code,
		BookingID:     bookingID,
		Status:        domain.DiscountUsageConfirmed,
	}

	usages, err := uc.ListUsages(ctx, centerID, code.ID.Hex(), 10)
	if err != nil {
		t.Fatalf("ListUsages() error = %v", err)
	}
	if len(usages) != 1 || usages[0].Code != "USADO" {
		t.Fatalf("usages = %+v, want the code usage", usages)
	}

	if _, err := uc.ListUsages(ctx, centerID, "no-es-id", 10); err == nil {
		t.Fatal("invalid code id should fail")
	}
	if _, err := uc.ListUsages(ctx, primitive.NewObjectID(), code.ID.Hex(), 10); err == nil {
		t.Fatal("code from another center should fail")
	}
}
