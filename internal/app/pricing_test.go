package app

import (
	"context"
	"testing"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// ---------- Fakes en memoria ----------

type pricingMembershipRepo struct {
	members map[primitive.ObjectID]*domain.Member
	plans   map[primitive.ObjectID]*domain.MembershipPlan
	tokens  map[string]*domain.MemberToken
	verifs  map[primitive.ObjectID]*domain.MemberVerification
}

func newPricingMembershipRepo() *pricingMembershipRepo {
	return &pricingMembershipRepo{
		members: map[primitive.ObjectID]*domain.Member{},
		plans:   map[primitive.ObjectID]*domain.MembershipPlan{},
		tokens:  map[string]*domain.MemberToken{},
		verifs:  map[primitive.ObjectID]*domain.MemberVerification{},
	}
}

func (r *pricingMembershipRepo) CreatePlan(_ context.Context, plan *domain.MembershipPlan) error {
	r.plans[plan.ID] = plan
	return nil
}
func (r *pricingMembershipRepo) UpdatePlan(_ context.Context, plan *domain.MembershipPlan) error {
	r.plans[plan.ID] = plan
	return nil
}
func (r *pricingMembershipRepo) FindPlanByID(_ context.Context, id primitive.ObjectID) (*domain.MembershipPlan, error) {
	if p, ok := r.plans[id]; ok {
		return p, nil
	}
	return nil, domain.NewNotFoundError("plan no encontrado")
}
func (r *pricingMembershipRepo) FindPlansByCenter(_ context.Context, centerID primitive.ObjectID) ([]domain.MembershipPlan, error) {
	out := []domain.MembershipPlan{}
	for _, p := range r.plans {
		if p.SportCenterID == centerID {
			out = append(out, *p)
		}
	}
	return out, nil
}
func (r *pricingMembershipRepo) DeletePlan(_ context.Context, id primitive.ObjectID) error {
	delete(r.plans, id)
	return nil
}

func (r *pricingMembershipRepo) CreateMember(_ context.Context, member *domain.Member) error {
	r.members[member.ID] = member
	return nil
}
func (r *pricingMembershipRepo) UpdateMember(_ context.Context, member *domain.Member) error {
	r.members[member.ID] = member
	return nil
}
func (r *pricingMembershipRepo) FindMemberByID(_ context.Context, id primitive.ObjectID) (*domain.Member, error) {
	if m, ok := r.members[id]; ok {
		return m, nil
	}
	return nil, domain.NewNotFoundError("socio no encontrado")
}
func (r *pricingMembershipRepo) FindMemberByDocument(_ context.Context, centerID primitive.ObjectID, document string) (*domain.Member, error) {
	for _, m := range r.members {
		if m.SportCenterID == centerID && normalizeDocument(m.DocumentNumber) == normalizeDocument(document) {
			return m, nil
		}
	}
	return nil, domain.NewNotFoundError("socio no encontrado")
}
func (r *pricingMembershipRepo) FindMembersByCenter(_ context.Context, centerID primitive.ObjectID) ([]domain.Member, error) {
	out := []domain.Member{}
	for _, m := range r.members {
		if m.SportCenterID == centerID {
			out = append(out, *m)
		}
	}
	return out, nil
}
func (r *pricingMembershipRepo) DeleteMember(_ context.Context, id primitive.ObjectID) error {
	delete(r.members, id)
	return nil
}

func (r *pricingMembershipRepo) CreateVerification(_ context.Context, v *domain.MemberVerification) error {
	r.verifs[v.ID] = v
	return nil
}
func (r *pricingMembershipRepo) FindVerificationByID(_ context.Context, id primitive.ObjectID) (*domain.MemberVerification, error) {
	if v, ok := r.verifs[id]; ok {
		return v, nil
	}
	return nil, domain.NewNotFoundError("verificación no encontrada")
}
func (r *pricingMembershipRepo) IncrementVerificationAttempts(_ context.Context, id primitive.ObjectID) error {
	if v, ok := r.verifs[id]; ok {
		v.Attempts++
	}
	return nil
}
func (r *pricingMembershipRepo) ConsumeVerification(_ context.Context, id primitive.ObjectID, at time.Time) error {
	if v, ok := r.verifs[id]; ok {
		v.ConsumedAt = &at
	}
	return nil
}

func (r *pricingMembershipRepo) CreateMemberToken(_ context.Context, token *domain.MemberToken) error {
	r.tokens[token.TokenHash] = token
	return nil
}
func (r *pricingMembershipRepo) FindMemberTokenByHash(_ context.Context, hash string) (*domain.MemberToken, error) {
	if t, ok := r.tokens[hash]; ok {
		return t, nil
	}
	return nil, domain.NewNotFoundError("token no encontrado")
}

var _ MembershipRepository = (*pricingMembershipRepo)(nil)

type pricingDiscountRepo struct {
	codes    map[primitive.ObjectID]*domain.DiscountCode
	counters map[string]*domain.DiscountUsageCounter
	usages   map[primitive.ObjectID]*domain.DiscountUsage
}

func newPricingDiscountRepo() *pricingDiscountRepo {
	return &pricingDiscountRepo{
		codes:    map[primitive.ObjectID]*domain.DiscountCode{},
		counters: map[string]*domain.DiscountUsageCounter{},
		usages:   map[primitive.ObjectID]*domain.DiscountUsage{},
	}
}

func pricingCounterKey(source string, sourceID primitive.ObjectID, identity, period string) string {
	return source + "|" + sourceID.Hex() + "|" + identity + "|" + period
}

func (r *pricingDiscountRepo) CreateCode(_ context.Context, code *domain.DiscountCode) error {
	r.codes[code.ID] = code
	return nil
}
func (r *pricingDiscountRepo) CreateCodes(_ context.Context, codes []domain.DiscountCode) error {
	for i := range codes {
		c := codes[i]
		r.codes[c.ID] = &c
	}
	return nil
}
func (r *pricingDiscountRepo) FindCodeByID(_ context.Context, id primitive.ObjectID) (*domain.DiscountCode, error) {
	if c, ok := r.codes[id]; ok {
		return c, nil
	}
	return nil, domain.NewNotFoundError("código no encontrado")
}
func (r *pricingDiscountRepo) FindCodeByCode(_ context.Context, centerID primitive.ObjectID, code string) (*domain.DiscountCode, error) {
	for _, c := range r.codes {
		if c.SportCenterID == centerID && c.Code == code {
			return c, nil
		}
	}
	return nil, domain.NewNotFoundError("código no encontrado")
}
func (r *pricingDiscountRepo) FindCodesByCenter(_ context.Context, centerID primitive.ObjectID) ([]domain.DiscountCode, error) {
	out := []domain.DiscountCode{}
	for _, c := range r.codes {
		if c.SportCenterID == centerID {
			out = append(out, *c)
		}
	}
	return out, nil
}
func (r *pricingDiscountRepo) FindCodesByBatch(_ context.Context, centerID primitive.ObjectID, batchID string) ([]domain.DiscountCode, error) {
	out := []domain.DiscountCode{}
	for _, c := range r.codes {
		if c.SportCenterID == centerID && c.BatchID == batchID {
			out = append(out, *c)
		}
	}
	return out, nil
}
func (r *pricingDiscountRepo) UpdateCodeStatus(_ context.Context, id primitive.ObjectID, status domain.DiscountCodeStatus) error {
	if c, ok := r.codes[id]; ok {
		c.Status = status
	}
	return nil
}
func (r *pricingDiscountRepo) UpdateBatchStatus(_ context.Context, centerID primitive.ObjectID, batchID string, status domain.DiscountCodeStatus) error {
	for _, c := range r.codes {
		if c.SportCenterID == centerID && c.BatchID == batchID {
			c.Status = status
		}
	}
	return nil
}
func (r *pricingDiscountRepo) TryIncrementCodeUsed(_ context.Context, id primitive.ObjectID, maxUses int) (bool, error) {
	c, ok := r.codes[id]
	if !ok {
		return false, domain.NewNotFoundError("código no encontrado")
	}
	if maxUses > 0 && c.UsedCount >= maxUses {
		return false, nil
	}
	c.UsedCount++
	return true, nil
}
func (r *pricingDiscountRepo) ReleaseCodeUsed(_ context.Context, id primitive.ObjectID) error {
	if c, ok := r.codes[id]; ok && c.UsedCount > 0 {
		c.UsedCount--
	}
	return nil
}

func (r *pricingDiscountRepo) TryReserveCounter(_ context.Context, centerID primitive.ObjectID, source string, sourceID primitive.ObjectID, identityKey, periodKey string, limit int) (bool, error) {
	key := pricingCounterKey(source, sourceID, identityKey, periodKey)
	counter, ok := r.counters[key]
	if !ok {
		counter = &domain.DiscountUsageCounter{
			SportCenterID: centerID,
			Source:        source,
			SourceID:      sourceID,
			IdentityKey:   identityKey,
			PeriodKey:     periodKey,
		}
		r.counters[key] = counter
	}
	if limit > 0 && counter.Reserved+counter.Confirmed >= limit {
		return false, nil
	}
	counter.Reserved++
	return true, nil
}
func (r *pricingDiscountRepo) FindCounter(_ context.Context, source string, sourceID primitive.ObjectID, identityKey, periodKey string) (*domain.DiscountUsageCounter, error) {
	if c, ok := r.counters[pricingCounterKey(source, sourceID, identityKey, periodKey)]; ok {
		copy := *c
		return &copy, nil
	}
	return nil, nil
}
func (r *pricingDiscountRepo) ConfirmCounter(_ context.Context, source string, sourceID primitive.ObjectID, identityKey, periodKey string) error {
	if c, ok := r.counters[pricingCounterKey(source, sourceID, identityKey, periodKey)]; ok && c.Reserved > 0 {
		c.Reserved--
		c.Confirmed++
	}
	return nil
}
func (r *pricingDiscountRepo) ReleaseCounter(_ context.Context, source string, sourceID primitive.ObjectID, identityKey, periodKey string) error {
	if c, ok := r.counters[pricingCounterKey(source, sourceID, identityKey, periodKey)]; ok && c.Reserved > 0 {
		c.Reserved--
	}
	return nil
}
func (r *pricingDiscountRepo) ReleaseConfirmedCounter(_ context.Context, source string, sourceID primitive.ObjectID, identityKey, periodKey string) error {
	if c, ok := r.counters[pricingCounterKey(source, sourceID, identityKey, periodKey)]; ok && c.Confirmed > 0 {
		c.Confirmed--
	}
	return nil
}

func (r *pricingDiscountRepo) CreateUsage(_ context.Context, usage *domain.DiscountUsage) error {
	copy := *usage
	r.usages[usage.BookingID] = &copy
	return nil
}
func (r *pricingDiscountRepo) FindUsageByBookingID(_ context.Context, bookingID primitive.ObjectID) (*domain.DiscountUsage, error) {
	if u, ok := r.usages[bookingID]; ok {
		copy := *u
		return &copy, nil
	}
	return nil, nil
}
func (r *pricingDiscountRepo) FindUsagesByCodeID(_ context.Context, codeID primitive.ObjectID) ([]domain.DiscountUsage, error) {
	out := []domain.DiscountUsage{}
	for _, u := range r.usages {
		if u.SourceID == codeID {
			out = append(out, *u)
		}
	}
	return out, nil
}
func (r *pricingDiscountRepo) FindUsagesByCenter(_ context.Context, centerID primitive.ObjectID, _ int) ([]domain.DiscountUsage, error) {
	out := []domain.DiscountUsage{}
	for _, u := range r.usages {
		if u.SportCenterID == centerID {
			out = append(out, *u)
		}
	}
	return out, nil
}
func (r *pricingDiscountRepo) UpdateUsageStatus(_ context.Context, bookingID primitive.ObjectID, status string) error {
	if u, ok := r.usages[bookingID]; ok {
		u.Status = status
	}
	return nil
}

var _ DiscountRepository = (*pricingDiscountRepo)(nil)

// ---------- Helpers ----------

type pricingFixture struct {
	centerID primitive.ObjectID
	court    *domain.Court
	center   *domain.SportCenter
	service  *PricingService
	members  *pricingMembershipRepo
	discount *pricingDiscountRepo
}

func newPricingFixture(t *testing.T) *pricingFixture {
	t.Helper()
	centerID := primitive.NewObjectID()
	courtID := primitive.NewObjectID()
	court := &domain.Court{
		ID:            courtID,
		SportCenterID: centerID,
		Name:          "Cancha 1",
		Schedule: []domain.CourtSchedule{
			{Hour: 10, Minutes: 0, Price: 30000, Status: "available"},
		},
	}
	center := &domain.SportCenter{ID: centerID, Name: "Centro Test", Timezone: domain.SantiagoTimezone}

	courtRepo := &mockCourtRepoForRecurring{FindByIDFn: func(context.Context, primitive.ObjectID) (*domain.Court, error) {
		return court, nil
	}}
	centerRepo := &mockCenterRepoForRecurring{}

	members := newPricingMembershipRepo()
	discount := newPricingDiscountRepo()
	service := NewPricingService(courtRepo, centerRepo, &mockSpaceRepo{}, nil, members, discount)
	return &pricingFixture{
		centerID: centerID,
		court:    court,
		center:   center,
		service:  service,
		members:  members,
		discount: discount,
	}
}

func (f *pricingFixture) addMember(t *testing.T, planDiscount domain.DiscountValue, limit domain.UsageLimit) (*domain.Member, *domain.MembershipPlan) {
	t.Helper()
	plan := &domain.MembershipPlan{
		ID:            primitive.NewObjectID(),
		SportCenterID: f.centerID,
		Name:          "Socio Full",
		Discount:      planDiscount,
		Scope:         domain.DiscountScope{Mode: domain.DiscountScopeCategory, Categories: []domain.SpaceCategory{domain.SpaceCategoryCancha}},
		UsageLimit:    limit,
		Active:        true,
	}
	f.members.plans[plan.ID] = plan
	member := &domain.Member{
		ID:            primitive.NewObjectID(),
		SportCenterID: f.centerID,
		PlanID:        plan.ID,
		Name:          "Ana Socia",
		Email:         "ana@example.com",
		Status:        domain.MemberStatusActive,
	}
	f.members.members[member.ID] = member
	return member, plan
}

func (f *pricingFixture) addCode(t *testing.T, code string, discount domain.DiscountValue, limit domain.UsageLimit, scope domain.DiscountScope) *domain.DiscountCode {
	t.Helper()
	doc := &domain.DiscountCode{
		ID:            primitive.NewObjectID(),
		SportCenterID: f.centerID,
		Code:          code,
		Discount:      discount,
		Scope:         scope,
		UsageLimit:    limit,
		Status:        domain.DiscountCodeStatusActive,
	}
	f.discount.codes[doc.ID] = doc
	return doc
}

func (f *pricingFixture) quote(t *testing.T, input QuoteInput) *QuoteResult {
	t.Helper()
	input.CourtID = f.court.ID
	if input.Date.IsZero() {
		input.Date = time.Now()
	}
	if input.DurationMinutes == 0 {
		input.DurationMinutes = 60
	}
	result, err := f.service.Quote(context.Background(), input)
	if err != nil {
		t.Fatalf("Quote() error = %v", err)
	}
	return result
}

// ---------- Tests ----------

func TestPricingQuoteWithoutBenefits(t *testing.T) {
	f := newPricingFixture(t)
	result := f.quote(t, QuoteInput{Hour: 10})

	if result.Quote.BasePrice != 30000 || result.Quote.FinalPrice != 30000 {
		t.Fatalf("precio = base %v final %v, want 30000/30000", result.Quote.BasePrice, result.Quote.FinalPrice)
	}
	if len(result.Quote.Applied) != 0 || len(result.Quote.Rejected) != 0 {
		t.Fatalf("applied/rejected = %v/%v, want empty", result.Quote.Applied, result.Quote.Rejected)
	}
	if result.Source != "" {
		t.Fatalf("source = %q, want empty", result.Source)
	}
}

func TestPricingQuoteAppliesLargerBenefit(t *testing.T) {
	f := newPricingFixture(t)
	member, plan := f.addMember(t, domain.DiscountValue{Type: domain.DiscountTypePercent, Value: 10}, domain.UsageLimit{Period: domain.DiscountPeriodTotal})
	f.addCode(t, "GRANDE", domain.DiscountValue{Type: domain.DiscountTypeFixed, Value: 5000}, domain.UsageLimit{Period: domain.DiscountPeriodTotal}, domain.DiscountScope{Mode: domain.DiscountScopeAll})

	result := f.quote(t, QuoteInput{Hour: 10, MemberID: member.ID.Hex(), DiscountCode: "grande"})

	if result.Source != domain.DiscountSourceCode {
		t.Fatalf("source = %q, want code", result.Source)
	}
	if result.Quote.DiscountTotal != 5000 || result.Quote.FinalPrice != 25000 {
		t.Fatalf("discount/final = %v/%v, want 5000/25000", result.Quote.DiscountTotal, result.Quote.FinalPrice)
	}
	if len(result.Quote.Applied) != 1 || result.Quote.Applied[0].Kind != "code" {
		t.Fatalf("applied = %+v, want single code benefit", result.Quote.Applied)
	}
	if len(result.Quote.Rejected) != 1 || result.Quote.Rejected[0].Kind != domain.DiscountSourcePlan {
		t.Fatalf("rejected = %+v, want rejected plan", result.Quote.Rejected)
	}
	if result.PlanID != primitive.NilObjectID {
		t.Fatalf("plan id should be cleared when code wins")
	}
	_ = plan
}

func TestPricingQuoteTiePrefersMembership(t *testing.T) {
	f := newPricingFixture(t)
	member, _ := f.addMember(t, domain.DiscountValue{Type: domain.DiscountTypePercent, Value: 10}, domain.UsageLimit{Period: domain.DiscountPeriodTotal})
	code := f.addCode(t, "IGUAL", domain.DiscountValue{Type: domain.DiscountTypePercent, Value: 10}, domain.UsageLimit{Period: domain.DiscountPeriodTotal}, domain.DiscountScope{Mode: domain.DiscountScopeAll})

	result := f.quote(t, QuoteInput{Hour: 10, MemberID: member.ID.Hex(), DiscountCode: "IGUAL"})

	if result.Source != domain.DiscountSourcePlan {
		t.Fatalf("source = %q, want plan on tie", result.Source)
	}
	if result.Quote.DiscountTotal != 3000 {
		t.Fatalf("discount = %v, want 3000", result.Quote.DiscountTotal)
	}
	if code.UsedCount != 0 {
		t.Fatalf("code used count changed during quote: %d", code.UsedCount)
	}
	if len(result.Quote.Rejected) != 1 || result.Quote.Rejected[0].Code != "IGUAL" {
		t.Fatalf("rejected = %+v, want code rejected", result.Quote.Rejected)
	}
}

func TestPricingQuoteRejectsOutOfScopeCode(t *testing.T) {
	f := newPricingFixture(t)
	f.addCode(t, "OTRO", domain.DiscountValue{Type: domain.DiscountTypePercent, Value: 20}, domain.UsageLimit{Period: domain.DiscountPeriodTotal}, domain.DiscountScope{
		Mode:     domain.DiscountScopeSpaces,
		SpaceIDs: []primitive.ObjectID{primitive.NewObjectID()},
	})

	result := f.quote(t, QuoteInput{Hour: 10, DiscountCode: "OTRO"})

	if result.Source != "" || result.Quote.FinalPrice != 30000 {
		t.Fatalf("source/final = %q/%v, want no discount and 30000", result.Source, result.Quote.FinalPrice)
	}
	if len(result.Quote.Rejected) != 1 || result.Quote.Rejected[0].Reason != "el código no aplica a este espacio" {
		t.Fatalf("rejected = %+v, want scope rejection", result.Quote.Rejected)
	}
}

func TestPricingQuoteEnforcesPerUserLimit(t *testing.T) {
	f := newPricingFixture(t)
	code := f.addCode(t, "UNO", domain.DiscountValue{Type: domain.DiscountTypePercent, Value: 10}, domain.UsageLimit{MaxUsesPerUser: 1, Period: domain.DiscountPeriodTotal}, domain.DiscountScope{Mode: domain.DiscountScopeAll})
	f.discount.counters[pricingCounterKey(domain.DiscountSourceCode, code.ID, "ana@example.com", "all")] = &domain.DiscountUsageCounter{
		Source:      domain.DiscountSourceCode,
		SourceID:    code.ID,
		IdentityKey: "ana@example.com",
		PeriodKey:   "all",
		Reserved:    1,
	}

	result := f.quote(t, QuoteInput{Hour: 10, DiscountCode: "UNO", IdentityEmail: "Ana@Example.com"})

	if result.Source != "" {
		t.Fatalf("source = %q, want no discount after limit reached", result.Source)
	}
	if len(result.Quote.Rejected) != 1 || result.Quote.Rejected[0].Reason != "ya usaste este código el máximo de veces permitido" {
		t.Fatalf("rejected = %+v, want per-user limit rejection", result.Quote.Rejected)
	}
}

func TestPricingQuoteRejectsInvalidMemberToken(t *testing.T) {
	f := newPricingFixture(t)
	result := f.quote(t, QuoteInput{Hour: 10, MemberToken: "token-invalido"})

	if result.Source != "" {
		t.Fatalf("source = %q, want no discount", result.Source)
	}
	if len(result.Quote.Rejected) != 1 || result.Quote.Rejected[0].Kind != domain.DiscountSourcePlan {
		t.Fatalf("rejected = %+v, want invalid member rejection", result.Quote.Rejected)
	}
}

func TestPricingQuoteAcceptsMemberToken(t *testing.T) {
	f := newPricingFixture(t)
	member, plan := f.addMember(t, domain.DiscountValue{Type: domain.DiscountTypePercent, Value: 15}, domain.UsageLimit{Period: domain.DiscountPeriodTotal})
	f.members.tokens[hashToken("token-ok")] = &domain.MemberToken{
		SportCenterID: f.centerID,
		MemberID:      member.ID,
		PlanID:        plan.ID,
		TokenHash:     hashToken("token-ok"),
		ExpiresAt:     time.Now().Add(time.Hour),
	}

	result := f.quote(t, QuoteInput{Hour: 10, MemberToken: "token-ok"})

	if result.Source != domain.DiscountSourcePlan {
		t.Fatalf("source = %q, want plan", result.Source)
	}
	if result.Quote.FinalPrice != 25500 {
		t.Fatalf("final = %v, want 25500", result.Quote.FinalPrice)
	}
	if result.Quote.Member == nil || result.Quote.Member.Name != "Ana Socia" {
		t.Fatalf("member = %+v, want Ana Socia", result.Quote.Member)
	}
}

func TestPricingReserveConfirmReleaseCode(t *testing.T) {
	f := newPricingFixture(t)
	code := f.addCode(t, "PROMO", domain.DiscountValue{Type: domain.DiscountTypePercent, Value: 10}, domain.UsageLimit{MaxUses: 2, MaxUsesPerUser: 1, Period: domain.DiscountPeriodTotal}, domain.DiscountScope{Mode: domain.DiscountScopeAll})
	ctx := context.Background()
	result := f.quote(t, QuoteInput{Hour: 10, DiscountCode: "PROMO", IdentityEmail: "ana@example.com"})

	booking := &domain.Booking{ID: primitive.NewObjectID(), BookingCode: "ABC123"}
	if err := f.service.Reserve(ctx, result, booking); err != nil {
		t.Fatalf("Reserve() error = %v", err)
	}
	if code.UsedCount != 1 {
		t.Fatalf("used_count = %d, want 1", code.UsedCount)
	}
	usage, _ := f.discount.FindUsageByBookingID(ctx, booking.ID)
	if usage == nil || usage.Status != domain.DiscountUsageReserved {
		t.Fatalf("usage = %+v, want reserved", usage)
	}

	// Un segundo intento con el mismo correo choca con el límite por persona.
	second := f.quote(t, QuoteInput{Hour: 10, DiscountCode: "PROMO", IdentityEmail: "ana@example.com"})
	if second.Source != "" {
		t.Fatalf("second quote should be rejected, got source %q", second.Source)
	}

	if err := f.service.Confirm(ctx, booking.ID); err != nil {
		t.Fatalf("Confirm() error = %v", err)
	}
	usage, _ = f.discount.FindUsageByBookingID(ctx, booking.ID)
	if usage.Status != domain.DiscountUsageConfirmed {
		t.Fatalf("usage status = %q, want confirmed", usage.Status)
	}
	counter, _ := f.discount.FindCounter(ctx, domain.DiscountSourceCode, code.ID, "ana@example.com", "all")
	if counter == nil || counter.Confirmed != 1 || counter.Reserved != 0 {
		t.Fatalf("counter = %+v, want confirmed=1 reserved=0", counter)
	}

	// Cancelar la reserva pagada devuelve el uso del código y el cupo.
	if err := f.service.Release(ctx, booking.ID); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
	if code.UsedCount != 0 {
		t.Fatalf("used_count after release = %d, want 0", code.UsedCount)
	}
	usage, _ = f.discount.FindUsageByBookingID(ctx, booking.ID)
	if usage.Status != domain.DiscountUsageReleased {
		t.Fatalf("usage status = %q, want released", usage.Status)
	}
	counter, _ = f.discount.FindCounter(ctx, domain.DiscountSourceCode, code.ID, "ana@example.com", "all")
	if counter.Confirmed != 0 {
		t.Fatalf("confirmed after release = %d, want 0", counter.Confirmed)
	}
}

func TestPricingReserveFailsWhenCodeExhausted(t *testing.T) {
	f := newPricingFixture(t)
	code := f.addCode(t, "ULTIMO", domain.DiscountValue{Type: domain.DiscountTypePercent, Value: 10}, domain.UsageLimit{MaxUses: 1, Period: domain.DiscountPeriodTotal}, domain.DiscountScope{Mode: domain.DiscountScopeAll})
	ctx := context.Background()
	result := f.quote(t, QuoteInput{Hour: 10, DiscountCode: "ULTIMO"})

	// Otro cliente se lleva el último uso entre la cotización y la reserva.
	code.UsedCount = 1
	err := f.service.Reserve(ctx, result, &domain.Booking{ID: primitive.NewObjectID()})
	if err == nil || err != domain.ErrDiscountUnavailable {
		t.Fatalf("Reserve() error = %v, want ErrDiscountUnavailable", err)
	}
	if len(f.discount.usages) != 0 {
		t.Fatalf("usage should not be created when reservation fails")
	}
}

func TestPricingReserveReleasesCodeWhenCounterFails(t *testing.T) {
	f := newPricingFixture(t)
	code := f.addCode(t, "DOBLE", domain.DiscountValue{Type: domain.DiscountTypePercent, Value: 10}, domain.UsageLimit{MaxUses: 10, MaxUsesPerUser: 1, Period: domain.DiscountPeriodTotal}, domain.DiscountScope{Mode: domain.DiscountScopeAll})
	ctx := context.Background()
	result := f.quote(t, QuoteInput{Hour: 10, DiscountCode: "DOBLE", IdentityEmail: "ana@example.com"})
	f.discount.counters[pricingCounterKey(domain.DiscountSourceCode, code.ID, "ana@example.com", "all")] = &domain.DiscountUsageCounter{
		Source: domain.DiscountSourceCode, SourceID: code.ID, IdentityKey: "ana@example.com", PeriodKey: "all", Reserved: 1,
	}

	err := f.service.Reserve(ctx, result, &domain.Booking{ID: primitive.NewObjectID()})
	if err != domain.ErrDiscountUnavailable {
		t.Fatalf("Reserve() error = %v, want ErrDiscountUnavailable", err)
	}
	if code.UsedCount != 0 {
		t.Fatalf("used_count = %d, want 0 after rollback", code.UsedCount)
	}
}

func TestPricingReserveWithoutBenefitIsNoop(t *testing.T) {
	f := newPricingFixture(t)
	ctx := context.Background()
	result := f.quote(t, QuoteInput{Hour: 10})
	if err := f.service.Reserve(ctx, result, &domain.Booking{ID: primitive.NewObjectID()}); err != nil {
		t.Fatalf("Reserve() error = %v", err)
	}
	if err := f.service.Confirm(ctx, primitive.NewObjectID()); err != nil {
		t.Fatalf("Confirm() error = %v", err)
	}
	if err := f.service.Release(ctx, primitive.NewObjectID()); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
}

func TestPricingCommissionSnapshot(t *testing.T) {
	booking := &domain.Booking{
		Pricing: &domain.BookingPricing{BasePrice: 30000, DiscountTotal: 3000, FinalPrice: 27000},
	}
	applyCommissionSnapshot(booking, 10, 27000)
	if booking.Pricing.Commission == nil {
		t.Fatal("commission snapshot not set")
	}
	if booking.Pricing.Commission.Rate != 10 || booking.Pricing.Commission.Base != 27000 || booking.Pricing.Commission.Amount != 2700 {
		t.Fatalf("commission = %+v, want rate 10 base 27000 amount 2700", booking.Pricing.Commission)
	}

	// Un porcentaje inválido cae al default del 7%.
	fallback := &domain.Booking{Pricing: &domain.BookingPricing{}}
	applyCommissionSnapshot(fallback, 0, 10000)
	if fallback.Pricing.Commission.Rate != defaultMarketplaceFeePercent || fallback.Pricing.Commission.Amount != 700 {
		t.Fatalf("fallback commission = %+v, want 7%% of 10000", fallback.Pricing.Commission)
	}

	// Sin snapshot de precios no se inventa una comisión.
	plain := &domain.Booking{}
	applyCommissionSnapshot(plain, 10, 10000)
	if plain.Pricing != nil {
		t.Fatalf("booking without pricing should not get commission, got %+v", plain.Pricing)
	}
}

func TestApplyQuoteToBookingStoresSnapshot(t *testing.T) {
	f := newPricingFixture(t)
	member, plan := f.addMember(t, domain.DiscountValue{Type: domain.DiscountTypePercent, Value: 10}, domain.UsageLimit{Period: domain.DiscountPeriodTotal})
	result := f.quote(t, QuoteInput{Hour: 10, MemberID: member.ID.Hex()})

	booking := &domain.Booking{}
	applyQuoteToBooking(booking, result)

	if booking.Price != 30000 || booking.FinalPrice != 27000 {
		t.Fatalf("price/final = %v/%v, want 30000/27000", booking.Price, booking.FinalPrice)
	}
	if booking.Pricing == nil || booking.Pricing.MemberID != member.ID.Hex() {
		t.Fatalf("pricing snapshot = %+v, want member id %s", booking.Pricing, member.ID.Hex())
	}
	if booking.Pricing.Applied[0].ID != plan.ID.Hex() {
		t.Fatalf("applied plan id = %q, want %q", booking.Pricing.Applied[0].ID, plan.ID.Hex())
	}
}

func TestPricingReserveAgainAfterRelease(t *testing.T) {
	f := newPricingFixture(t)
	code := f.addCode(t, "RETRY", domain.DiscountValue{Type: domain.DiscountTypePercent, Value: 10}, domain.UsageLimit{MaxUses: 5, MaxUsesPerUser: 1, Period: domain.DiscountPeriodTotal}, domain.DiscountScope{Mode: domain.DiscountScopeAll})
	ctx := context.Background()
	booking := &domain.Booking{ID: primitive.NewObjectID(), BookingCode: "RETRY1", GuestDetails: &domain.GuestDetails{Email: "ana@example.com"}}

	// Primer intento de pago: se reserva y luego se abandona.
	result := f.quote(t, QuoteInput{Hour: 10, DiscountCode: "RETRY", IdentityEmail: "ana@example.com"})
	if err := f.service.Reserve(ctx, result, booking); err != nil {
		t.Fatalf("first Reserve() error = %v", err)
	}
	if err := f.service.Release(ctx, booking.ID); err != nil {
		t.Fatalf("Release() error = %v", err)
	}

	// Reintento sobre la misma reserva: debe volver a quedar reservado y el
	// contador por persona debe terminar confirmado una sola vez.
	result = f.quote(t, QuoteInput{Hour: 10, DiscountCode: "RETRY", IdentityEmail: "ana@example.com"})
	if result.Source != domain.DiscountSourceCode {
		t.Fatalf("source after release = %q, want code", result.Source)
	}
	if err := f.service.Reserve(ctx, result, booking); err != nil {
		t.Fatalf("second Reserve() error = %v", err)
	}
	if err := f.service.Confirm(ctx, booking.ID); err != nil {
		t.Fatalf("Confirm() error = %v", err)
	}

	usage, _ := f.discount.FindUsageByBookingID(ctx, booking.ID)
	if usage == nil || usage.Status != domain.DiscountUsageConfirmed {
		t.Fatalf("usage = %+v, want confirmed after retry", usage)
	}
	if code.UsedCount != 1 {
		t.Fatalf("used_count = %d, want 1", code.UsedCount)
	}
	counter, _ := f.discount.FindCounter(ctx, domain.DiscountSourceCode, code.ID, "ana@example.com", "all")
	if counter == nil || counter.Reserved != 0 || counter.Confirmed != 1 {
		t.Fatalf("counter = %+v, want reserved=0 confirmed=1", counter)
	}
}
