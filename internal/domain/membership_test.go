package domain

import (
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

func TestDiscountAmountFor(t *testing.T) {
	cases := []struct {
		name string
		base float64
		d    DiscountValue
		want float64
	}{
		{"percent", 30000, DiscountValue{Type: DiscountTypePercent, Value: 10}, 3000},
		{"percent rounds", 29990, DiscountValue{Type: DiscountTypePercent, Value: 10}, 2999},
		{"fixed", 30000, DiscountValue{Type: DiscountTypeFixed, Value: 5000}, 5000},
		{"fixed capped at base", 4000, DiscountValue{Type: DiscountTypeFixed, Value: 5000}, 4000},
		{"zero base", 0, DiscountValue{Type: DiscountTypePercent, Value: 10}, 0},
		{"negative base", -100, DiscountValue{Type: DiscountTypePercent, Value: 10}, 0},
		{"invalid type", 30000, DiscountValue{Type: "OTHER", Value: 10}, 0},
		{"negative value", 30000, DiscountValue{Type: DiscountTypeFixed, Value: -100}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DiscountAmountFor(tc.base, tc.d); got != tc.want {
				t.Fatalf("DiscountAmountFor(%v, %+v) = %v, want %v", tc.base, tc.d, got, tc.want)
			}
		})
	}
}

