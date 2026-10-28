package domain

import (
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

func TestFacilityBookingEndsAtNextLocalMidnight(t *testing.T) {
	loc := GetSantiagoLocation()
	cases := []string{"2026-01-15", "2026-09-05", "2026-04-04"}
	for _, day := range cases {
		d, err := time.ParseInLocation("2006-01-02", day, loc)
		if err != nil {
			t.Fatal(err)
		}
		b := &Booking{
			CourtID:         primitive.NewObjectID(),
			Date:            d,
			Hour:            0,
			Minutes:         0,
			DurationMinutes: 1440,
			SpaceCategory:   SpaceCategoryPiscina,
		}
		if err := b.PrepareForWrite(nil); err != nil {
			t.Fatalf("%s: %v", day, err)
		}
		want := time.Date(d.Year(), d.Month(), d.Day()+1, 0, 0, 0, 0, loc).UTC()
		if !b.EndsAt.Equal(want) {
			t.Errorf("%s: ends_at = %s, want %s", day, b.EndsAt, want)
		}
		if b.DurationMinutes != 1440 {
			t.Errorf("%s: duration = %d, want 1440", day, b.DurationMinutes)
		}
	}
}
