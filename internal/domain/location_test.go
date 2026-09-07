package domain

import (
	"testing"
	"time"
)

func TestSantiagoDayBoundsAcrossDSTStart(t *testing.T) {
	tests := []struct {
		date      string
		startUTC  string
		endUTC    string
		localHour int
	}{
		{date: "2026-09-05", startUTC: "2026-09-05T04:00:00Z", endUTC: "2026-09-06T04:00:00Z", localHour: 0},
		{date: "2026-09-06", startUTC: "2026-09-06T04:00:00Z", endUTC: "2026-09-07T03:00:00Z", localHour: 1},
		{date: "2026-09-07", startUTC: "2026-09-07T03:00:00Z", endUTC: "2026-09-08T03:00:00Z", localHour: 0},
	}

	for _, tt := range tests {
		t.Run(tt.date, func(t *testing.T) {
			date, err := ParseSantiagoDate(tt.date)
			if err != nil {
				t.Fatalf("ParseSantiagoDate() error = %v", err)
			}

			start, end := SantiagoDayBounds(date)
			if got := start.UTC().Format(time.RFC3339); got != tt.startUTC {
				t.Errorf("start = %s, want %s", got, tt.startUTC)
			}
			if got := end.UTC().Format(time.RFC3339); got != tt.endUTC {
				t.Errorf("end = %s, want %s", got, tt.endUTC)
			}
			if got := start.In(GetSantiagoLocation()).Hour(); got != tt.localHour {
				t.Errorf("local start hour = %d, want %d", got, tt.localHour)
			}
			if got := SantiagoDateStart(date); !got.Equal(start) {
				t.Errorf("SantiagoDateStart() = %s, want %s", got, start)
			}
		})
	}
}

func TestParseSantiagoDatePreservesDSTTransitionDate(t *testing.T) {
	date, err := ParseSantiagoDate("2026-09-06")
	if err != nil {
		t.Fatalf("ParseSantiagoDate() error = %v", err)
	}

	if got := date.In(GetSantiagoLocation()).Format("2006-01-02"); got != "2026-09-06" {
		t.Fatalf("date = %s, want 2026-09-06", got)
	}
}

func TestNormalizeScheduleAcrossOffsets(t *testing.T) {
	tests := []struct {
		date         string
		hour         int
		minutes      int
		scheduledUTC string
	}{
		{date: "2026-07-15", hour: 22, minutes: 30, scheduledUTC: "2026-07-16T02:30:00Z"},
		{date: "2026-09-06", hour: 22, minutes: 0, scheduledUTC: "2026-09-07T01:00:00Z"},
	}

	for _, tt := range tests {
		t.Run(tt.date, func(t *testing.T) {
			date, err := ParseSantiagoDate(tt.date)
			if err != nil {
				t.Fatal(err)
			}

			canonicalDate, localDate, timezone, scheduledAt, err := NormalizeSchedule(date, tt.hour, tt.minutes)
			if err != nil {
				t.Fatalf("NormalizeSchedule() error = %v", err)
			}
			if localDate != tt.date {
				t.Errorf("localDate = %s, want %s", localDate, tt.date)
			}
			if timezone != SantiagoTimezone {
				t.Errorf("timezone = %s, want %s", timezone, SantiagoTimezone)
			}
			if got := scheduledAt.UTC().Format(time.RFC3339); got != tt.scheduledUTC {
				t.Errorf("scheduledAt = %s, want %s", got, tt.scheduledUTC)
			}
			if !canonicalDate.Equal(SantiagoDateStart(date)) {
				t.Errorf("canonicalDate = %s, want %s", canonicalDate, SantiagoDateStart(date))
			}
		})
	}
}

func TestNormalizeScheduleRejectsSkippedLocalTime(t *testing.T) {
	date, err := ParseSantiagoDate("2026-09-06")
	if err != nil {
		t.Fatal(err)
	}

	_, _, _, _, err = NormalizeSchedule(date, 0, 30)
	if err == nil {
		t.Fatal("NormalizeSchedule() error = nil, want skipped local time error")
	}
	if _, ok := err.(*NonexistentLocalTimeError); !ok {
		t.Fatalf("NormalizeSchedule() error = %T, want *NonexistentLocalTimeError", err)
	}
}

func TestBookingNormalizeScheduleOverwritesClientDerivedFields(t *testing.T) {
	date, err := ParseSantiagoDate("2026-09-06")
	if err != nil {
		t.Fatal(err)
	}
	booking := Booking{
		Date:        date,
		Hour:        22,
		Minutes:     0,
		LocalDate:   "incorrect",
		Timezone:    "UTC",
		ScheduledAt: time.Unix(0, 0),
	}

	if err := booking.NormalizeSchedule(); err != nil {
		t.Fatal(err)
	}
	if booking.LocalDate != "2026-09-06" || booking.Timezone != SantiagoTimezone || booking.ScheduledAt.UTC().Format(time.RFC3339) != "2026-09-07T01:00:00Z" {
		t.Fatalf("booking fields were not normalized: %+v", booking)
	}
}