func TestDiscountScopeCovers(t *testing.T) {
	spaceID := primitive.NewObjectID()
	otherID := primitive.NewObjectID()

	cases := []struct {
		name     string
		scope    DiscountScope
		category SpaceCategory
		spaceID  primitive.ObjectID
		want     bool
	}{
		{"default is cancha", DiscountScope{}, SpaceCategoryCancha, spaceID, true},
		{"default excludes piscina", DiscountScope{}, SpaceCategoryPiscina, spaceID, false},
		{"category match", DiscountScope{Mode: DiscountScopeCategory, Categories: []SpaceCategory{SpaceCategoryQuincho}}, SpaceCategoryQuincho, spaceID, true},
		{"category mismatch", DiscountScope{Mode: DiscountScopeCategory, Categories: []SpaceCategory{SpaceCategoryQuincho}}, SpaceCategoryCancha, spaceID, false},
		{"all covers everything", DiscountScope{Mode: DiscountScopeAll}, SpaceCategoryPiscina, spaceID, true},
		{"spaces match", DiscountScope{Mode: DiscountScopeSpaces, SpaceIDs: []primitive.ObjectID{spaceID}}, SpaceCategoryCancha, spaceID, true},
		{"spaces mismatch", DiscountScope{Mode: DiscountScopeSpaces, SpaceIDs: []primitive.ObjectID{otherID}}, SpaceCategoryCancha, spaceID, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.scope.Covers(tc.category, tc.spaceID); got != tc.want {
				t.Fatalf("Covers() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDiscountScopeNormalize(t *testing.T) {
	var scope DiscountScope
	scope.Normalize()
	if scope.Mode != DiscountScopeCategory {
		t.Fatalf("mode = %q, want %q", scope.Mode, DiscountScopeCategory)
	}
	if len(scope.Categories) != 1 || scope.Categories[0] != SpaceCategoryCancha {
		t.Fatalf("categories = %v, want [cancha]", scope.Categories)
	}

	spaces := DiscountScope{Mode: DiscountScopeSpaces}
	spaces.Normalize()
	if len(spaces.Categories) != 0 {
		t.Fatalf("spaces scope should not get default categories, got %v", spaces.Categories)
	}
}

func TestDiscountCodeDerivedStatus(t *testing.T) {
	now := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)

	cases := []struct {
		name string
		code DiscountCode
		want string
	}{
		{"available", DiscountCode{Status: DiscountCodeStatusActive}, DiscountCodeDerivedAvailable},
		{"voided wins", DiscountCode{Status: DiscountCodeStatusVoided, ValidFrom: &future}, DiscountCodeDerivedVoided},
		{"scheduled", DiscountCode{Status: DiscountCodeStatusActive, ValidFrom: &future}, DiscountCodeDerivedScheduled},
		{"expired", DiscountCode{Status: DiscountCodeStatusActive, ValidTo: &past}, DiscountCodeDerivedExpired},
		{"exhausted", DiscountCode{Status: DiscountCodeStatusActive, UsedCount: 5, UsageLimit: UsageLimit{MaxUses: 5}}, DiscountCodeDerivedExhausted},
		{"unlimited is available", DiscountCode{Status: DiscountCodeStatusActive, UsedCount: 500}, DiscountCodeDerivedAvailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.code.DerivedStatus(now); got != tc.want {
				t.Fatalf("DerivedStatus() = %q, want %q", got, tc.want)
			}
			if tc.code.IsUsable(now) != (tc.want == DiscountCodeDerivedAvailable) {
				t.Fatalf("IsUsable() inconsistent with DerivedStatus %q", tc.want)
			}
		})
	}
}

func TestMemberIsUsable(t *testing.T) {
	now := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	past := now.Add(-24 * time.Hour)
	future := now.Add(24 * time.Hour)

	cases := []struct {
		name   string
		member Member
		want   bool
	}{
		{"active without dates", Member{Status: MemberStatusActive}, true},
		{"suspended", Member{Status: MemberStatusSuspended}, false},
		{"expired status", Member{Status: MemberStatusExpired}, false},
		{"not started", Member{Status: MemberStatusActive, ValidFrom: &future}, false},
		{"ended", Member{Status: MemberStatusActive, ValidTo: &past}, false},
		{"within range", Member{Status: MemberStatusActive, ValidFrom: &past, ValidTo: &future}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.member.IsUsable(now); got != tc.want {
				t.Fatalf("IsUsable() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestPeriodKey(t *testing.T) {
	loc, err := time.LoadLocation(SantiagoTimezone)
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	// 2026-03-01 01:00 UTC = 2026-02-28 22:00 in Santiago (UTC-3), so the
	// period must follow the center timezone, not UTC.
	instant := time.Date(2026, 3, 1, 1, 0, 0, 0, time.UTC)

	cases := []struct {
		period DiscountPeriod
		want   string
	}{
		{DiscountPeriodDay, "2026-02-28"},
		{DiscountPeriodWeek, "2026-W09"},
		{DiscountPeriodFortnight, "2026-02-Q2"},
		{DiscountPeriodMonth, "2026-02"},
		{DiscountPeriodYear, "2026"},
		{DiscountPeriodTotal, "all"},
		{"", "all"},
	}
	for _, tc := range cases {
		t.Run(string(tc.period), func(t *testing.T) {
			if got := PeriodKey(tc.period, instant, loc); got != tc.want {
				t.Fatalf("PeriodKey(%q) = %q, want %q", tc.period, got, tc.want)
			}
		})
	}

	if got := PeriodKey(DiscountPeriodMonth, instant, nil); got != "2026-03" {
		t.Fatalf("PeriodKey with nil location = %q, want UTC month 2026-03", got)
	}
}

func TestPeriodKeyFortnightBoundary(t *testing.T) {
	loc, err := time.LoadLocation(SantiagoTimezone)
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	cases := []struct {
		day  int
		want string
	}{
		{1, "2026-10-Q1"},
		{15, "2026-10-Q1"},
		{16, "2026-10-Q2"},
		{31, "2026-10-Q2"},
	}
	for _, tc := range cases {
		instant := time.Date(2026, 10, tc.day, 12, 0, 0, 0, loc)
		if got := PeriodKey(DiscountPeriodFortnight, instant, loc); got != tc.want {
			t.Errorf("day %d: PeriodKey(fortnight) = %q, want %q", tc.day, got, tc.want)
		}
	}
	if !DiscountPeriodFortnight.IsValid() {
		t.Error("DiscountPeriodFortnight should be valid")
	}
}

func TestBookingChargedPrice(t *testing.T) {
	cases := []struct {
		name    string
		booking *Booking
		want    float64
	}{
		{"with discount", &Booking{Price: 30000, FinalPrice: 27000}, 27000},
		{"fully discounted", &Booking{Price: 30000, FinalPrice: 0, Pricing: &BookingPricing{}}, 0},
		{"legacy without final price", &Booking{Price: 30000}, 30000},
		{"nil booking", nil, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.booking.ChargedPrice(); got != tc.want {
				t.Fatalf("ChargedPrice() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestUsageLimitNormalize(t *testing.T) {
	limit := UsageLimit{MaxUses: -3, MaxUsesPerUser: -1, Period: "bogus"}
	limit.Normalize()
	if limit.MaxUses != 0 || limit.MaxUsesPerUser != 0 || limit.Period != DiscountPeriodTotal {
		t.Fatalf("Normalize() = %+v, want zeroed limits and total period", limit)
	}

	monthly := UsageLimit{MaxUses: 10, MaxUsesPerUser: 2, Period: DiscountPeriodMonth}
	monthly.Normalize()
	if monthly.Period != DiscountPeriodMonth || monthly.MaxUses != 10 || monthly.MaxUsesPerUser != 2 {
		t.Fatalf("Normalize() changed valid values: %+v", monthly)
	}
}
