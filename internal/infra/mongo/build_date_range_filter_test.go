package mongo

import (
	"testing"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
)

func TestBuildDateRangeFilterCoversFullWeek(t *testing.T) {
	start, err := domain.ParseSantiagoDate("2026-09-07")
	if err != nil {
		t.Fatal(err)
	}
	end, err := domain.ParseSantiagoDate("2026-09-13")
	if err != nil {
		t.Fatal(err)
	}

	f := buildDateRangeFilter(start, end)
	lo := f["$gte"].(time.Time)
	hi := f["$lt"].(time.Time)

	// Range must be [Monday 2026-09-07 00:00 CHILE, Monday 2026-09-14 00:00 CHILE)
	if got := lo.UTC().Format(time.RFC3339); got != "2026-09-07T03:00:00Z" {
		t.Errorf("lower = %s, want 2026-09-07T03:00:00Z (00:00 lunes)", got)
	}
	if got := hi.UTC().Format(time.RFC3339); got != "2026-09-14T03:00:00Z" {
		t.Errorf("upper = %s, want 2026-09-14T03:00:00Z (00:00 del día siguiente al domingo)", got)
	}
}

func TestBuildDateRangeFilterIncludesCanonicalBooking(t *testing.T) {
	start, _ := domain.ParseSantiagoDate("2026-09-07")
	end, _ := domain.ParseSantiagoDate("2026-09-13")
	f := buildDateRangeFilter(start, end)

	// The stored booking date is the canonical start-of-day instant:
	// ISODate('2026-09-07T03:00:00.000Z') == 2026-09-07 00:00 America/Santiago.
	bookingDate := time.Date(2026, 9, 7, 0, 0, 0, 0, domain.GetSantiagoLocation())
	t.Logf("booking date as stored: %s", bookingDate.UTC().Format(time.RFC3339))

	match := matchesRange(bookingDate, f)
	if !match {
		t.Errorf("booking at %s should fall inside range [%s, %s)",
			bookingDate.UTC().Format(time.RFC3339),
			f["$gte"].(time.Time).UTC().Format(time.RFC3339),
			f["$lt"].(time.Time).UTC().Format(time.RFC3339))
	}
}

func matchesRange(v time.Time, r map[string]interface{}) bool {
	lo := r["$gte"].(time.Time)
	hi := r["$lt"].(time.Time)
	return !v.Before(lo) && v.Before(hi)
}