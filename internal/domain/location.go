package domain

import "time"

const civilDateLayout = "2006-01-02"

// GetSantiagoLocation returns the America/Santiago location (Chile, DST-aware).
// It falls back to UTC when the timezone database is not available in the
// runtime, mirroring the previous behavior of ignored LoadLocation errors.
func GetSantiagoLocation() *time.Location {
	loc, err := time.LoadLocation("America/Santiago")
	if err != nil {
		return time.UTC
	}
	return loc
}

// ParseSantiagoDate parses a calendar date without requiring local midnight to
// exist. Chile's DST transition can skip midnight on some dates.
func ParseSantiagoDate(value string) (time.Time, error) {
	date, err := time.Parse(civilDateLayout, value)
	if err != nil {
		return time.Time{}, err
	}
	return time.Date(date.Year(), date.Month(), date.Day(), 12, 0, 0, 0, GetSantiagoLocation()), nil
}

// SantiagoCivilDate represents a Santiago calendar date at noon. Noon is used
// as a stable carrier for year/month/day across DST transitions.
func SantiagoCivilDate(date time.Time) time.Time {
	loc := GetSantiagoLocation()
	dateCL := date.In(loc)
	return time.Date(dateCL.Year(), dateCL.Month(), dateCL.Day(), 12, 0, 0, 0, loc)
}

// SantiagoDayBounds returns the first valid instant of a calendar date and the
// first valid instant of the following date. On 2026-09-06, for example, the
// first valid local time is 01:00 because Chile skips midnight.
func SantiagoDayBounds(date time.Time) (time.Time, time.Time) {
	civilDate := SantiagoCivilDate(date)
	nextCivilDate := civilDate.AddDate(0, 0, 1)
	return firstInstantOfCivilDate(civilDate), firstInstantOfCivilDate(nextCivilDate)
}

// SantiagoDateStart is the canonical persisted value for a Santiago calendar
// date. It matches the lower bound used by date queries and unique indexes.
func SantiagoDateStart(date time.Time) time.Time {
	start, _ := SantiagoDayBounds(date)
	return start
}

func firstInstantOfCivilDate(civilDate time.Time) time.Time {
	loc := GetSantiagoLocation()
	year, month, day := civilDate.Date()
	instant := civilDate

	// Santiago's UTC offset changes on minute boundaries. Walking backwards
	// from noon avoids constructing a local time that may not exist.
	for {
		previous := instant.Add(-time.Minute).In(loc)
		if previous.Year() != year || previous.Month() != month || previous.Day() != day {
			return instant
		}
		instant = previous
	}
}

// MatchInstant rebuilds the absolute instant of a booking from its civil date,
// hour and minute in America/Santiago. time.Date resolves the UTC offset for
// the exact calendar day, so it is DST-safe (including transition days where
// Chile moves midnight between -03:00 and -04:00).
func MatchInstant(date time.Time, hour, minutes int) time.Time {
	loc := GetSantiagoLocation()
	d := date.In(loc)
	return time.Date(d.Year(), d.Month(), d.Day(), hour, minutes, 0, 0, loc)
}
