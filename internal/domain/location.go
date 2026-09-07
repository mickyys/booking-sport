package domain

import "time"

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

// MatchInstant rebuilds the absolute instant of a booking from its civil date,
// hour and minute in America/Santiago. time.Date resolves the UTC offset for
// the exact calendar day, so it is DST-safe (including transition days where
// Chile moves midnight between -03:00 and -04:00).
func MatchInstant(date time.Time, hour, minutes int) time.Time {
	loc := GetSantiagoLocation()
	d := date.In(loc)
	return time.Date(d.Year(), d.Month(), d.Day(), hour, minutes, 0, 0, loc)
}