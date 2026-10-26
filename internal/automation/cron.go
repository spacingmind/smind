package automation

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// MinCronInterval is the shortest cadence a cron spec may have (ADR-0044
// section 2.1). A spec that can fire more often is rejected, not clamped.
const MinCronInterval = 5 * time.Minute

// Cron is a parsed five-field cron spec, evaluated in UTC.
type Cron struct {
	spec                    string
	min, hour, dom, mon, dw uint64
	domStar, dowStar        bool
}

var cronMacros = map[string]string{
	"@hourly": "0 * * * *",
	"@daily":  "0 0 * * *",
	"@weekly": "0 0 * * 0",
}

var (
	monthNames = map[string]int{"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6, "jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12}
	dayNames   = map[string]int{"sun": 0, "mon": 1, "tue": 2, "wed": 3, "thu": 4, "fri": 5, "sat": 6}
)

// ParseCron parses spec (five fields, or @hourly/@daily/@weekly) and rejects a
// spec whose cadence can be shorter than MinCronInterval.
func ParseCron(spec string) (*Cron, error) {
	spec = strings.TrimSpace(spec)
	if m, ok := cronMacros[spec]; ok {
		spec = m
	}
	f := strings.Fields(spec)
	if len(f) != 5 {
		return nil, fmt.Errorf("cron %q: want 5 fields (minute hour day-of-month month day-of-week)", spec)
	}
	c := &Cron{spec: strings.Join(f, " ")}
	var err error
	if c.min, _, err = parseCronField(f[0], 0, 59, nil, "minute"); err != nil {
		return nil, err
	}
	if c.hour, _, err = parseCronField(f[1], 0, 23, nil, "hour"); err != nil {
		return nil, err
	}
	if c.dom, c.domStar, err = parseCronField(f[2], 1, 31, nil, "day-of-month"); err != nil {
		return nil, err
	}
	if c.mon, _, err = parseCronField(f[3], 1, 12, monthNames, "month"); err != nil {
		return nil, err
	}
	if c.dw, c.dowStar, err = parseCronField(f[4], 0, 7, dayNames, "day-of-week"); err != nil {
		return nil, err
	}
	if c.dw&(1<<7) != 0 { // 7 is Sunday too
		c.dw = c.dw&^(1<<7) | 1
	}
	if gap := c.minGap(); gap < MinCronInterval {
		return nil, fmt.Errorf("cron %q can fire every %s: the minimum cadence is %s", c.spec, gap, MinCronInterval)
	}
	return c, nil
}

// String returns the normalised spec.
func (c *Cron) String() string { return c.spec }

func parseCronField(s string, lo, hi int, names map[string]int, what string) (uint64, bool, error) {
	var set uint64
	star := s == "*"
	for _, part := range strings.Split(s, ",") {
		if part == "" {
			return 0, false, fmt.Errorf("cron %s %q: empty list item", what, s)
		}
		rng, step := part, 1
		if i := strings.IndexByte(part, '/'); i >= 0 {
			n, err := strconv.Atoi(part[i+1:])
			if err != nil || n < 1 {
				return 0, false, fmt.Errorf("cron %s %q: bad step", what, s)
			}
			rng, step = part[:i], n
		}
		from, to := lo, hi
		switch {
		case rng == "*":
		case strings.Contains(rng, "-"):
			a, b, _ := strings.Cut(rng, "-")
			var err error
			if from, err = cronValue(a, names, what); err != nil {
				return 0, false, err
			}
			if to, err = cronValue(b, names, what); err != nil {
				return 0, false, err
			}
		default:
			v, err := cronValue(rng, names, what)
			if err != nil {
				return 0, false, err
			}
			from, to = v, v
			if step != 1 || strings.Contains(part, "/") {
				to = hi // "5/15" means 5-max/15, standard cron
			}
		}
		if from < lo || to > hi || from > to {
			return 0, false, fmt.Errorf("cron %s %q: out of range %d-%d", what, s, lo, hi)
		}
		for v := from; v <= to; v += step {
			set |= 1 << uint(v)
		}
	}
	return set, star, nil
}

func cronValue(s string, names map[string]int, what string) (int, error) {
	if v, ok := names[strings.ToLower(s)]; ok {
		return v, nil
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("cron %s: %q is not a number", what, s)
	}
	return v, nil
}

func (c *Cron) dayMatches(t time.Time) bool {
	domOK := c.dom&(1<<uint(t.Day())) != 0
	dowOK := c.dw&(1<<uint(t.Weekday())) != 0
	switch {
	case c.domStar && c.dowStar:
		return true
	case c.domStar:
		return dowOK
	case c.dowStar:
		return domOK
	}
	return domOK || dowOK // both restricted: either matches (standard cron)
}

func (c *Cron) matches(t time.Time) bool {
	return c.min&(1<<uint(t.Minute())) != 0 && c.hour&(1<<uint(t.Hour())) != 0 &&
		c.mon&(1<<uint(t.Month())) != 0 && c.dayMatches(t)
}

// Next returns the first instant strictly after t (UTC, minute resolution)
// that matches the spec; ok is false if none exists in the next 8 years.
func (c *Cron) Next(t time.Time) (time.Time, bool) {
	t = t.UTC().Truncate(time.Minute).Add(time.Minute)
	limit := t.AddDate(8, 0, 0)
	for t.Before(limit) {
		if c.mon&(1<<uint(t.Month())) == 0 {
			t = time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, time.UTC)
			continue
		}
		if !c.dayMatches(t) {
			t = time.Date(t.Year(), t.Month(), t.Day()+1, 0, 0, 0, 0, time.UTC)
			continue
		}
		if c.hour&(1<<uint(t.Hour())) == 0 {
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour()+1, 0, 0, 0, time.UTC)
			continue
		}
		if c.min&(1<<uint(t.Minute())) == 0 {
			t = t.Add(time.Minute)
			continue
		}
		return t, true
	}
	return time.Time{}, false
}

// minGap is the smallest gap between two consecutive matches. Every field is
// periodic over a year, so scanning two years of consecutive matches finds the
// minimum; the scan is cheap because Next skips whole non-matching days.
func (c *Cron) minGap() time.Duration {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC) // a leap year
	prev, ok := c.Next(start.Add(-time.Minute))
	if !ok {
		return 1<<62 - 1
	}
	gap := time.Duration(1<<62 - 1)
	end := start.AddDate(2, 0, 0)
	for n := 0; n < 200000; n++ {
		next, ok := c.Next(prev)
		if !ok || next.After(end) {
			break
		}
		if d := next.Sub(prev); d < gap {
			gap = d
			if gap < MinCronInterval {
				return gap
			}
		}
		prev = next
	}
	return gap
}
