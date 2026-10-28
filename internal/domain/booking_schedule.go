package domain

import (
	"fmt"
	"time"

	"github.com/hamp/booking-sport/internal/domain/civil"
)

// ScheduleSchemaVersionUTC is the temporal contract introduced by the UTC
// migration: scheduled_at and ends_at are canonical, and date/hour/minutes are
// kept only as a compatibility projection.
const ScheduleSchemaVersionUTC = 2

// MaxBookingDuration bounds the interval that a single booking may occupy. It
// is used to limit legacy occupancy scans and, later, the Redis candidate
// range (ZRANGEBYSCORE lower bound).
const MaxBookingDuration = 24 * time.Hour

// TimezoneLocation resolves the IANA timezone of the center.
//
// Historic centers have no timezone stored yet; they are interpreted as
// America/Santiago (see docs/README-reservas-utc-redis.md). An explicitly
// configured but invalid zone is an error: silently falling back to UTC would
// shift every booking of that center.
func (sc *SportCenter) TimezoneLocation() (*time.Location, error) {
	if sc.Timezone == "" {
		return GetSantiagoLocation(), nil
	}
	return LoadTimezone(sc.Timezone)
}

// PrepareForWrite validates and completes the temporal representation of a
// booking before it is persisted.
//
// Two contracts are accepted:
//
//   - UTC (v2): scheduled_at is present. date/hour/minutes/local_date are
//     validated against it when provided and derived when omitted. Seconds
//     must be zero, and the duration must be positive.
//   - Legacy: date + hour + minutes. The wall clock is resolved in the center
//     timezone and the document is upgraded to v2.
//
// Contradictory inputs are rejected instead of silently overwritten.
func (b *Booking) PrepareForWrite(defaultLocation *time.Location) error {
	loc := defaultLocation
	if loc == nil {
		loc = GetSantiagoLocation()
	}
	if b.Timezone != "" {
		parsed, err := LoadTimezone(b.Timezone)
		if err != nil {
			return err
		}
		loc = parsed
	}

	if !b.ScheduledAt.IsZero() || b.ScheduleSchemaVersion >= ScheduleSchemaVersionUTC {
		if err := b.prepareUTC(loc); err != nil {
			return err
		}
	} else {
		if err := b.prepareLegacy(loc); err != nil {
			return err
		}
	}

	b.ScheduleSchemaVersion = ScheduleSchemaVersionUTC
	b.Timezone = loc.String()
	b.DurationMinutes = b.DurationOrDefault()
	b.EndsAt = b.ScheduledAt.UTC().
		Add(time.Duration(b.DurationMinutes) * time.Minute)
	if b.SpaceCategory.IsFacility() {
		// Un día completo termina en la próxima medianoche local: en días con
		// cambio de horario (23/25 h) sumar 24 h solaparía o dejaría un hueco
		// con la reserva del día siguiente.
		local := b.ScheduledAt.In(loc)
		b.EndsAt = time.Date(local.Year(), local.Month(), local.Day()+1, 0, 0, 0, 0, loc).UTC()
	}

	return nil
}

func (b *Booking) prepareUTC(loc *time.Location) error {
	if b.ScheduledAt.IsZero() {
		return fmt.Errorf("scheduled_at is required for schedule_schema_version %d", ScheduleSchemaVersionUTC)
	}
	if b.ScheduledAt.Second() != 0 || b.ScheduledAt.Nanosecond() != 0 {
		return fmt.Errorf("scheduled_at must not include seconds")
	}
	if b.DurationMinutes <= 0 {
		// New contract (only scheduled_at) must state the duration. Legacy
		// callers that already resolved date/hour/minutes default to 60.
		if b.Date.IsZero() {
			return fmt.Errorf("duration_minutes must be greater than zero")
		}
		b.DurationMinutes = 60
	}

	start := b.ScheduledAt.UTC()
	local := start.In(loc)
	expectedDate := civil.DateStart(start, loc)
	expectedLocalDate := local.Format(civil.DateLayout)

	if b.Date.IsZero() {
		// New contract: only scheduled_at + duration_minutes were sent. Any
		// compatibility field that *is* present must still be coherent.
		if b.LocalDate != "" && b.LocalDate != expectedLocalDate {
			return fmt.Errorf("local_date %q contradicts scheduled_at (expected %q)", b.LocalDate, expectedLocalDate)
		}
		b.Date = expectedDate
		b.LocalDate = expectedLocalDate
		b.Hour = local.Hour()
		b.Minutes = local.Minute()
		return nil
	}

	if !sameSecond(b.Date, expectedDate) {
		return fmt.Errorf("date %s contradicts scheduled_at %s in %s",
			b.Date.UTC().Format(time.RFC3339), start.Format(time.RFC3339), loc)
	}
	if b.LocalDate != "" && b.LocalDate != expectedLocalDate {
		return fmt.Errorf("local_date %q contradicts scheduled_at (expected %q)", b.LocalDate, expectedLocalDate)
	}
	if b.Hour != local.Hour() || b.Minutes != local.Minute() {
		return fmt.Errorf("hour/minutes %02d:%02d contradict scheduled_at (expected %02d:%02d in %s)",
			b.Hour, b.Minutes, local.Hour(), local.Minute(), loc)
	}

	b.LocalDate = expectedLocalDate
	return nil
}

func (b *Booking) prepareLegacy(loc *time.Location) error {
	if b.Date.IsZero() {
		return fmt.Errorf("date is required when scheduled_at is not provided")
	}
	if b.Hour < 0 || b.Hour > 23 || b.Minutes < 0 || b.Minutes > 59 {
		return fmt.Errorf("invalid local time %02d:%02d", b.Hour, b.Minutes)
	}

	canonicalDate, localDate, scheduledAt, err := civil.Normalize(b.Date, b.Hour, b.Minutes, loc)
	if err != nil {
		return err
	}

	b.Date = canonicalDate
	b.LocalDate = localDate
	b.ScheduledAt = scheduledAt.UTC()
	return nil
}

// sameSecond compares two instants at second precision, which is the
// resolution persisted by MongoDB for dates.
func sameSecond(a, b time.Time) bool {
	return a.UTC().Truncate(time.Second).Equal(b.UTC().Truncate(time.Second))
}
