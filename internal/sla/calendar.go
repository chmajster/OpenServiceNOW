package sla

import (
	"fmt"
	"sort"
	"strings"
	"time"
	_ "time/tzdata"
	"unicode/utf8"
)

func minute(s string) (int, error) {
	if s == "24:00" {
		return 1440, nil
	}
	t, err := time.Parse("15:04", s)
	if err != nil || len(s) != 5 {
		return 0, ErrInvalid
	}
	return t.Hour()*60 + t.Minute(), nil
}
func validateWindows(w []Window) error {
	if len(w) > 8 {
		return ErrInvalid
	}
	last := -1
	for _, v := range w {
		a, e := minute(v.Start)
		b, f := minute(v.End)
		if e != nil || f != nil || a >= b || a < last {
			return ErrInvalid
		}
		last = b
	}
	return nil
}
func validName(s string) bool {
	return utf8.ValidString(s) && strings.TrimSpace(s) == s && len(s) > 0 && utf8.RuneCountInString(s) <= 120
}
func validDate(s string) bool { _, e := time.Parse("2006-01-02", s); return e == nil && len(s) == 10 }
func ValidateCalendar(c Calendar) error {
	if !validName(c.Name) || len(c.Week) > 7 || len(c.Holidays) > 1000 || len(c.Exceptions) > 1000 {
		return ErrInvalid
	}
	if c.Timezone == "Local" || c.Timezone == "" {
		return ErrInvalid
	}
	if _, err := time.LoadLocation(c.Timezone); err != nil {
		return fmt.Errorf("%w: wymagana strefa IANA", ErrInvalid)
	}
	seen := map[int]bool{}
	total := 0
	for _, d := range c.Week {
		if d.Weekday < 0 || d.Weekday > 6 || seen[d.Weekday] || validateWindows(d.Windows) != nil {
			return ErrInvalid
		}
		seen[d.Weekday] = true
		for _, w := range d.Windows {
			a, _ := minute(w.Start)
			b, _ := minute(w.End)
			total += b - a
		}
	}
	if total < 60 {
		return fmt.Errorf("%w: co najmniej 60 minut pracy tygodniowo", ErrInvalid)
	}
	dates := map[string]bool{}
	for _, d := range c.Holidays {
		if !validDate(d) || dates[d] {
			return ErrInvalid
		}
		dates[d] = true
	}
	dates = map[string]bool{}
	for _, d := range c.Exceptions {
		if !validDate(d.Date) || dates[d.Date] || validateWindows(d.Windows) != nil {
			return ErrInvalid
		}
		dates[d.Date] = true
	}
	return nil
}

type span struct{ start, end time.Time }

func (c Calendar) windows(date time.Time) []Window {
	key := date.Format("2006-01-02")
	for _, e := range c.Exceptions {
		if e.Date == key {
			return e.Windows
		}
	}
	for _, h := range c.Holidays {
		if h == key {
			return nil
		}
	}
	for _, d := range c.Week {
		if d.Weekday == int(date.Weekday()) {
			return d.Windows
		}
	}
	return nil
}

