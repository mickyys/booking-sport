package app

import (
	"testing"

	"github.com/hamp/booking-sport/internal/domain"
)

func intPtr(v int) *int { return &v }

func TestBuildRulesFromLegacyCourtScheduleGeneral(t *testing.T) {
	rules := BuildRulesFromLegacyCourtSchedule([]domain.CourtSchedule{
		{Hour: 9, Minutes: 0, Price: 10000, Status: "available"},
		{Hour: 10, Minutes: 0, Price: 10000, Status: "available"},
		{Hour: 11, Minutes: 0, Price: 0, Status: "closed"},
	})

	if len(rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(rules))
	}
	if len(rules[0].DaysOfWeek) != 7 {
		t.Fatalf("expected general rule for 7 days, got %v", rules[0].DaysOfWeek)
	}
	if len(rules[0].Slots) != 2 {
		t.Fatalf("expected 2 franjas, got %d", len(rules[0].Slots))
	}
	if rules[0].Slots[0].StartTime != "09:00" || rules[0].Slots[0].EndTime != "10:00" {
		t.Errorf("unexpected first franja %+v", rules[0].Slots[0])
	}
	if rules[0].Slots[0].Price != 10000 {
		t.Errorf("expected price 10000, got %v", rules[0].Slots[0].Price)
	}
}

func TestBuildRulesFromLegacyCourtSchedulePerDay(t *testing.T) {
	rules := BuildRulesFromLegacyCourtSchedule([]domain.CourtSchedule{
		{Hour: 15, Minutes: 30, Price: 20000, Status: "available", DayOfWeek: intPtr(1)},
		{Hour: 9, Minutes: 0, Price: 10000, Status: "available", DayOfWeek: intPtr(2)},
	})

	if len(rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(rules))
	}
	if len(rules[0].DaysOfWeek) != 1 || rules[0].DaysOfWeek[0] != 1 {
		t.Errorf("expected rule for Monday, got %v", rules[0].DaysOfWeek)
	}
	if rules[0].Slots[0].StartTime != "15:30" || rules[0].Slots[0].EndTime != "16:30" {
		t.Errorf("unexpected franja %+v", rules[0].Slots[0])
	}
	if len(rules[1].DaysOfWeek) != 1 || rules[1].DaysOfWeek[0] != 2 {
		t.Errorf("expected rule for Tuesday, got %v", rules[1].DaysOfWeek)
	}
}

func TestBuildRulesFromLegacyCourtScheduleSkipsMidnightOverflow(t *testing.T) {
	rules := BuildRulesFromLegacyCourtSchedule([]domain.CourtSchedule{
		{Hour: 23, Minutes: 30, Price: 10000, Status: "available"},
		{Hour: 23, Minutes: 0, Price: 10000, Status: "available"},
	})

	if len(rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(rules))
	}
	if len(rules[0].Slots) != 1 {
		t.Fatalf("expected only the 23:00 franja, got %d", len(rules[0].Slots))
	}
	if rules[0].Slots[0].EndTime != "24:00" {
		t.Errorf("expected end 24:00, got %s", rules[0].Slots[0].EndTime)
	}
}

func TestBuildRulesFromLegacyFacility(t *testing.T) {
	rules := BuildRulesFromLegacyFacility(12, 23)
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(rules))
	}
	if rules[0].Slots[0].StartTime != "12:00" || rules[0].Slots[0].EndTime != "23:00" {
		t.Errorf("unexpected franja %+v", rules[0].Slots[0])
	}
	if rules[0].Slots[0].Price != 0 {
		t.Errorf("expected price 0 for facility availability, got %v", rules[0].Slots[0].Price)
	}
}

func TestBuildRulesFromLegacyFacilityDefaults(t *testing.T) {
	rules := BuildRulesFromLegacyFacility(0, 0)
	if rules[0].Slots[0].StartTime != "09:00" || rules[0].Slots[0].EndTime != "22:00" {
		t.Errorf("expected default range 09:00-22:00, got %+v", rules[0].Slots[0])
	}
}

func TestBuildFullDayRule(t *testing.T) {
	rules := BuildFullDayRule()
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(rules))
	}
	if rules[0].Slots[0].StartTime != "00:00" || rules[0].Slots[0].EndTime != "24:00" {
		t.Errorf("unexpected franja %+v", rules[0].Slots[0])
	}
}

func TestBuildRulesFromLegacySpace(t *testing.T) {
	cancha := &domain.Space{
		Category: domain.SpaceCategoryCancha,
		Schedule: []domain.CourtSchedule{{Hour: 9, Minutes: 0, Price: 10000, Status: "available"}},
	}
	if rules := BuildRulesFromLegacySpace(cancha); len(rules) != 1 {
		t.Errorf("expected cancha rules, got %d", len(rules))
	}

	quincho := &domain.Space{Category: domain.SpaceCategoryQuincho, OpenHour: 12, CloseHour: 23}
	if rules := BuildRulesFromLegacySpace(quincho); len(rules) != 1 || rules[0].Slots[0].StartTime != "12:00" {
		t.Errorf("expected quincho rule, got %+v", rules)
	}

	sede := &domain.Space{Category: domain.SpaceCategorySede}
	if rules := BuildRulesFromLegacySpace(sede); len(rules) != 1 || rules[0].Slots[0].EndTime != "24:00" {
		t.Errorf("expected full-day rule, got %+v", rules)
	}
}
