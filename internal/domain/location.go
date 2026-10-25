package domain

import (
	"fmt"
	"time"

	"github.com/hamp/booking-sport/internal/domain/civil"
)

const civilDateLayout = civil.DateLayout

const SantiagoTimezone = "America/Santiago"

// GetSantiagoLocation returns the America/Santiago location (Chile, DST-aware).
// It falls back to UTC when the timezone database is not available in the
// runtime. Production binaries embed time/tzdata, so the fallback is a last
// resort for exotic builds only.
func GetSantiagoLocation() *time.Location {
	loc, err := time.LoadLocation(SantiagoTimezone)
	if err != nil {
		return time.UTC
	}
	return loc
}

// LoadTimezone validates an IANA timezone name. Unlike GetSantiagoLocation it
// never falls back: an invalid zone is an error, because silently using UTC
// would shift every booking of that center.
func LoadTimezone(name string) (*time.Location, error) {
	if name == "" {
		return nil, fmt.Errorf("timezone is required")
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, fmt.Errorf("invalid timezone %q: %w", name, err)
	}
	return loc, nil
}

// NonexistentLocalTimeError reports a wall-clock time skipped by a DST
// transition.
type NonexistentLocalTimeError = civil.NonexistentLocalTimeError

// AmbiguousLocalTimeError reports a wall-clock time that occurs twice because
// the clock was moved back.
type AmbiguousLocalTimeError = civil.AmbiguousLocalTimeError

// NormalizeSchedule derives every persisted representation of a local slot in
// America/Santiago. It rejects local times skipped or repeated by a timezone
// transition.
func NormalizeSchedule(date time.Time, hour, minutes int) (canonicalDate time.Time, localDate string, timezone string, scheduledAt time.Time, err error) {
	canonicalDate, localDate, scheduledAt, err = civil.Normalize(date, hour, minutes, GetSantiagoLocation())
	if err != nil {
		return time.Time{}, "", "", time.Time{}, err
	}
	return canonicalDate, localDate, SantiagoTimezone, scheduledAt, nil
}

// ParseSantiagoDate parses a calendar date without requiring local midnight to
// exist. Chile's DST transition can skip midnight on some dates.
func ParseSantiagoDate(value string) (time.Time, error) {
	return civil.ParseDate(value, GetSantiagoLocation())
}

// ParseDate parses a YYYY-MM-DD civil date in the given timezone.
func ParseDate(value string, loc *time.Location) (time.Time, error) {
	if loc == nil {
		return time.Time{}, fmt.Errorf("timezone is required")
	}
	return civil.ParseDate(value, loc)
}

// SantiagoCivilDate represents a Santiago calendar date at noon. Noon is used
// as a stable carrier for year/month/day across DST transitions.
func SantiagoCivilDate(date time.Time) time.Time {
	return civil.CivilDate(date, GetSantiagoLocation())
}

// SantiagoDayBounds returns the first valid instant of a calendar date and the
// first valid instant of the following date. On 2026-09-06, for example, the
// first valid local time is 01:00 because Chile skips midnight.
func SantiagoDayBounds(date time.Time) (time.Time, time.Time) {
	return civil.DayBounds(date, GetSantiagoLocation())
}

// SantiagoDateStart is the canonical persisted value for a Santiago calendar
// date. It matches the lower bound used by date queries and unique indexes.
func SantiagoDateStart(date time.Time) time.Time {
	return civil.DateStart(date, GetSantiagoLocation())
}

// MatchInstant rebuilds the absolute instant of a booking from its civil date,
// hour and minute in America/Santiago. time.Date resolves the UTC offset for
// the exact calendar day, so it is DST-safe (including transition days where
// Chile moves midnight between -03:00 and -04:00).
func MatchInstant(date time.Time, hour, minutes int) time.Time {
	return civil.MatchInstant(date, hour, minutes, GetSantiagoLocation())
}

// StartInstant returns the canonical start instant of a booking.
//
// Documents written during the UTC migration carry scheduled_at (with the
// minutes included). Legacy documents without it are resolved from the civil
// date, hour and minutes in the center timezone. Using this helper avoids
// reconstructing the start with a truncated minute, which previously allowed
// cancelling a booking that had already started.
func (b *Booking) StartInstant() time.Time {
	if !b.ScheduledAt.IsZero() {
		return b.ScheduledAt.UTC()
	}
	return MatchInstant(b.Date, b.Hour, b.Minutes).UTC()
}

// DurationOrDefault returns the booking duration in minutes. Legacy documents
// without duration_minutes are interpreted as one hour, matching the previous
// behavior.
func (b *Booking) DurationOrDefault() int {
	if b.DurationMinutes > 0 {
		return b.DurationMinutes
	}
	return 60
}

// EndInstant returns the exclusive end of the booking interval.
func (b *Booking) EndInstant() time.Time {
	if !b.EndsAt.IsZero() {
		return b.EndsAt.UTC()
	}
	return b.StartInstant().Add(time.Duration(b.DurationOrDefault()) * time.Minute)
}

// ChargedPrice devuelve el monto efectivamente cobrado de una reserva. Las
// reservas creadas antes de los descuentos no tienen final_price: en ese caso
// se usa el precio lista para no tratar el cobro como cero. El snapshot de
// precios distingue ese caso legacy de un descuento real del 100%.
func (b *Booking) ChargedPrice() float64 {
	if b == nil {
		return 0
	}
	if b.Pricing != nil || b.FinalPrice > 0 {
		return b.FinalPrice
	}
	return b.Price
}
