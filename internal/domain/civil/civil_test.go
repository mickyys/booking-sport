package civil

import (
	"errors"
	"testing"
	"time"
)

func mustLoad(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("LoadLocation(%q): %v", name, err)
	}
	return loc
}

func mustDate(t *testing.T, value string, loc *time.Location) time.Time {
	t.Helper()
	date, err := ParseDate(value, loc)
	if err != nil {
		t.Fatalf("ParseDate(%q): %v", value, err)
	}
	return date
}

func TestNormalizeSantiagoLateBookingKeepsWallClock(t *testing.T) {
	loc := mustLoad(t, "America/Santiago")
	date := mustDate(t, "2026-07-15", loc)

	canonical, localDate, scheduledAt, err := Normalize(date, 22, 30, loc)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}

	if localDate != "2026-07-15" {
		t.Errorf("localDate = %q, want 2026-07-15", localDate)
	}
	// 22:30 in Santiago (UTC-4 in July) is 02:30 UTC the next day.
	want := time.Date(2026, time.July, 16, 2, 30, 0, 0, time.UTC)
	if !scheduledAt.Equal(want) {
		t.Errorf("scheduledAt = %s, want %s", scheduledAt, want)
	}
	wantDate := time.Date(2026, time.July, 15, 4, 0, 0, 0, time.UTC)
	if !canonical.Equal(wantDate) {
		t.Errorf("canonicalDate = %s, want %s", canonical, wantDate)
	}
}

func TestNormalizeRejectsNonexistentLocalTime(t *testing.T) {
	loc := mustLoad(t, "America/Santiago")
	// Chile skips 00:00-00:59 on 2026-09-06.
	date := mustDate(t, "2026-09-06", loc)

	_, _, _, err := Normalize(date, 0, 0, loc)

	var target *NonexistentLocalTimeError
	if !errors.As(err, &target) {
		t.Fatalf("Normalize error = %v, want NonexistentLocalTimeError", err)
	}
	if target.Timezone != "America/Santiago" {
		t.Errorf("Timezone = %q", target.Timezone)
	}
}

func TestNormalizeRejectsAmbiguousLocalTime(t *testing.T) {
	loc := mustLoad(t, "America/Santiago")
	// Chile repeats 23:00-23:59 on 2026-04-04.
	date := mustDate(t, "2026-04-04", loc)

	_, _, _, err := Normalize(date, 23, 30, loc)

	var target *AmbiguousLocalTimeError
	if !errors.As(err, &target) {
		t.Fatalf("Normalize error = %v, want AmbiguousLocalTimeError", err)
	}
}

func TestNormalizeHandlesNonHourTransitions(t *testing.T) {
	loc := mustLoad(t, "Australia/Lord_Howe")

	// 2026-10-04: 02:00 jumps to 02:30, so 02:15 does not exist.
	_, _, _, err := Normalize(mustDate(t, "2026-10-04", loc), 2, 15, loc)
	var nonexistent *NonexistentLocalTimeError
	if !errors.As(err, &nonexistent) {
		t.Fatalf("Normalize(02:15) error = %v, want NonexistentLocalTimeError", err)
	}

	// 2026-04-05: 02:00 falls back to 01:30, so 01:45 happens twice.
	_, _, _, err = Normalize(mustDate(t, "2026-04-05", loc), 1, 45, loc)
	var ambiguous *AmbiguousLocalTimeError
	if !errors.As(err, &ambiguous) {
		t.Fatalf("Normalize(01:45) error = %v, want AmbiguousLocalTimeError", err)
	}
}

func TestDayStartSkipsNonexistentMidnight(t *testing.T) {
	loc := mustLoad(t, "America/Santiago")

	start := DayStart(mustDate(t, "2026-09-06", loc), loc)
	want := time.Date(2026, time.September, 6, 1, 0, 0, 0, loc)
	if !start.Equal(want) {
		t.Fatalf("DayStart = %s, want %s", start, want)
	}
}

func TestDayBoundsAreContiguousAcrossDST(t *testing.T) {
	loc := mustLoad(t, "America/Santiago")

	for _, day := range []string{"2026-04-04", "2026-04-05", "2026-09-05", "2026-09-06", "2026-09-07"} {
		date := mustDate(t, day, loc)
		start, end := DayBounds(date, loc)
		if !end.After(start) {
			t.Fatalf("%s: end %s is not after start %s", day, end, start)
		}

		nextStart, _ := DayBounds(date.AddDate(0, 0, 1), loc)
		if !end.Equal(nextStart) {
			t.Errorf("%s: end %s != next start %s", day, end, nextStart)
		}
	}
}

func TestMatchInstantPicksFirstOccurrenceWhenAmbiguous(t *testing.T) {
	loc := mustLoad(t, "America/Santiago")
	date := mustDate(t, "2026-04-04", loc)

	got := MatchInstant(date, 23, 30, loc)
	// First occurrence still has the DST offset (-03:00) => 02:30 UTC next day.
	want := time.Date(2026, time.April, 5, 2, 30, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("MatchInstant = %s, want %s", got, want)
	}
}

func TestNormalizeRejectsOutOfRangeClock(t *testing.T) {
	loc := mustLoad(t, "America/Santiago")
	date := mustDate(t, "2026-07-15", loc)

	if _, _, _, err := Normalize(date, 24, 0, loc); err == nil {
		t.Fatal("expected error for hour 24")
	}
	if _, _, _, err := Normalize(date, 10, 60, loc); err == nil {
		t.Fatal("expected error for minute 60")
	}
}
