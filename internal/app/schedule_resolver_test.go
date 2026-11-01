package app

import (
	"testing"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
)

func daysEqual(got *domain.ScheduleRule, want []int) bool {
	if got == nil {
		return false
	}
	if len(got.DaysOfWeek) != len(want) {
		return false
	}
	seen := make(map[int]bool)
	for _, d := range got.DaysOfWeek {
		seen[d] = true
	}
	for _, d := range want {
		if !seen[d] {
			return false
		}
	}
	return true
}

func TestMostSpecificRuleForDay(t *testing.T) {
	general := domain.ScheduleRule{DaysOfWeek: []int{0, 1, 2, 3, 4, 5, 6}}
	weekdays := domain.ScheduleRule{DaysOfWeek: []int{1, 2, 3, 4, 5}}
	monday := domain.ScheduleRule{DaysOfWeek: []int{1}}

	rules := []domain.ScheduleRule{general, weekdays, monday}

	// Lunes (1): debe ganar la regla específica de lunes
	if got := mostSpecificRuleForDay(rules, 1); !daysEqual(got, monday.DaysOfWeek) {
		t.Errorf("monday: expected monday rule to win, got %v", got)
	}
	// Sábado (6): solo aplica general
	if got := mostSpecificRuleForDay(rules, 6); !daysEqual(got, general.DaysOfWeek) {
		t.Errorf("saturday: expected general rule to win, got %v", got)
	}
	// Martes (2): gana weekdays
	if got := mostSpecificRuleForDay(rules, 2); !daysEqual(got, weekdays.DaysOfWeek) {
		t.Errorf("tuesday: expected weekdays rule to win, got %v", got)
	}
	// Domingo (0): general
	if got := mostSpecificRuleForDay(rules, 0); !daysEqual(got, general.DaysOfWeek) {
		t.Errorf("sunday: expected general rule to win, got %v", got)
	}
}

func TestMostSpecificRuleForDayNone(t *testing.T) {
	rules := []domain.ScheduleRule{{DaysOfWeek: []int{1, 2, 3, 4, 5}}}
	if got := mostSpecificRuleForDay(rules, 6); got != nil {
		t.Errorf("expected nil rule for saturday (cerrado)")
	}
}

func TestExpandFranjasToSlots(t *testing.T) {
	franjas := []domain.ScheduleRuleSlot{
		{StartTime: "09:00", EndTime: "11:00", Price: 10000, PaymentRequired: true},
		{StartTime: "15:30", EndTime: "17:00", Price: 20000},
	}

	slots := expandFranjasToSlots(franjas)

	// 09:00-11:00 -> 09:00, 10:00. 15:30-17:00 -> 15:30 (90 min => 1 slot de 60).
	if len(slots) != 3 {
		t.Fatalf("expected 3 slots, got %d: %+v", len(slots), slots)
	}

	first := slots[0]
	if first.Hour != 9 || first.Minutes != 0 || first.Price != 10000 || !first.PaymentRequired {
		t.Errorf("unexpected first slot: %+v", first)
	}
	second := slots[1]
	if second.Hour != 10 || second.Minutes != 0 || second.Price != 10000 {
		t.Errorf("unexpected second slot: %+v", second)
	}
	third := slots[2]
	if third.Hour != 15 || third.Minutes != 30 || third.Price != 20000 {
		t.Errorf("unexpected third slot: %+v", third)
	}
}

func TestApplyBlocksToSlots(t *testing.T) {
	loc, _ := time.LoadLocation("America/Santiago")
	date := time.Date(2026, 8, 20, 0, 0, 0, 0, loc)

	slots := expandFranjasToSlots([]domain.ScheduleRuleSlot{
		{StartTime: "09:00", EndTime: "12:00", Price: 10000},
	})
	blocks := []domain.Block{
		{Date: date, StartTime: "10:30", EndTime: "11:30", Reason: "Mantenimiento"},
	}

	out := applyBlocksToSlots(slots, blocks)

	// 09:00 (libre), 10:00 (solapa 10:30-11:30 => closed), 11:00 (solapa => closed)
	if len(out) != 3 {
		t.Fatalf("expected 3 slots, got %d", len(out))
	}
	if out[0].Status != "available" {
		t.Errorf("slot 09:00 should be available, got %s", out[0].Status)
	}
	if out[1].Status != "closed" {
		t.Errorf("slot 10:00 should be closed (block 10:30-11:30), got %s", out[1].Status)
	}
	if out[2].Status != "closed" {
		t.Errorf("slot 11:00 should be closed (block 10:30-11:30), got %s", out[2].Status)
	}
}

func TestFindFranjaForInterval(t *testing.T) {
	franjas := []domain.ScheduleRuleSlot{
		{StartTime: "09:00", EndTime: "17:00", Price: 10000},
		{StartTime: "17:00", EndTime: "23:00", Price: 25000},
	}

	// Reserva de 60 min dentro de la primera franja
	f := findFranjaForInterval(franjas, 9*60, 10*60)
	if f == nil || f.Price != 10000 {
		t.Fatalf("expected franja 09:00-17:00, got %+v", f)
	}

	// Reserva de 90 min (15:30-17:00) dentro de la primera franja
	f = findFranjaForInterval(franjas, 15*60+30, 17*60)
	if f == nil || f.Price != 10000 {
		t.Fatalf("expected franja to cover 15:30-17:00, got %+v", f)
	}

	// Reserva que cruza el límite entre franjas (16:30-17:30) => no cubierta
	f = findFranjaForInterval(franjas, 16*60+30, 17*60+30)
	if f != nil {
		t.Fatalf("expected no franja to cover a crossing interval, got %+v", f)
	}

	// Reserva fuera de toda franja (23:30-00:30)
	f = findFranjaForInterval(franjas, 23*60+30, 24*60+30)
	if f != nil {
		t.Fatalf("expected no franja for out-of-range interval, got %+v", f)
	}

	// Franja que empieza exactamente en el inicio de otra (15:30-17:00)
	franjas2 := []domain.ScheduleRuleSlot{
		{StartTime: "15:30", EndTime: "17:00", Price: 20000},
	}
	f = findFranjaForInterval(franjas2, 15*60+30, 16*60+30)
	if f == nil || f.Price != 20000 {
		t.Fatalf("expected franja 15:30-17:00 to cover 15:30-16:30, got %+v", f)
	}
}
