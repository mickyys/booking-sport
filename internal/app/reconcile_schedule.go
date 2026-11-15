package app

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ReconcileSchedule defines the local hours at which the occupancy reconciler
// runs. It is intentionally simple: every run happens at minute 0 of the
// configured hours. Times are resolved in a fixed IANA location so the process
// timezone never affects the schedule.
type ReconcileSchedule struct {
	location *time.Location
	allowed  map[int]bool
	hours    []int
}

// ParseReconcileSchedule parses a spec such as "06:00,09:00-22:00".
//
// Each comma-separated part is either a single hour ("06" or "06:00") or an
// inclusive range ("09:00-22:00"). Only minute 00 is supported because the
// reconciler runs on the hour. Duplicates are ignored and the resulting hours
// are sorted.
func ParseReconcileSchedule(spec string, loc *time.Location) (ReconcileSchedule, error) {
	if loc == nil {
		return ReconcileSchedule{}, errors.New("reconcile schedule: timezone is required")
	}

	allowed := make(map[int]bool)
	for _, raw := range strings.Split(spec, ",") {
		part := strings.TrimSpace(raw)
		if part == "" {
			continue
		}

		if strings.Contains(part, "-") {
			bounds := strings.SplitN(part, "-", 2)
			from, err := parseReconcileHour(bounds[0])
			if err != nil {
				return ReconcileSchedule{}, fmt.Errorf("reconcile schedule %q: %w", spec, err)
			}
			to, err := parseReconcileHour(bounds[1])
			if err != nil {
				return ReconcileSchedule{}, fmt.Errorf("reconcile schedule %q: %w", spec, err)
			}
			if from > to {
				return ReconcileSchedule{}, fmt.Errorf("reconcile schedule %q: range %q starts after it ends", spec, part)
			}
			for h := from; h <= to; h++ {
				allowed[h] = true
			}
			continue
		}

		h, err := parseReconcileHour(part)
		if err != nil {
			return ReconcileSchedule{}, fmt.Errorf("reconcile schedule %q: %w", spec, err)
		}
		allowed[h] = true
	}

	if len(allowed) == 0 {
		return ReconcileSchedule{}, fmt.Errorf("reconcile schedule %q has no hours", spec)
	}

	hours := make([]int, 0, len(allowed))
	for h := range allowed {
		hours = append(hours, h)
	}
	sort.Ints(hours)

	return ReconcileSchedule{location: loc, allowed: allowed, hours: hours}, nil
}

func parseReconcileHour(raw string) (int, error) {
	token := strings.TrimSpace(raw)
	if token == "" {
		return 0, errors.New("empty hour")
	}
	if strings.Contains(token, ":") {
		parts := strings.SplitN(token, ":", 2)
		if parts[1] != "00" {
			return 0, fmt.Errorf("hour %q: only minute 00 is supported", token)
		}
		token = strings.TrimSpace(parts[0])
	}
	h, err := strconv.Atoi(token)
	if err != nil || h < 0 || h > 23 {
		return 0, fmt.Errorf("invalid hour %q", raw)
	}
	return h, nil
}

// Hours returns the configured local hours in ascending order.
func (s ReconcileSchedule) Hours() []int {
	out := make([]int, len(s.hours))
	copy(out, s.hours)
	return out
}

// Next returns the first scheduled instant strictly after after.
//
// It walks forward hour by hour in the configured location, so DST transitions
// are handled by the time package: skipped or repeated wall-clock hours simply
// never match an allowed hour (the configured windows are far from Chile's
// transitions). A non-empty schedule guarantees a result within 24 hours.
func (s ReconcileSchedule) Next(after time.Time) time.Time {
	local := after.In(s.location)
	candidate := time.Date(local.Year(), local.Month(), local.Day(), local.Hour(), 0, 0, 0, s.location)
	if !candidate.After(after) {
		candidate = candidate.Add(time.Hour)
	}

	// Upper bound: one full week, enough for any DST transition plus a sparse
	// schedule. With at least one allowed hour it always returns earlier.
	for i := 0; i < 24*8; i++ {
		if s.allowed[candidate.Hour()] {
			return candidate
		}
		candidate = candidate.Add(time.Hour)
	}

	// Defensive fallback: should never happen with a parsed schedule.
	return after.Add(24 * time.Hour)
}
