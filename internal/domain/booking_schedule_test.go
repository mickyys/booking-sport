package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func santiago(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(SantiagoTimezone)
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	return loc
}

func TestPrepareForWriteUpgradesLegacyBooking(t *testing.T) {
	loc := santiago(t)
	date, err := ParseSantiagoDate("2026-07-15")
	if err != nil {
		t.Fatalf("ParseSantiagoDate: %v", err)
	}

	booking := &Booking{Date: date, Hour: 22, Minutes: 30}

	if err := booking.PrepareForWrite(loc); err != nil {
		t.Fatalf("PrepareForWrite: %v", err)
	}

	if booking.ScheduleSchemaVersion != ScheduleSchemaVersionUTC {
		t.Errorf("schema version = %d, want %d", booking.ScheduleSchemaVersion, ScheduleSchemaVersionUTC)
	}
	if booking.Timezone != SantiagoTimezone {
		t.Errorf("timezone = %q", booking.Timezone)
	}
	if booking.LocalDate != "2026-07-15" {
		t.Errorf("local_date = %q", booking.LocalDate)
	}
	wantStart := time.Date(2026, time.July, 16, 2, 30, 0, 0, time.UTC)
	if !booking.ScheduledAt.Equal(wantStart) {
		t.Errorf("scheduled_at = %s, want %s", booking.ScheduledAt, wantStart)
	}
	wantEnd := wantStart.Add(60 * time.Minute)
	if !booking.EndsAt.Equal(wantEnd) {
		t.Errorf("ends_at = %s, want %s", booking.EndsAt, wantEnd)
	}
	if booking.DurationMinutes != 60 {
		t.Errorf("duration_minutes = %d, want 60", booking.DurationMinutes)
	}
}

func TestPrepareForWriteAcceptsNewContract(t *testing.T) {
	loc := santiago(t)
	start := time.Date(2026, time.July, 16, 2, 30, 0, 0, time.UTC)

	booking := &Booking{
		ScheduledAt:     start,
		DurationMinutes: 90,
	}

	if err := booking.PrepareForWrite(loc); err != nil {
		t.Fatalf("PrepareForWrite: %v", err)
	}

	if booking.LocalDate != "2026-07-15" {
		t.Errorf("local_date = %q, want 2026-07-15", booking.LocalDate)
	}
	if booking.Hour != 22 || booking.Minutes != 30 {
		t.Errorf("hour/minutes = %02d:%02d, want 22:30", booking.Hour, booking.Minutes)
	}
	wantDate := time.Date(2026, time.July, 15, 4, 0, 0, 0, time.UTC)
	if !booking.Date.Equal(wantDate) {
		t.Errorf("date = %s, want %s", booking.Date, wantDate)
	}
	wantEnd := start.Add(90 * time.Minute)
	if !booking.EndsAt.Equal(wantEnd) {
		t.Errorf("ends_at = %s, want %s", booking.EndsAt, wantEnd)
	}
}

func TestPrepareForWriteRejectsContradictions(t *testing.T) {
	loc := santiago(t)
	start := time.Date(2026, time.July, 16, 2, 30, 0, 0, time.UTC)
	wrongDate := time.Date(2026, time.July, 20, 4, 0, 0, 0, time.UTC)

	cases := []struct {
		name    string
		booking Booking
		want    string
	}{
		{
			name:    "date contradicts scheduled_at",
			booking: Booking{ScheduledAt: start, Date: wrongDate, DurationMinutes: 60},
			want:    "contradicts scheduled_at",
		},
		{
			name:    "hour contradicts scheduled_at",
			booking: Booking{ScheduledAt: start, Date: time.Date(2026, time.July, 15, 4, 0, 0, 0, time.UTC), Hour: 9, Minutes: 0, DurationMinutes: 60},
			want:    "contradict",
		},
		{
			name:    "local_date contradicts scheduled_at",
			booking: Booking{ScheduledAt: start, LocalDate: "2026-07-20", DurationMinutes: 60},
			want:    "contradicts scheduled_at",
		},
		{
			name:    "seconds are rejected",
			booking: Booking{ScheduledAt: start.Add(30 * time.Second), DurationMinutes: 60},
			want:    "seconds",
		},
		{
			name:    "new contract requires duration",
			booking: Booking{ScheduledAt: start},
			want:    "duration_minutes",
		},
		{
			name:    "legacy requires date",
			booking: Booking{Hour: 10},
			want:    "date is required",
		},
		{
			name:    "invalid timezone",
			booking: Booking{Date: time.Now(), Hour: 10, Timezone: "Mars/Olympus"},
			want:    "invalid timezone",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.booking.PrepareForWrite(loc)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %q, want substring %q", err, tc.want)
			}
		})
	}
}

func TestPrepareForWriteRejectsDSTGap(t *testing.T) {
	loc := santiago(t)
	date, err := ParseSantiagoDate("2026-09-06")
	if err != nil {
		t.Fatalf("ParseSantiagoDate: %v", err)
	}

	booking := &Booking{Date: date, Hour: 0, Minutes: 0}
	err = booking.PrepareForWrite(loc)

	var nonexistent *NonexistentLocalTimeError
	if !errors.As(err, &nonexistent) {
		t.Fatalf("error = %v, want NonexistentLocalTimeError", err)
	}
}

func TestPrepareForWriteHonorsCenterTimezone(t *testing.T) {
	madrid, err := time.LoadLocation("Europe/Madrid")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	date, err := ParseDate("2026-07-15", madrid)
	if err != nil {
		t.Fatalf("ParseDate: %v", err)
	}

	booking := &Booking{Date: date, Hour: 22, Minutes: 30, Timezone: "Europe/Madrid"}
	if err := booking.PrepareForWrite(santiago(t)); err != nil {
		t.Fatalf("PrepareForWrite: %v", err)
	}

	if booking.Timezone != "Europe/Madrid" {
		t.Errorf("timezone = %q, want Europe/Madrid", booking.Timezone)
	}
	if booking.LocalDate != "2026-07-15" {
		t.Errorf("local_date = %q, want 2026-07-15", booking.LocalDate)
	}
	// 22:30 CEST (UTC+2) is 20:30 UTC the same day.
	want := time.Date(2026, time.July, 15, 20, 30, 0, 0, time.UTC)
	if !booking.ScheduledAt.Equal(want) {
		t.Errorf("scheduled_at = %s, want %s", booking.ScheduledAt, want)
	}
}

func TestSportCenterTimezoneLocation(t *testing.T) {
	center := &SportCenter{}
	loc, err := center.TimezoneLocation()
	if err != nil {
		t.Fatalf("legacy center: %v", err)
	}
	if loc.String() != SantiagoTimezone {
		t.Errorf("legacy center location = %q, want %q", loc, SantiagoTimezone)
	}

	center = &SportCenter{Timezone: "Europe/Madrid"}
	loc, err = center.TimezoneLocation()
	if err != nil {
		t.Fatalf("valid center: %v", err)
	}
	if loc.String() != "Europe/Madrid" {
		t.Errorf("location = %q", loc)
	}

	center = &SportCenter{Timezone: "Not/AZone"}
	if _, err := center.TimezoneLocation(); err == nil {
		t.Fatal("expected error for invalid timezone")
	}
}
