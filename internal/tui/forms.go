package tui

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"vegasyncor/internal/api"
	"vegasyncor/internal/config"
)

// ---------- form job ----------

func connOptions(conns []api.ConnectionView) []option {
	if len(conns) == 0 {
		return []option{{"", "(nessuna – crearla nella scheda Connessioni)"}}
	}
	out := make([]option, len(conns))
	for i, c := range conns {
		out[i] = option{c.ID, fmt.Sprintf("%s  (%s, utente %s)", c.Name, c.Host, c.Username)}
	}
	return out
}

func newJobForm(j *config.Job, conns []api.ConnectionView) *form {
	isNew := j == nil
	if isNew {
		first := ""
		if len(conns) > 0 {
			first = conns[0].ID
		}
		j = &config.Job{
			Enabled:  true,
			Source:   config.Location{Type: config.LocSMB, ConnectionID: first},
			SourceRO: true,
			Dest:     config.Location{Type: config.LocLocal},
			Mode:     config.ModeMirrorArchive, ArchiveDays: 30,
			Schedule: config.Schedule{Type: config.SchedWeekly, Times: []string{"22:00"}, Days: []int{1, 2, 3, 4, 5}, EveryMinutes: 60},
		}
	}
	locTypes := []option{{config.LocSMB, "Cartella di rete (SMB)"}, {config.LocLocal, "Cartella locale del server"}}
	var modes []option
	for _, m := range config.Modes {
		modes = append(modes, option{m, config.ModeLabel(m)})
	}
	var scheds []option
	for _, s := range config.ScheduleTypes {
		scheds = append(scheds, option{s, config.ScheduleTypeLabel(s)})
	}
	isSMB := func(k string) func(*form) bool { return func(f *form) bool { return f.choice(k) == config.LocSMB } }
	sched := func(types ...string) func(*form) bool {
		return func(f *form) bool {
			for _, t := range types {
				if f.choice("sched") == t {
					return true
				}
			}
			return false
		}
	}
	every := ""
	if j.Schedule.EveryMinutes > 0 {
		every = strconv.Itoa(j.Schedule.EveryMinutes)
	}
	arch := ""
	if j.ArchiveDays > 0 {
		arch = strconv.Itoa(j.ArchiveDays)
	}
	bw := ""
	if j.BandwidthKBps > 0 {
		bw = strconv.Itoa(j.BandwidthKBps)
	}
	cron := j.Schedule.Cron
	if cron == "" {
		cron = "0 22 * * 1-5"
	}

	title := "Nuova sincronizzazione"
	if !isNew {
		title = "Modifica: " + j.Name
	}
	fm := &form{Title: title, Fields: []*field{
		section("Generale"),
		newText("name", "Nome", j.Name, "es. Contabilità PC-Ufficio"),
		newBool("enabled", "Attivo", j.Enabled).withHelp("se disattivato, il job non parte da pianificazione (resta avviabile a mano)"),

		section("① Sorgente – da dove copiare"),
		newChoice("src_type", "Tipo", locTypes, j.Source.Type),
		newChoice("src_conn", "Connessione", connOptions(conns), j.Source.ConnectionID).when(isSMB("src_type")),
		newText("src_share", "Condivisione", j.Source.Share, "es. Documenti").browsable().when(isSMB("src_type")).
			withHelp("nome della cartella condivisa sul PC/server (Invio per elencarle)"),
		newText("src_path", "Sottocartella", j.Source.Path, "vuoto = tutta la condivisione").browsable().labeled(pathLabel("src")).
			withHelp("Invio per sfogliare le cartelle"),
		newBool("src_ro", "Sola lettura", j.SourceRO).
			withHelp("consigliato: la sorgente viene montata in sola lettura, impossibile modificarla o cancellarla"),

		section("② Destinazione – dove salvare la copia"),
		newChoice("dst_type", "Tipo", locTypes, j.Dest.Type),
		newChoice("dst_conn", "Connessione", connOptions(conns), j.Dest.ConnectionID).when(isSMB("dst_type")),
		newText("dst_share", "Condivisione", j.Dest.Share, "es. Backup").browsable().when(isSMB("dst_type")),
		newText("dst_path", "Cartella", j.Dest.Path, "vuoto = radice della condivisione").browsable().labeled(pathLabel("dst")).withHelp("Invio per sfogliare le cartelle"),

		section("③ Modalità di copia"),
		newChoice("mode", "Modalità", modes, j.Mode),
		newText("archive_days", "Conserva archivio (gg)", arch, "vuoto = per sempre").
			when(func(f *form) bool { return f.choice("mode") == config.ModeMirrorArchive }).
			withHelp("le versioni archiviate più vecchie di N giorni vengono eliminate"),
		newBool("allow_empty", "Consenti sorgente vuota", j.AllowEmptySource).
			when(func(f *form) bool { return f.choice("mode") != config.ModeAdditive }).
			withHelp("di norma un mirror con sorgente vuota viene bloccato per non svuotare il backup"),
		newText("excludes", "Escludi", strings.Join(j.Excludes, ", "), "es. *.tmp, Cache/").
			withHelp("modelli separati da virgola (Thumbs.db, desktop.ini, ~$* sono già esclusi)"),
		newText("bwlimit", "Limite banda (KB/s)", bw, "vuoto = illimitata"),

		section("④ Pianificazione"),
		newChoice("sched", "Quando", scheds, j.Schedule.Type),
		newText("every", "Ogni (minuti)", every, "es. 30, 60, 240").when(sched(config.SchedInterval)).
			withHelp("60 = ogni ora, 240 = ogni 4 ore"),
		newText("win_from", "Fascia dalle", j.Schedule.WindowFrom, "facoltativo, es. 08:00").when(sched(config.SchedInterval)),
		newText("win_to", "Fascia alle", j.Schedule.WindowTo, "facoltativo, es. 19:00").when(sched(config.SchedInterval)),
		newText("times", "Orari", strings.Join(j.Schedule.Times, ", "), "es. 13:00, 22:30").
			when(sched(config.SchedDaily, config.SchedWeekly)).withHelp("uno o più orari separati da virgola"),
		newDays("days", "Giorni", j.Schedule.Days).withHelp("per gli intervalli: nessun giorno selezionato = tutti i giorni").when(sched(config.SchedWeekly, config.SchedInterval)),
		newText("cron", "Espressione cron", cron, "min ora giorno mese giorno-sett").when(sched(config.SchedCron)).
			withHelp("es. \"0 */2 * * 1-5\" = ogni 2 ore, lun-ven"),
	}}
	fm.init()
	return fm
}

