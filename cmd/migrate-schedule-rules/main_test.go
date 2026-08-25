package main

import (
	"reflect"
	"testing"
)

func intPtr(i int) *int { return &i }

func TestBuildRulesGeneralAndSpecific(t *testing.T) {
	slots := []legacyCourtSchedule{
		{Hour: 9, Minutes: 0, Price: 10000, Status: "available", DayOfWeek: nil},
		{Hour: 10, Minutes: 0, Price: 10000, Status: "available", DayOfWeek: nil},
		{Hour: 15, Minutes: 30, Price: 20000, Status: "available", DayOfWeek: intPtr(1)}, // lunes 15:30
		{Hour: 17, Minutes: 0, Price: 20000, Status: "available", DayOfWeek: intPtr(1)},  // lunes 17:00
	}

	rules := buildRules(slots)

	if len(rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(rules))
	}

	// Regla general (todos los días)
	general := rules[0]
	if len(general.DaysOfWeek) != 7 {
		t.Errorf("expected general rule to cover 7 days, got %v", general.DaysOfWeek)
	}
	if len(general.Slots) != 2 {
		t.Errorf("expected 2 general franjas, got %d", len(general.Slots))
	}
	if general.Slots[0].StartTime != "09:00" || general.Slots[0].EndTime != "10:00" {
		t.Errorf("unexpected first general franja: %s-%s", general.Slots[0].StartTime, general.Slots[0].EndTime)
	}
	if general.Slots[1].StartTime != "10:00" || general.Slots[1].EndTime != "11:00" {
		t.Errorf("unexpected second general franja: %s-%s", general.Slots[1].StartTime, general.Slots[1].EndTime)
	}

	// Regla del lunes (day 1)
	monday := rules[1]
	if !reflect.DeepEqual(monday.DaysOfWeek, []int{1}) {
		t.Errorf("expected monday rule days=[1], got %v", monday.DaysOfWeek)
	}
	if len(monday.Slots) != 2 {
		t.Fatalf("expected 2 monday franjas, got %d", len(monday.Slots))
	}
	// Debe ordenarse por hora: 15:30 y luego 17:00
	if monday.Slots[0].StartTime != "15:30" || monday.Slots[0].EndTime != "16:30" {
		t.Errorf("unexpected monday first franja: %s-%s", monday.Slots[0].StartTime, monday.Slots[0].EndTime)
	}
	if monday.Slots[1].StartTime != "17:00" || monday.Slots[1].EndTime != "18:00" {
		t.Errorf("unexpected monday second franja: %s-%s", monday.Slots[1].StartTime, monday.Slots[1].EndTime)
	}
	if monday.Slots[0].Price != 20000 {
		t.Errorf("expected price 20000, got %v", monday.Slots[0].Price)
	}
}

func TestBuildRulesSkipsClosed(t *testing.T) {
	slots := []legacyCourtSchedule{
		{Hour: 9, Minutes: 0, Price: 10000, Status: "available", DayOfWeek: nil},
		{Hour: 10, Minutes: 0, Price: 10000, Status: "closed", DayOfWeek: nil},
	}

	rules := buildRules(slots)

	if len(rules) != 1 {
		t.Fatalf("expected 1 rule (closed slot skipped), got %d", len(rules))
	}
	if len(rules[0].Slots) != 1 {
		t.Errorf("expected 1 franja, got %d", len(rules[0].Slots))
	}
}

func TestBuildRulesMidnightSlots(t *testing.T) {
	slots := []legacyCourtSchedule{
		{Hour: 22, Minutes: 0, Price: 30000, Status: "available", DayOfWeek: nil},
		{Hour: 23, Minutes: 0, Price: 31000, Status: "available", DayOfWeek: nil},
		{Hour: 23, Minutes: 59, Price: 32000, Status: "available", DayOfWeek: nil},
	}

	rules := buildRules(slots)

	if len(rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(rules))
	}
	// 22:00 (ok), 23:00 -> fin 24:00 (ok), 23:59 -> cruza la medianoche (se omite)
	if len(rules[0].Slots) != 2 {
		t.Fatalf("expected 2 franjas (23:59 omitted), got %d: %+v", len(rules[0].Slots), rules[0].Slots)
	}
	last := rules[0].Slots[1]
	if last.StartTime != "23:00" || last.EndTime != "24:00" {
		t.Errorf("expected franja 23:00-24:00, got %s-%s", last.StartTime, last.EndTime)
	}
}

func TestBuildRulesPaymentFlags(t *testing.T) {
	p := true
	slots := []legacyCourtSchedule{
		{
			Hour: 9, Minutes: 0, Price: 10000, Status: "available",
			PaymentRequired: false, PaymentOptional: true, PartialPaymentEnabled: &p, DayOfWeek: nil,
		},
	}

	rules := buildRules(slots)
	if len(rules) != 1 || len(rules[0].Slots) != 1 {
		t.Fatalf("unexpected rules: %d rules, %d franjas", len(rules), len(rules[0].Slots))
	}
	f := rules[0].Slots[0]
	if f.PaymentRequired || !f.PaymentOptional || f.PartialPaymentEnabled == nil || !*f.PartialPaymentEnabled {
		t.Errorf("payment flags not preserved: %+v", f)
	}
}
