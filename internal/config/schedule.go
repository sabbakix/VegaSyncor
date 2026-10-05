package config

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

const (
	SchedManual   = "manual"
	SchedInterval = "interval"
	SchedDaily    = "daily"
	SchedWeekly   = "weekly"
	SchedCron     = "cron"
)

var ScheduleTypes = []string{SchedInterval, SchedDaily, SchedWeekly, SchedCron, SchedManual}

func ScheduleTypeLabel(t string) string {
	switch t {
	case SchedManual:
		return T("Manual only")
	case SchedInterval:
		return T("At intervals")
	case SchedDaily:
		return T("Every day")
	case SchedWeekly:
		return T("Days of the week")
	case SchedCron:
		return T("Cron expression")
	}
	return t
}

// Schedule describes when a job runs.
type Schedule struct {
	Type string `json:"type"`
	// EveryMinutes for Type=interval.
	EveryMinutes int `json:"every_minutes,omitempty"`
	// Optional window for Type=interval: runs only between From and To (HH:MM).
	WindowFrom string `json:"window_from,omitempty"`
	WindowTo   string `json:"window_to,omitempty"`
	// Times (HH:MM) for Type=daily/weekly.
	Times []string `json:"times,omitempty"`
	// Days for Type=weekly and Type=interval: 0=Sunday ... 6=Saturday.
	Days []int `json:"days,omitempty"`
	// Cron: standard 5-field expression.
	Cron string `json:"cron,omitempty"`
}

// DayName returns the translated short name of a weekday (0=Sunday).
func DayName(d int) string {
	switch d {
	case 0:
		return T("Sun")
	case 1:
		return T("Mon")
	case 2:
		return T("Tue")
	case 3:
		return T("Wed")
	case 4:
		return T("Thu")
	case 5:
		return T("Fri")
	}
	return T("Sat")
}

// WeekOrder is the display order of the days (from Monday).
var WeekOrder = []int{1, 2, 3, 4, 5, 6, 0}

func ParseHHMM(s string) (int, int, error) {
	s = strings.TrimSpace(s)
	t, err := time.Parse("15:04", s)
	if err != nil {
		if t, err = time.Parse("15.04", s); err != nil {
			return 0, 0, errors.New(Tf("invalid time %q (use HH:MM)", s))
		}
	}
	return t.Hour(), t.Minute(), nil
}

func ParseTimes(s string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, p := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == ';' }) {
		h, m, err := ParseHHMM(p)
		if err != nil {
			return nil, err
		}
		v := fmt.Sprintf("%02d:%02d", h, m)
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out, nil
}

var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

func ParseCron(spec string) (cron.Schedule, error) { return cronParser.Parse(spec) }

func daysField(days []int) string {
	if len(days) == 0 || len(days) == 7 {
		return "*"
	}
	d := append([]int(nil), days...)
	sort.Ints(d)
	parts := make([]string, len(d))
	for i, v := range d {
		parts[i] = strconv.Itoa(v)
	}
	return strings.Join(parts, ",")
}

func (s *Schedule) Validate() error {
	for _, d := range s.Days {
		if d < 0 || d > 6 {
			return errors.New(T("invalid day of the week"))
		}
	}
	switch s.Type {
	case SchedManual:
	case SchedInterval:
		if s.EveryMinutes < 1 {
			return errors.New(T("interval: enter the minutes (at least 1)"))
		}
		if (s.WindowFrom == "") != (s.WindowTo == "") {
			return errors.New(T("time window: enter both start and end"))
		}
		if s.WindowFrom != "" {
			if _, _, err := ParseHHMM(s.WindowFrom); err != nil {
				return err
			}
			if _, _, err := ParseHHMM(s.WindowTo); err != nil {
				return err
			}
		}
	case SchedDaily, SchedWeekly:
		if len(s.Times) == 0 {
			return errors.New(T("enter at least one time (e.g. 13:00, 22:30)"))
		}
		for _, t := range s.Times {
			if _, _, err := ParseHHMM(t); err != nil {
				return err
			}
		}
		if s.Type == SchedWeekly && len(s.Days) == 0 {
			return errors.New(T("select at least one day"))
		}
	case SchedCron:
		if _, err := ParseCron(s.Cron); err != nil {
			return errors.New(Tf("invalid cron expression: %v", err))
		}
	default:
		return errors.New(T("invalid schedule type"))
	}
	return nil
}

