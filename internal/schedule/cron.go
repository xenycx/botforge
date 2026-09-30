// Package schedule parses five-field cron specifications (minute hour
// day-of-month month day-of-week) and computes run times in a time zone.
//
// Times are computed on the wall clock of the zone: a time that does not exist
// (the hour skipped when clocks spring forward) is skipped, and a time that
// happens twice (when clocks fall back) runs once, at its first occurrence.
package schedule

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Spec is a parsed cron specification.
type Spec struct {
	min, hour, dom, month, dow uint64 // bit sets
	domStar, dowStar           bool
	src                        string
}

// String returns the normalized source.
func (s Spec) String() string { return s.src }

var fieldBounds = [5][2]int{{0, 59}, {0, 23}, {1, 31}, {1, 12}, {0, 7}}

var dayNames = map[string]int{"sun": 0, "mon": 1, "tue": 2, "wed": 3, "thu": 4, "fri": 5, "sat": 6}
var monthNames = map[string]int{"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6, "jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12}

// Parse accepts "m h dom mon dow" with *, lists (1,2), ranges (1-5), steps
// (*/15, 0-30/5) and day/month names, plus @hourly, @daily, @weekly, @monthly.
func Parse(s string) (Spec, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "@hourly":
		s = "0 * * * *"
	case "@daily", "@midnight":
		s = "0 0 * * *"
	case "@weekly":
		s = "0 0 * * 0"
	case "@monthly":
		s = "0 0 1 * *"
	}
	f := strings.Fields(s)
	if len(f) != 5 {
		return Spec{}, errors.New("a schedule has five fields: minute hour day month weekday")
	}
	var sp Spec
	var sets [5]uint64
	for i, field := range f {
		names := map[string]int(nil)
		if i == 3 {
			names = monthNames
		} else if i == 4 {
			names = dayNames
		}
		set, err := parseField(field, fieldBounds[i][0], fieldBounds[i][1], names)
		if err != nil {
			return Spec{}, fmt.Errorf("%s: %w", []string{"minute", "hour", "day", "month", "weekday"}[i], err)
		}
		sets[i] = set
	}
	if sets[4]&(1<<7) != 0 { // 7 is Sunday too
		sets[4] = sets[4]&^(1<<7) | 1
	}
	sp.min, sp.hour, sp.dom, sp.month, sp.dow = sets[0], sets[1], sets[2], sets[3], sets[4]
	sp.domStar, sp.dowStar = f[2] == "*", f[4] == "*"
	sp.src = strings.Join(f, " ")
	return sp, nil
}

func parseField(field string, lo, hi int, names map[string]int) (uint64, error) {
	var set uint64
	for _, part := range strings.Split(field, ",") {
		step := 1
		if base, st, ok := strings.Cut(part, "/"); ok {
			n, err := strconv.Atoi(st)
			if err != nil || n < 1 || n > hi {
				return 0, fmt.Errorf("invalid step %q", st)
			}
			part, step = base, n
		}
		a, b := lo, hi
		if part != "*" {
			x, y, isRange := strings.Cut(part, "-")
			var err error
			if a, err = value(x, names, lo, hi); err != nil {
				return 0, err
			}
			b = a
			if isRange {
				if b, err = value(y, names, lo, hi); err != nil {
					return 0, err
				}
				if b < a {
					return 0, fmt.Errorf("range %q goes backwards", part)
				}
			} else if step > 1 {
				b = hi // "5/15" means from 5 to the end, every 15
			}
		}
		for v := a; v <= b; v += step {
			set |= 1 << uint(v)
		}
	}
	return set, nil
}

func value(s string, names map[string]int, lo, hi int) (int, error) {
	if n, ok := names[s]; ok {
		return n, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < lo || n > hi {
		return 0, fmt.Errorf("%q is out of range %d-%d", s, lo, hi)
	}
	return n, nil
}

func has(set uint64, v int) bool { return set&(1<<uint(v)) != 0 }

// dayMatches applies cron's rule: when both day fields are restricted, either
// may match.
func (s Spec) dayMatches(t time.Time) bool {
	d, w := has(s.dom, t.Day()), has(s.dow, int(t.Weekday()))
	switch {
	case s.domStar && s.dowStar:
		return true
	case s.domStar:
		return w
	case s.dowStar:
		return d
	}
	return d || w
}

// Next returns the first run time strictly after `after`, evaluated on the
// wall clock of loc. It searches at most five years ahead and returns the zero
// time when nothing matches (for example February 30).
func (s Spec) Next(after time.Time, loc *time.Location) time.Time {
	w := after.In(loc)
	// Start at the next whole wall-clock minute.
	y, mo, d, h, mi := w.Year(), w.Month(), w.Day(), w.Hour(), w.Minute()+1
	limit := w.Year() + 5
	for y <= limit {
		t := wall(y, mo, d, h, mi, loc)
		// Normalize wall-clock fields (minute overflow etc.) without DST effects.
		y, mo, d, h, mi = t.y, t.mo, t.d, t.h, t.mi
		switch {
		case !has(s.month, int(mo)):
			y, mo, d, h, mi = nextMonth(y, mo)
		case !s.dayMatches(time.Date(y, mo, d, 12, 0, 0, 0, time.UTC)):
			d, h, mi = d+1, 0, 0
		case !has(s.hour, h):
			h, mi = h+1, 0
		case !has(s.min, mi):
			mi++
		default:
			at := time.Date(y, mo, d, h, mi, 0, 0, loc)
			// A skipped wall time normalizes to a different hour or minute: skip it.
			if at.Hour() != h || at.Minute() != mi || !at.After(after) {
				mi++
				continue
			}
			return at
		}
	}
	return time.Time{}
}

type wallTime struct {
	y        int
	mo       time.Month
	d, h, mi int
}

// wall normalizes wall-clock fields in UTC (no DST gaps), so overflowed
// minutes and hours roll over like a calendar, not like instants.
func wall(y int, mo time.Month, d, h, mi int, _ *time.Location) wallTime {
	t := time.Date(y, mo, d, h, mi, 0, 0, time.UTC)
	return wallTime{t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute()}
}

func nextMonth(y int, mo time.Month) (int, time.Month, int, int, int) {
	if mo == 12 {
		return y + 1, 1, 1, 0, 0
	}
	return y, mo + 1, 1, 0, 0
}

// NextN returns up to n upcoming run times.
func (s Spec) NextN(after time.Time, loc *time.Location, n int) []time.Time {
	var out []time.Time
	t := after
	for len(out) < n {
		t = s.Next(t, loc)
		if t.IsZero() {
			break
		}
		out = append(out, t)
	}
	return out
}
