package domain

import (
	"testing"
	"time"
)

func TestStartInstantUsesMinutes(t *testing.T) {
	date, err := ParseSantiagoDate("2026-07-15")
	if err != nil {
		t.Fatalf("ParseSantiagoDate: %v", err)
	}

	booking := &Booking{
		Date:    date,
		Hour:    22,
		Minutes: 30,
	}

	want := time.Date(2026, time.July, 16, 2, 30, 0, 0, time.UTC)
	if got := booking.StartInstant(); !got.Equal(want) {
		t.Fatalf("StartInstant() = %s, want %s", got, want)
	}
}

func TestStartInstantPrefersScheduledAt(t *testing.T) {
	scheduled := time.Date(2026, time.January, 10, 15, 45, 0, 0, time.UTC)
	booking := &Booking{
		Date:        time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC),
		Hour:        9,
		Minutes:     0,
		ScheduledAt: scheduled,
	}

	if got := booking.StartInstant(); !got.Equal(scheduled) {
		t.Fatalf("StartInstant() = %s, want %s", got, scheduled)
	}
}

func TestDurationOrDefault(t *testing.T) {
	if got := (&Booking{}).DurationOrDefault(); got != 60 {
		t.Fatalf("legacy DurationOrDefault() = %d, want 60", got)
	}
	if got := (&Booking{DurationMinutes: 90}).DurationOrDefault(); got != 90 {
		t.Fatalf("DurationOrDefault() = %d, want 90", got)
	}
}

func TestEndInstant(t *testing.T) {
	start := time.Date(2026, time.July, 16, 2, 30, 0, 0, time.UTC)
	booking := &Booking{
		ScheduledAt:     start,
		DurationMinutes: 90,
	}

	want := time.Date(2026, time.July, 16, 4, 0, 0, 0, time.UTC)
	if got := booking.EndInstant(); !got.Equal(want) {
		t.Fatalf("EndInstant() = %s, want %s", got, want)
	}
}
