package app

import (
	"testing"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
)

func TestParseReconcileSchedule(t *testing.T) {
	loc := domain.GetSantiagoLocation()

	got, err := ParseReconcileSchedule("06:00,09:00-22:00", loc)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	hours := got.Hours()
	if len(hours) != 15 {
		t.Fatalf("hours = %v, want 15 entries", hours)
	}
	if hours[0] != 6 || hours[1] != 9 || hours[len(hours)-1] != 22 {
		t.Fatalf("hours = %v, want 6,9..22", hours)
	}

	short, err := ParseReconcileSchedule("6,9-22", loc)
	if err != nil {
		t.Fatalf("parse short form: %v", err)
	}
	if len(short.Hours()) != len(hours) {
		t.Fatalf("short form hours = %v, want same as long form", short.Hours())
	}

	dedup, err := ParseReconcileSchedule("06,06:00,09:00-09:00", loc)
	if err != nil {
		t.Fatalf("parse dedup: %v", err)
	}
	if h := dedup.Hours(); len(h) != 2 || h[0] != 6 || h[1] != 9 {
		t.Fatalf("dedup hours = %v, want [6 9]", h)
	}

	for _, spec := range []string{"", "24", "09:30", "22-09", "abc", "-1", "09:00-"} {
		if _, err := ParseReconcileSchedule(spec, loc); err == nil {
			t.Errorf("ParseReconcileSchedule(%q) = nil error, want error", spec)
		}
	}

	if _, err := ParseReconcileSchedule("06:00", nil); err == nil {
		t.Error("nil location must be rejected")
	}
}

func TestReconcileScheduleNext(t *testing.T) {
	loc := domain.GetSantiagoLocation()
	s, err := ParseReconcileSchedule("06:00,09:00-22:00", loc)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	at := func(y int, m time.Month, d, hour, min int) time.Time {
		return time.Date(y, m, d, hour, min, 0, 0, loc)
	}

	tests := []struct {
		name string
		from time.Time
		want time.Time
	}{
		{"before first window", at(2026, time.November, 16, 8, 59), at(2026, time.November, 16, 9, 0)},
		{"exactly on a slot advances", at(2026, time.November, 16, 9, 0), at(2026, time.November, 16, 10, 0)},
		{"within the day", at(2026, time.November, 16, 21, 30), at(2026, time.November, 16, 22, 0)},
		{"last slot jumps to 06:00", at(2026, time.November, 16, 22, 0), at(2026, time.November, 17, 6, 0)},
		{"night jumps to 06:00", at(2026, time.November, 16, 23, 30), at(2026, time.November, 17, 6, 0)},
		{"early morning starts at 06:00", at(2026, time.November, 16, 0, 0), at(2026, time.November, 16, 6, 0)},
		{"before 06:00", at(2026, time.November, 16, 5, 59), at(2026, time.November, 16, 6, 0)},
		{"after 06:00 waits until 09:00", at(2026, time.November, 16, 6, 0), at(2026, time.November, 16, 9, 0)},
		{"gap between 06:00 and 09:00", at(2026, time.November, 16, 7, 30), at(2026, time.November, 16, 9, 0)},
		{"friday night", at(2026, time.November, 20, 22, 30), at(2026, time.November, 21, 6, 0)},
		{"spring forward morning", at(2026, time.September, 6, 5, 30), at(2026, time.September, 6, 6, 0)},
		{"spring forward night", at(2026, time.September, 6, 22, 30), at(2026, time.September, 7, 6, 0)},
		{"fall back morning", at(2026, time.April, 5, 5, 30), at(2026, time.April, 5, 6, 0)},
		{"fall back night", at(2026, time.April, 5, 22, 30), at(2026, time.April, 6, 6, 0)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := s.Next(tc.from)
			if !got.Equal(tc.want) {
				t.Fatalf("Next(%s) = %s, want %s",
					tc.from.Format(time.RFC3339), got.Format(time.RFC3339), tc.want.Format(time.RFC3339))
			}
		})
	}

	// A non-zero second keeps the next run in the future.
	from := at(2026, time.November, 16, 9, 0).Add(30 * time.Second)
	if got, want := s.Next(from), at(2026, time.November, 16, 10, 0); !got.Equal(want) {
		t.Fatalf("Next(with seconds) = %s, want %s", got, want)
	}
}

func TestReconcileScheduleNext_NeverSkipsOrRepeats(t *testing.T) {
	loc := domain.GetSantiagoLocation()
	s, err := ParseReconcileSchedule("06:00,09:00-22:00", loc)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	start := time.Date(2026, time.January, 1, 0, 0, 0, 0, loc)
	end := time.Date(2027, time.January, 1, 0, 0, 0, 0, loc)
	for now := start; now.Before(end); now = now.Add(30 * time.Minute) {
		next := s.Next(now)
		if !next.After(now) {
			t.Fatalf("Next(%s) = %s, must be strictly after", now, next)
		}
		local := next.In(loc)
		if local.Minute() != 0 || local.Second() != 0 {
			t.Fatalf("Next(%s) = %s, want minute 0", now, next)
		}
		if !s.allowed[local.Hour()] {
			t.Fatalf("Next(%s) = %s, hour %d is not allowed", now, next, local.Hour())
		}
		// The 22:00 -> 06:00 window is 8 wall-clock hours, but on the fall-back
		// day the absolute gap grows to 9 hours.
		if gap := next.Sub(now); gap > 9*time.Hour {
			t.Fatalf("Next(%s) = %s, gap %s exceeds the 22:00 -> 06:00 window", now, next, gap)
		}
	}
}

func TestReconcileScheduleNext_AdvancesFromReturnedSlot(t *testing.T) {
	loc := domain.GetSantiagoLocation()
	s, err := ParseReconcileSchedule("06:00,09:00-22:00", loc)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	slot := time.Date(2026, time.November, 16, 9, 0, 0, 0, loc)
	next := s.Next(slot)
	if next.Equal(slot) {
		t.Fatalf("Next(%s) returned the same slot", slot)
	}
	if again := s.Next(next); !again.After(next) {
		t.Fatalf("Next(%s) = %s, must advance", next, again)
	}
}
