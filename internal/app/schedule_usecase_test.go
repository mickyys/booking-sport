package app

import (
	"testing"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
)

func dateForTest() time.Time {
	return time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
}

func ptrBool(b bool) *bool { return &b }

func TestTimeToMinutes(t *testing.T) {
	cases := []struct {
		in      string
		want    int
		wantErr bool
	}{
		{"09:00", 540, false},
		{"15:30", 930, false},
		{"23:59", 1439, false},
		{"24:00", 1440, false},
		{"9:00", 540, false},
		{"abc", 0, true},
		{"", 0, true},
		{"10:75", 0, true},
	}
	for _, c := range cases {
		got, err := timeToMinutes(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("timeToMinutes(%q): expected error, got %d", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("timeToMinutes(%q): unexpected error %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("timeToMinutes(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func slot(start, end string, price float64) domain.ScheduleRuleSlot {
	return domain.ScheduleRuleSlot{
		StartTime:       start,
		EndTime:         end,
		Price:           price,
		PaymentRequired: true,
	}
}

func TestValidateSlotList(t *testing.T) {
	// OK: franjas consecutivas
	if err := validateSlotList([]domain.ScheduleRuleSlot{slot("09:00", "17:00", 10000), slot("17:00", "23:00", 25000)}); err != nil {
		t.Fatalf("expected no error for consecutive franjas, got %v", err)
	}

	// Error: hora final menor que inicial
	if err := validateSlotList([]domain.ScheduleRuleSlot{slot("17:00", "09:00", 10000)}); err == nil {
		t.Error("expected error for end < start")
	}

	// Error: franjas superpuestas
	if err := validateSlotList([]domain.ScheduleRuleSlot{slot("09:00", "17:00", 10000), slot("15:00", "23:00", 20000)}); err == nil {
		t.Error("expected error for overlapping franjas")
	}

	// Error: precio negativo
	if err := validateSlotList([]domain.ScheduleRuleSlot{slot("09:00", "10:00", -1)}); err == nil {
		t.Error("expected error for negative price")
	}

	// Error: franjas idénticas
	if err := validateSlotList([]domain.ScheduleRuleSlot{slot("09:00", "10:00", 100), slot("09:00", "10:00", 200)}); err == nil {
		t.Error("expected error for identical franjas")
	}
}

func TestValidateRuleSet(t *testing.T) {
	// OK: regla lunes-viernes y regla sábado sin solape
	rules := []domain.ScheduleRule{
		{DaysOfWeek: []int{1, 2, 3, 4, 5}, Slots: []domain.ScheduleRuleSlot{slot("09:00", "23:00", 10000)}},
		{DaysOfWeek: []int{6}, Slots: []domain.ScheduleRuleSlot{slot("10:00", "22:00", 15000)}},
	}
	if err := validateRuleSet(rules); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Error: dos reglas comparten día y franjas superpuestas
	rules = []domain.ScheduleRule{
		{DaysOfWeek: []int{1, 2}, Slots: []domain.ScheduleRuleSlot{slot("09:00", "17:00", 10000)}},
		{DaysOfWeek: []int{2}, Slots: []domain.ScheduleRuleSlot{slot("15:00", "20:00", 20000)}},
	}
	if err := validateRuleSet(rules); err == nil {
		t.Error("expected error for overlapping rules on same day")
	}

	// OK: reglas que comparten día pero sin solape
	rules = []domain.ScheduleRule{
		{DaysOfWeek: []int{1, 2}, Slots: []domain.ScheduleRuleSlot{slot("09:00", "12:00", 10000)}},
		{DaysOfWeek: []int{2}, Slots: []domain.ScheduleRuleSlot{slot("12:00", "20:00", 20000)}},
	}
	if err := validateRuleSet(rules); err != nil {
		t.Fatalf("expected no error for non-overlapping shared day, got %v", err)
	}

	// Error: day_of_week fuera de rango
	if err := validateRuleSet([]domain.ScheduleRule{{DaysOfWeek: []int{7}, Slots: []domain.ScheduleRuleSlot{slot("09:00", "10:00", 100)}}}); err == nil {
		t.Error("expected error for invalid day_of_week")
	}

	// Error: days_of_week vacío
	if err := validateRuleSet([]domain.ScheduleRule{{DaysOfWeek: []int{}, Slots: []domain.ScheduleRuleSlot{slot("09:00", "10:00", 100)}}}); err == nil {
		t.Error("expected error for empty days_of_week")
	}
}

func TestValidateException(t *testing.T) {
	// closed sin slots es válido
	if err := validateException(&domain.ScheduleException{Type: domain.ScheduleExceptionClosed, Date: dateForTest()}); err != nil {
		t.Fatalf("expected no error for closed exception, got %v", err)
	}

	// special sin slots es inválido
	if err := validateException(&domain.ScheduleException{Type: domain.ScheduleExceptionSpecial, Date: dateForTest()}); err == nil {
		t.Error("expected error for special exception without slots")
	}

	// tipo inválido
	if err := validateException(&domain.ScheduleException{Type: "otro", Date: dateForTest()}); err == nil {
		t.Error("expected error for invalid exception type")
	}

	// special válido
	if err := validateException(&domain.ScheduleException{
		Type:  domain.ScheduleExceptionSpecial,
		Date:  dateForTest(),
		Slots: []domain.ScheduleRuleSlot{slot("10:00", "15:00", 20000)},
	}); err != nil {
		t.Fatalf("expected no error for valid special exception, got %v", err)
	}
}

func TestValidateBlock(t *testing.T) {
	if err := validateBlock(&domain.Block{Date: dateForTest(), StartTime: "18:00", EndTime: "20:00"}); err != nil {
		t.Fatalf("expected no error for valid block, got %v", err)
	}
	if err := validateBlock(&domain.Block{Date: dateForTest(), StartTime: "20:00", EndTime: "18:00"}); err == nil {
		t.Error("expected error for block with end < start")
	}
}
