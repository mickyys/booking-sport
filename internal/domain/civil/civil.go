// Package civil converts between civil (wall-clock) times and absolute
// instants for an arbitrary IANA timezone.
//
// It exists because the booking domain historically hardcoded America/Santiago.
// Every function takes an explicit *time.Location so new sport centers can use
// their own timezone without duplicating DST logic.
//
// The conversion is generic: it does not assume one-hour DST transitions or
// that a transition happens at midnight. It enumerates the UTC offsets in
// effect around the target civil date and keeps the instants whose local
// representation matches the requested wall clock.
package civil

import (
	"fmt"
	"sort"
	"time"
)

// DateLayout is the persisted civil date format (YYYY-MM-DD).
const DateLayout = "2006-01-02"

// NonexistentLocalTimeError reports a wall-clock time skipped by a timezone
// transition (for example 00:00 in America/Santiago on 2026-09-06).
type NonexistentLocalTimeError struct {
	Date     string
	Hour     int
	Minutes  int
	Timezone string
}

func (e *NonexistentLocalTimeError) Error() string {
	return fmt.Sprintf("the local time %s %02d:%02d does not exist in %s because of a timezone transition",
		e.Date, e.Hour, e.Minutes, e.Timezone)
}

// AmbiguousLocalTimeError reports a wall-clock time that occurs twice because
// the clock was moved back.
type AmbiguousLocalTimeError struct {
	Date     string
	Hour     int
	Minutes  int
	Timezone string
}

func (e *AmbiguousLocalTimeError) Error() string {
	return fmt.Sprintf("the local time %s %02d:%02d occurs twice in %s because of a timezone transition",
		e.Date, e.Hour, e.Minutes, e.Timezone)
}

// CivilDate returns noon of the civil date of t in loc. Noon is used as a
// stable carrier for year/month/day because midnight may not exist on DST
// transition days.
func CivilDate(t time.Time, loc *time.Location) time.Time {
	local := t.In(loc)
	return time.Date(local.Year(), local.Month(), local.Day(), 12, 0, 0, 0, loc)
}

// ParseDate parses a YYYY-MM-DD civil date into noon of that date in loc.
func ParseDate(value string, loc *time.Location) (time.Time, error) {
	date, err := time.Parse(DateLayout, value)
	if err != nil {
		return time.Time{}, err
	}
	return time.Date(date.Year(), date.Month(), date.Day(), 12, 0, 0, 0, loc), nil
}

// DayStart returns the first valid instant of the civil date of date in loc.
// On a day where midnight is skipped, this is the first existing local time.
func DayStart(date time.Time, loc *time.Location) time.Time {
	civilDate := CivilDate(date, loc)
	year, month, day := civilDate.Date()

	for minute := 0; minute < 24*60; minute++ {
		candidates := candidatesOf(year, month, day, minute/60, minute%60, loc)
		if len(candidates) > 0 {
			return candidates[0]
		}
	}
	// Unreachable for real IANA zones: at most a couple of hours are skipped.
	return time.Date(year, month, day, 0, 0, 0, 0, loc)
}

// DayBounds returns the first instant of the civil date of date and the first
// instant of the following civil date.
func DayBounds(date time.Time, loc *time.Location) (time.Time, time.Time) {
	civilDate := CivilDate(date, loc)
	nextCivilDate := civilDate.AddDate(0, 0, 1)
	return DayStart(civilDate, loc), DayStart(nextCivilDate, loc)
}

// DateStart is the canonical persisted value for a civil date: the first valid
// instant of that date. It matches the lower bound used by date range queries.
func DateStart(date time.Time, loc *time.Location) time.Time {
	return DayStart(date, loc)
}

// MatchInstant rebuilds the absolute instant of a wall clock in loc.
//
// When the wall clock is ambiguous it returns the first occurrence; callers
// that accept user input should use Normalize, which rejects ambiguity.
func MatchInstant(date time.Time, hour, minutes int, loc *time.Location) time.Time {
	civilDate := CivilDate(date, loc)
	candidates := candidatesOf(civilDate.Year(), civilDate.Month(), civilDate.Day(), hour, minutes, loc)
	if len(candidates) > 0 {
		return candidates[0]
	}
	// Nonexistent wall clock: time.Date normalizes forward, which is the best
	// available interpretation for read paths.
	return time.Date(civilDate.Year(), civilDate.Month(), civilDate.Day(), hour, minutes, 0, 0, loc)
}

// Normalize converts a civil date plus wall clock into its canonical
// representations. It rejects local times that do not exist or that occur
// twice.
func Normalize(date time.Time, hour, minutes int, loc *time.Location) (
	canonicalDate time.Time,
	localDate string,
	scheduledAt time.Time,
	err error,
) {
	if hour < 0 || hour > 23 || minutes < 0 || minutes > 59 {
		return time.Time{}, "", time.Time{}, fmt.Errorf("invalid local time %02d:%02d", hour, minutes)
	}

	civilDate := CivilDate(date, loc)
	candidates := candidatesOf(civilDate.Year(), civilDate.Month(), civilDate.Day(), hour, minutes, loc)
	dateLabel := civilDate.Format(DateLayout)

	switch len(candidates) {
	case 0:
		return time.Time{}, "", time.Time{}, &NonexistentLocalTimeError{
			Date: dateLabel, Hour: hour, Minutes: minutes, Timezone: loc.String(),
		}
	case 2:
		return time.Time{}, "", time.Time{}, &AmbiguousLocalTimeError{
			Date: dateLabel, Hour: hour, Minutes: minutes, Timezone: loc.String(),
		}
	}

	return DayStart(civilDate, loc), dateLabel, candidates[0], nil
}

// candidatesOf returns every instant in loc whose local representation is the
// requested wall clock, sorted ascending. Zero means nonexistent, two means
// ambiguous. More than two is not expected for IANA zones but is handled.
func candidatesOf(year int, month time.Month, day, hour, minutes int, loc *time.Location) []time.Time {
	// Wall clock encoded as if it were UTC. Subtracting an offset yields a
	// candidate instant.
	wall := time.Date(year, month, day, hour, minutes, 0, 0, time.UTC)

	offsets := map[int]struct{}{}
	reference := time.Date(year, month, day, 12, 0, 0, 0, loc)
	// Sampling every 12 hours across ±2 days captures any transition that
	// could affect this civil date, regardless of its magnitude or direction.
	for k := -4; k <= 4; k++ {
		_, offset := reference.Add(time.Duration(k) * 12 * time.Hour).Zone()
		offsets[offset] = struct{}{}
	}

	var out []time.Time
	for offset := range offsets {
		candidate := wall.Add(-time.Duration(offset) * time.Second)
		local := candidate.In(loc)
		localYear, localMonth, localDay := local.Date()
		localHour, localMinute, localSecond := local.Clock()
		if localYear != year || localMonth != month || localDay != day ||
			localHour != hour || localMinute != minutes || localSecond != 0 {
			continue
		}
		out = append(out, candidate)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Before(out[j]) })
	return out
}