func pathLabel(prefix string) func(*form) string {
	return func(f *form) string {
		if f.choice(prefix+"_type") == config.LocSMB {
			return "Sottocartella"
		}
		return "Cartella (percorso)"
	}
}

func atoiField(fm *form, key, what string) (int, error) {
	v := fm.val(key)
	if v == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("%s: inserire un numero", what)
	}
	return n, nil
}

// jobFromForm costruisce il job dai campi del form (la validazione completa la fa il servizio).
func jobFromForm(fm *form, id string) (config.Job, error) {
	j := config.Job{
		ID:               id,
		Name:             fm.val("name"),
		Enabled:          fm.get("enabled").Bool,
		SourceRO:         fm.get("src_ro").Bool,
		Mode:             fm.choice("mode"),
		AllowEmptySource: fm.get("allow_empty").Bool,
	}
	loc := func(p string) config.Location {
		l := config.Location{Type: fm.choice(p + "_type"), Path: fm.val(p + "_path")}
		if l.Type == config.LocSMB {
			l.ConnectionID = fm.choice(p + "_conn")
			l.Share = fm.val(p + "_share")
		}
		return l
	}
	j.Source, j.Dest = loc("src"), loc("dst")
	var err error
	if j.Mode == config.ModeMirrorArchive {
		if j.ArchiveDays, err = atoiField(fm, "archive_days", "giorni di archivio"); err != nil {
			return j, err
		}
	}
	if j.BandwidthKBps, err = atoiField(fm, "bwlimit", "limite di banda"); err != nil {
		return j, err
	}
	for _, e := range strings.Split(fm.val("excludes"), ",") {
		if e = strings.TrimSpace(e); e != "" {
			j.Excludes = append(j.Excludes, e)
		}
	}
	s := config.Schedule{Type: fm.choice("sched")}
	switch s.Type {
	case config.SchedInterval:
		if s.EveryMinutes, err = atoiField(fm, "every", "intervallo"); err != nil {
			return j, err
		}
		if s.EveryMinutes == 0 {
			return j, errors.New("intervallo: indicare ogni quanti minuti")
		}
		s.WindowFrom, s.WindowTo = fm.val("win_from"), fm.val("win_to")
		if d := fm.get("days").days(); len(d) < 7 {
			s.Days = d
		}
	case config.SchedDaily, config.SchedWeekly:
		if s.Times, err = config.ParseTimes(fm.val("times")); err != nil {
			return j, err
		}
		if s.Type == config.SchedWeekly {
			s.Days = fm.get("days").days()
		}
	case config.SchedCron:
		s.Cron = fm.val("cron")
	}
	j.Schedule = s
	return j, nil
}

// ---------- form connessione ----------

var smbVersions = []option{
	{"", "Automatica (consigliata)"}, {"3.1.1", "SMB 3.1.1"}, {"3.0", "SMB 3.0"},
	{"2.1", "SMB 2.1 (Windows 7 / 2008 R2)"}, {"2.0", "SMB 2.0"}, {"1.0", "SMB 1 (sconsigliato, sistemi molto vecchi)"},
}

func newConnForm(c *api.ConnectionView) *form {
	isNew := c == nil
	if isNew {
		c = &api.ConnectionView{}
	}
	title, pwHelp, pwPlace := "Nuova connessione", "", "password dell'utente"
	if !isNew {
		title = "Modifica connessione: " + c.Name
		pwPlace = "(invariata – lasciare vuoto per mantenerla)"
		pwHelp = "inserire un valore solo per cambiarla"
	}
	fm := &form{Title: title, Fields: []*field{
		section("Server"),
		newText("name", "Nome", c.Name, "es. PC Ufficio Amministrazione"),
		newText("host", "Host / indirizzo IP", c.Host, "es. 192.168.1.20 oppure PC-UFFICIO"),
		newChoice("ver", "Versione SMB", smbVersions, c.SMBVersion).
			withHelp("lasciare automatica salvo errori di connessione"),
		section("Credenziali"),
		newText("domain", "Dominio / gruppo", c.Domain, "facoltativo, es. AZIENDA o WORKGROUP"),
		newText("user", "Utente", c.Username, "es. backup"),
		newPassword("password", "Password", pwPlace).withHelp(pwHelp),
	}}
	fm.init()
	return fm
}

func connFromForm(fm *form, id string) api.ConnectionInput {
	return api.ConnectionInput{
		Connection: config.Connection{
			ID: id, Name: fm.val("name"), Host: fm.val("host"), SMBVersion: fm.choice("ver"),
			Domain: fm.val("domain"), Username: fm.val("user"),
		},
		Password: fm.get("password").Input.Value(),
	}
}