// Next computes the next run after t (zero if manual).
func (s *Schedule) Next(t time.Time) time.Time {
	switch s.Type {
	case SchedInterval:
		return s.nextInterval(t)
	case SchedDaily, SchedWeekly:
		days := "*"
		if s.Type == SchedWeekly {
			days = daysField(s.Days)
		}
		var best time.Time
		for _, tm := range s.Times {
			h, m, err := ParseHHMM(tm)
			if err != nil {
				continue
			}
			sc, err := ParseCron(fmt.Sprintf("%d %d * * %s", m, h, days))
			if err != nil {
				continue
			}
			if n := sc.Next(t); best.IsZero() || n.Before(best) {
				best = n
			}
		}
		return best
	case SchedCron:
		if sc, err := ParseCron(s.Cron); err == nil {
			return sc.Next(t)
		}
	}
	return time.Time{}
}

// nextInterval: runs aligned to the interval starting from midnight
// (e.g. every 30 minutes -> :00 and :30), limited to the time window and days if set.
func (s *Schedule) nextInterval(t time.Time) time.Time {
	step := time.Duration(s.EveryMinutes) * time.Minute
	fromMin, toMin := 0, 24*60
	if s.WindowFrom != "" {
		h, m, _ := ParseHHMM(s.WindowFrom)
		fromMin = h*60 + m
		h, m, _ = ParseHHMM(s.WindowTo)
		toMin = h*60 + m
	}
	allowedDay := func(d time.Weekday) bool {
		if len(s.Days) == 0 {
			return true
		}
		for _, v := range s.Days {
			if int(d) == v {
				return true
			}
		}
		return false
	}
	inWindow := func(x time.Time) bool {
		mins := x.Hour()*60 + x.Minute()
		if fromMin <= toMin {
			return mins >= fromMin && mins <= toMin
		}
		return mins >= fromMin || mins <= toMin // window spanning midnight
	}
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	for i := 0; i < 9; i++ {
		d := day.AddDate(0, 0, i)
		if !allowedDay(d.Weekday()) {
			continue
		}
		start := d
		if s.WindowFrom != "" && fromMin <= toMin {
			start = d.Add(time.Duration(fromMin) * time.Minute)
		}
		for x := start; x.Before(d.AddDate(0, 0, 1)); x = x.Add(step) {
			if x.After(t) && inWindow(x) {
				return x
			}
		}
	}
	return time.Time{}
}

// Describe returns a short, translated description.
func (s *Schedule) Describe() string {
	days := func() string {
		if len(s.Days) == 0 || len(s.Days) == 7 {
			return T("every day")
		}
		set := map[int]bool{}
		for _, d := range s.Days {
			set[d] = true
		}
		if len(s.Days) == 5 && !set[0] && !set[6] {
			return T("Mon-Fri")
		}
		var n []string
		for _, d := range WeekOrder {
			if set[d] {
				n = append(n, DayName(d))
			}
		}
		return strings.Join(n, ",")
	}
	switch s.Type {
	case SchedManual:
		return T("manual")
	case SchedInterval:
		var e string
		if s.EveryMinutes%60 == 0 {
			e = Tf("every %d h", s.EveryMinutes/60)
		} else {
			e = Tf("every %d min", s.EveryMinutes)
		}
		if s.WindowFrom != "" {
			e += fmt.Sprintf(" %s-%s", s.WindowFrom, s.WindowTo)
		}
		if len(s.Days) > 0 && len(s.Days) < 7 {
			e += " " + days()
		}
		return e
	case SchedDaily:
		return T("daily") + " " + strings.Join(s.Times, ",")
	case SchedWeekly:
		return days() + " " + strings.Join(s.Times, ",")
	case SchedCron:
		return "cron " + s.Cron
	}
	return "?"
}
