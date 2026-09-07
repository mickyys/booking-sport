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