// Working intervals are intersections with the real UTC timeline. On a DST
// transition day repeated local minutes count twice and missing minutes never
// count. Windows are minute-aligned; elapsed values retain second precision.
func (c Calendar) spans(date time.Time) []span {
	end := date.AddDate(0, 0, 1)
	w := c.windows(date)
	result := []span{}
	if end.Sub(date) == 24*time.Hour {
		for _, v := range w {
			a, _ := minute(v.Start)
			b, _ := minute(v.End)
			result = append(result, span{date.Add(time.Duration(a) * time.Minute), date.Add(time.Duration(b) * time.Minute)})
		}
		return result
	}
	for t := date; t.Before(end); t = t.Add(time.Minute) {
		local := t.In(date.Location())
		m := local.Hour()*60 + local.Minute()
		active := false
		for _, v := range w {
			a, _ := minute(v.Start)
			b, _ := minute(v.End)
			if m >= a && m < b {
				active = true
				break
			}
		}
		if active {
			if len(result) > 0 && result[len(result)-1].end.Equal(t) {
				result[len(result)-1].end = t.Add(time.Minute)
			} else {
				result = append(result, span{t, t.Add(time.Minute)})
			}
		}
	}
	return result
}
func (c Calendar) traverse(start, end time.Time, seconds int64) (int64, time.Time, error) {
	loc, err := time.LoadLocation(c.Timezone)
	if err != nil {
		return 0, time.Time{}, ErrInvalid
	}
	local := start.In(loc)
	date := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	elapsed := int64(0)
	for day := 0; day < 3660; day++ {
		if !end.IsZero() && !date.Before(end) {
			return elapsed, end.UTC(), nil
		}
		for _, v := range c.spans(date) {
			a := v.start
			b := v.end
			if a.Before(start) {
				a = start
			}
			if !end.IsZero() && b.After(end) {
				b = end
			}
			if !a.Before(b) {
				continue
			}
			n := int64(b.Sub(a) / time.Microsecond)
			if end.IsZero() && elapsed+n >= seconds {
				return seconds, a.Add(time.Duration(seconds-elapsed) * time.Microsecond).UTC(), nil
			}
			elapsed += n
		}
		date = date.AddDate(0, 0, 1)
	}
	return 0, time.Time{}, fmt.Errorf("%w: kalendarz nie zapewnia terminu w ciągu 10 lat", ErrInvalid)
}
func (c Calendar) Elapsed(start, end time.Time) (int64, error) {
	if !end.After(start) {
		return 0, nil
	}
	n, _, e := c.traverse(start, end, 0)
	return n / 1000000, e
}
func (c Calendar) Add(start time.Time, seconds int64) (time.Time, error) {
	if seconds <= 0 {
		return start.UTC(), nil
	}
	_, t, e := c.traverse(start, time.Time{}, seconds*1000000)
	return t, e
}

func (c Calendar) elapsedMicros(start, end time.Time) (int64, error) {
	if !end.After(start) {
		return 0, nil
	}
	n, _, e := c.traverse(start, end, 0)
	return n, e
}
func (c Calendar) addMicros(start time.Time, micros int64) (time.Time, error) {
	if micros <= 0 {
		return start.UTC(), nil
	}
	_, t, e := c.traverse(start, time.Time{}, micros)
	return t, e
}
func ValidatePolicy(p Policy) error {
	if !validName(p.Name) || p.Order < 0 || p.Order > 100000 || p.Priority < 0 || p.Priority > 5 || p.CalendarID == 0 || p.CalendarVersion == 0 || p.ResponseSeconds < 60 || p.ResponseSeconds > 2592000 || p.ResolutionSeconds < 60 || p.ResolutionSeconds > 2592000 || p.WarningPercent < 1 || p.WarningPercent > 99 || len(p.PauseReasons) > 20 {
		return ErrInvalid
	}
	if p.Category != "" && p.Category != "hardware" && p.Category != "software" && p.Category != "network" && p.Category != "access" && p.Category != "other" {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for _, s := range p.PauseReasons {
		if !validName(s) || seen[s] {
			return ErrInvalid
		}
		seen[s] = true
	}
	return nil
}

// Lower administrative order wins; then specificity, then stable policy ID.
func choose(p []Policy, priority int, category string) *Policy {
	matches := []Policy{}
	for _, v := range p {
		if v.Enabled && (v.Priority == 0 || v.Priority == priority) && (v.Category == "" || v.Category == category) {
			matches = append(matches, v)
		}
	}
	specificity := func(v Policy) int {
		n := 0
		if v.Priority != 0 {
			n++
		}
		if v.Category != "" {
			n++
		}
		return n
	}
	sort.Slice(matches, func(i, j int) bool {
		a, b := matches[i], matches[j]
		if a.Order != b.Order {
			return a.Order < b.Order
		}
		if specificity(a) != specificity(b) {
			return specificity(a) > specificity(b)
		}
		return a.ID < b.ID
	})
	if len(matches) == 0 {
		return nil
	}
	return &matches[0]
}
