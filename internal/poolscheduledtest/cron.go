// Package poolscheduledtest implements pool scheduled connectivity test
// plans (guide card B4-#11): narrow-grammar cron parsing, plan CRUD and the
// periodic runner that tests due plans and optionally recovers accounts.
package poolscheduledtest

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Schedule is the parsed form of a supported cron expression.
//
// Supported grammar (narrowed per the execution decision for B4-#11; a cron
// library is deliberately NOT used — octopus adds no new module dependencies):
//
//	minute  : * | */n (1<=n<=59) | fixed value 0-59
//	hour    : * | comma-separated fixed values 0-23
//	day/mon/dow: * only
//
// Examples: "*/30 * * * *" (every 30 minutes), "0 9,21 * * *" (09:00 and
// 21:00 every day), "15 * * * *" (minute 15 of every hour). Everything else
// is rejected by ParseCronExpr, and create/update routes enforce the same
// shape, so the runner never sees an unsupported expression.
type Schedule struct {
	allMinutes   bool
	stepMinutes  int          // >0 when the minute field is */n
	fixedMinutes map[int]bool // minute-field fixed values (narrow grammar: at most one)
	allHours     bool
	hours        map[int]bool
}

// ParseCronExpr parses and validates a cron expression of the narrowed
// grammar. It returns an error describing the first unsupported feature.
func ParseCronExpr(expr string) (*Schedule, error) {
	fields := strings.Fields(strings.TrimSpace(expr))
	if len(fields) != 5 {
		return nil, fmt.Errorf("cron expression must have 5 fields, got %d", len(fields))
	}

	s := &Schedule{fixedMinutes: map[int]bool{}, hours: map[int]bool{}}

	// Minute field.
	minute := fields[0]
	switch {
	case minute == "*":
		s.allMinutes = true
	case strings.HasPrefix(minute, "*/"):
		n, err := strconv.Atoi(strings.TrimPrefix(minute, "*/"))
		if err != nil || n < 1 || n > 59 {
			return nil, fmt.Errorf("unsupported cron minute step %q (want */n with 1<=n<=59)", minute)
		}
		s.stepMinutes = n
	default:
		v, err := strconv.Atoi(minute)
		if err != nil || v < 0 || v > 59 {
			return nil, fmt.Errorf("unsupported cron minute value %q", minute)
		}
		s.fixedMinutes[v] = true
	}

	// Hour field: * or a comma-separated list of fixed values.
	hour := fields[1]
	if hour == "*" {
		s.allHours = true
	} else {
		for _, part := range strings.Split(hour, ",") {
			v, err := strconv.Atoi(strings.TrimSpace(part))
			if err != nil || v < 0 || v > 23 {
				return nil, fmt.Errorf("unsupported cron hour value %q", part)
			}
			s.hours[v] = true
		}
	}

	// Day-of-month, month and day-of-week must be a plain "*".
	for i, name := range []string{"day-of-month", "month", "day-of-week"} {
		if fields[2+i] != "*" {
			return nil, fmt.Errorf("unsupported cron %s field %q (only * is supported)", name, fields[2+i])
		}
	}
	return s, nil
}

// ValidateCronExpr reports whether expr fits the supported grammar.
func ValidateCronExpr(expr string) error {
	_, err := ParseCronExpr(expr)
	return err
}

// Next returns the first run instant strictly after from, resolved on the
// wall clock of from's location (DST gaps simply never match). from is
// truncated to the next minute boundary first, so a run recorded at exactly
// `from` is not returned again. The narrowed grammar guarantees at least one
// matching minute per day, so the bounded scan (8 days) always finds one for
// any expression that passed ParseCronExpr.
func (s *Schedule) Next(from time.Time) time.Time {
	t := from.Truncate(time.Minute).Add(time.Minute)
	limit := t.Add(8 * 24 * time.Hour)
	for ; t.Before(limit); t = t.Add(time.Minute) {
		if !s.hourMatches(t.Hour()) {
			continue
		}
		if !s.minuteMatches(t.Minute()) {
			continue
		}
		return t
	}
	return time.Time{}
}

func (s *Schedule) hourMatches(hour int) bool {
	return s.allHours || s.hours[hour]
}

func (s *Schedule) minuteMatches(minute int) bool {
	switch {
	case s.allMinutes:
		return true
	case s.stepMinutes > 0:
		return minute%s.stepMinutes == 0
	default:
		return s.fixedMinutes[minute]
	}
}
