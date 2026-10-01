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
		return "Solo manuale"
	case SchedInterval:
		return "A intervalli"
	case SchedDaily:
		return "Ogni giorno"
	case SchedWeekly:
		return "Giorni della settimana"
	case SchedCron:
		return "Espressione cron"
	}
	return t
}

// Schedule descrive quando eseguire un job.
type Schedule struct {
	Type string `json:"type"`
	// EveryMinutes per Type=interval.
	EveryMinutes int `json:"every_minutes,omitempty"`
	// Window opzionale per Type=interval: esegue solo tra From e To (HH:MM).
	WindowFrom string `json:"window_from,omitempty"`
	WindowTo   string `json:"window_to,omitempty"`
	// Times (HH:MM) per Type=daily/weekly.
	Times []string `json:"times,omitempty"`
	// Days per Type=weekly e per interval con finestra: 0=domenica ... 6=sabato.
	Days []int `json:"days,omitempty"`
	// Cron: espressione standard a 5 campi.
	Cron string `json:"cron,omitempty"`
}

var DayNames = []string{"Dom", "Lun", "Mar", "Mer", "Gio", "Ven", "Sab"}

// WeekOrder è l'ordine di visualizzazione dei giorni (da lunedì).
var WeekOrder = []int{1, 2, 3, 4, 5, 6, 0}

func ParseHHMM(s string) (int, int, error) {
	s = strings.TrimSpace(s)
	t, err := time.Parse("15:04", s)
	if err != nil {
		if t, err = time.Parse("15.04", s); err != nil {
			return 0, 0, fmt.Errorf("orario %q non valido (usare HH:MM)", s)
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
			return errors.New("giorno della settimana non valido")
		}
	}
	switch s.Type {
	case SchedManual:
	case SchedInterval:
		if s.EveryMinutes < 1 {
			return errors.New("intervallo: indicare i minuti (almeno 1)")
		}
		if (s.WindowFrom == "") != (s.WindowTo == "") {
			return errors.New("fascia oraria: indicare sia inizio che fine")
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
			return errors.New("indicare almeno un orario (es. 13:00, 22:30)")
		}
		for _, t := range s.Times {
			if _, _, err := ParseHHMM(t); err != nil {
				return err
			}
		}
		if s.Type == SchedWeekly && len(s.Days) == 0 {
			return errors.New("selezionare almeno un giorno")
		}
	case SchedCron:
		if _, err := ParseCron(s.Cron); err != nil {
			return fmt.Errorf("espressione cron non valida: %v", err)
		}
	default:
		return errors.New("tipo di pianificazione non valido")
	}
	return nil
}

// Next calcola la prossima esecuzione successiva a t (zero se manuale).
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

// nextInterval: esecuzioni allineate all'intervallo a partire dalla mezzanotte
// (es. ogni 30 minuti -> :00 e :30), limitate alla fascia oraria e ai giorni se indicati.
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
		return mins >= fromMin || mins <= toMin // fascia a cavallo della mezzanotte
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

// Describe restituisce una descrizione breve in italiano.
func (s *Schedule) Describe() string {
	days := func() string {
		if len(s.Days) == 0 || len(s.Days) == 7 {
			return "tutti i giorni"
		}
		set := map[int]bool{}
		for _, d := range s.Days {
			set[d] = true
		}
		if len(s.Days) == 5 && !set[0] && !set[6] {
			return "lun-ven"
		}
		var n []string
		for _, d := range WeekOrder {
			if set[d] {
				n = append(n, strings.ToLower(DayNames[d]))
			}
		}
		return strings.Join(n, ",")
	}
	switch s.Type {
	case SchedManual:
		return "manuale"
	case SchedInterval:
		var e string
		if s.EveryMinutes%60 == 0 {
			e = fmt.Sprintf("ogni %d h", s.EveryMinutes/60)
		} else {
			e = fmt.Sprintf("ogni %d min", s.EveryMinutes)
		}
		if s.WindowFrom != "" {
			e += fmt.Sprintf(" %s-%s", s.WindowFrom, s.WindowTo)
		}
		if len(s.Days) > 0 && len(s.Days) < 7 {
			e += " " + days()
		}
		return e
	case SchedDaily:
		return "ogni giorno " + strings.Join(s.Times, ",")
	case SchedWeekly:
		return days() + " " + strings.Join(s.Times, ",")
	case SchedCron:
		return "cron " + s.Cron
	}
	return "?"
}
