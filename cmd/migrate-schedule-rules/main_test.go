package main

import (
	"reflect"
	"testing"

	"github.com/hamp/booking-sport/internal/app"
	"github.com/hamp/booking-sport/internal/domain"
)

// La conversión de slots legacy a reglas vive en internal/app para que el
// comando de migración y el fallback de lectura no diverjan. Estos tests
// verifican el contrato que consume cmd/migrate-schedule-rules.
func intPtr(i int) *int { return &i }

func TestBuildRulesGeneralAndSpecific(t *testing.T) {
	slots := []domain.CourtSchedule{
		{Hour: 9, Minutes: 0, Price: 10000, Status: "available", DayOfWeek: nil},
		{Hour: 10, Minutes: 0, Price: 10000, Status: "available", DayOfWeek: nil},
		{Hour: 15, Minutes: 30, Price: 20000, Status: "available", DayOfWeek: intPtr(1)}, // lunes 15:30
		{Hour: 17, Minutes: 0, Price: 20000, Status: "available", DayOfWeek: intPtr(1)},  // lunes 17:00
	}

	rules := app.BuildRulesFromLegacyCourtSchedule(slots)

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
	// En el modelo legacy los slots de un día se suman a los generales, así que
	// la regla del lunes incluye 09:00 y 10:00 además de 15:30 y 17:00, ordenados.
	want := [][2]string{{"09:00", "10:00"}, {"10:00", "11:00"}, {"15:30", "16:30"}, {"17:00", "18:00"}}
	if len(monday.Slots) != len(want) {
		t.Fatalf("expected %d monday franjas, got %d", len(want), len(monday.Slots))
	}
	for i, w := range want {
		if monday.Slots[i].StartTime != w[0] || monday.Slots[i].EndTime != w[1] {
			t.Errorf("monday franja %d = %s-%s, want %s-%s", i, monday.Slots[i].StartTime, monday.Slots[i].EndTime, w[0], w[1])
		}
	}
	if monday.Slots[2].Price != 20000 || monday.Slots[0].Price != 10000 {
		t.Errorf("unexpected prices: general %v, specific %v", monday.Slots[0].Price, monday.Slots[2].Price)
	}
}

func TestBuildRulesSkipsClosed(t *testing.T) {
	slots := []domain.CourtSchedule{
		{Hour: 9, Minutes: 0, Price: 10000, Status: "available", DayOfWeek: nil},
		{Hour: 10, Minutes: 0, Price: 10000, Status: "closed", DayOfWeek: nil},
	}

	rules := app.BuildRulesFromLegacyCourtSchedule(slots)

	if len(rules) != 1 {
		t.Fatalf("expected 1 rule (closed slot skipped), got %d", len(rules))
	}
	if len(rules[0].Slots) != 1 {
		t.Errorf("expected 1 franja, got %d", len(rules[0].Slots))
	}
}

func TestBuildRulesMidnightSlots(t *testing.T) {
	slots := []domain.CourtSchedule{
		{Hour: 22, Minutes: 0, Price: 30000, Status: "available", DayOfWeek: nil},
		{Hour: 23, Minutes: 0, Price: 31000, Status: "available", DayOfWeek: nil},
		{Hour: 23, Minutes: 59, Price: 32000, Status: "available", DayOfWeek: nil},
	}

	rules := app.BuildRulesFromLegacyCourtSchedule(slots)

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
	slots := []domain.CourtSchedule{
		{
			Hour: 9, Minutes: 0, Price: 10000, Status: "available",
			PaymentRequired: false, PaymentOptional: true, PartialPaymentEnabled: &p, DayOfWeek: nil,
		},
	}

	rules := app.BuildRulesFromLegacyCourtSchedule(slots)
	if len(rules) != 1 || len(rules[0].Slots) != 1 {
		t.Fatalf("unexpected rules: %d rules, %d franjas", len(rules), len(rules[0].Slots))
	}
	f := rules[0].Slots[0]
	if f.PaymentRequired || !f.PaymentOptional || f.PartialPaymentEnabled == nil || !*f.PartialPaymentEnabled {
		t.Errorf("payment flags not preserved: %+v", f)
	}
}
