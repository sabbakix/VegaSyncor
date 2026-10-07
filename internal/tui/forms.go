package tui

import (
	"errors"
	"strconv"
	"strings"

	"vegasyncor/internal/api"
	"vegasyncor/internal/config"
)

// ---------- job form ----------

func connOptions(conns []api.ConnectionView) []option {
	if len(conns) == 0 {
		return []option{{"", T("(none – create one in the Connections tab)")}}
	}
	out := make([]option, len(conns))
	for i, c := range conns {
		out[i] = option{c.ID, Tf("%s  (%s, user %s)", c.Name, c.Host, c.Username)}
	}
	return out
}

// zone is the server time zone, shown next to the schedule.
func newJobForm(j *config.Job, conns []api.ConnectionView, zone string) *form {
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
	locTypes := []option{{config.LocSMB, T("Network folder (SMB)")}, {config.LocLocal, T("Local folder on this server")}}
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
	// archive and log folders: "" = default / none
	folder := func(l *config.Location) config.Location {
		if l != nil {
			return *l
		}
		c := j.Dest.ConnectionID
		if c == "" && len(conns) > 0 {
			c = conns[0].ID
		}
		return config.Location{ConnectionID: c}
	}
	arc, logd := folder(j.Archive), folder(j.LogDir)
	logDays := ""
	if j.LogCompressDays > 0 {
		logDays = strconv.Itoa(j.LogCompressDays)
	}
	isArchive := func(f *form) bool { return f.choice("mode") == config.ModeMirrorArchive }
	arcOn := func(f *form) bool { return isArchive(f) && f.choice("arc_type") != "" }
	logOn := func(f *form) bool { return f.choice("log_type") != "" }
	and := func(a, b func(*form) bool) func(*form) bool { return func(f *form) bool { return a(f) && b(f) } }
	cron := j.Schedule.Cron
	if cron == "" {
		cron = "0 22 * * 1-5"
	}

	title := T("New sync")
	if !isNew {
		title = T("Edit:") + " " + j.Name
	}
	fm := &form{Title: title, Fields: []*field{
		section(T("General")),
		newText("name", T("Name"), j.Name, T("e.g. Accounting Office-PC")).withHelp(T("name shown for this sync in the lists and logs")),
		newBool("enabled", T("Active"), j.Enabled).withHelp(T("when off, the job does not run on schedule (it can still be started manually)")),

		section(T("1. Source – where to copy from")),
		newChoice("src_type", T("Type"), locTypes, j.Source.Type).withHelp(T("shared folder of a PC/server on the network, or a folder of this server")),
		newChoice("src_conn", T("Connection"), connOptions(conns), j.Source.ConnectionID).when(isSMB("src_type")).
			withHelp(T("PC or server to read from, with the credentials saved in the Connections tab")),
		newText("src_share", T("Share"), j.Source.Share, T("e.g. Documents")).browsable().when(isSMB("src_type")).
			withHelp(T("name of the shared folder on the PC/server (Enter to list them)")),
		newText("src_path", T("Subfolder"), j.Source.Path, T("empty = the whole share")).browsable().labeled(pathLabel("src")).
			withHelp(T("Enter to browse the folders")),
		newBool("src_ro", T("Read-only"), j.SourceRO).
			withHelp(T("recommended: the source is mounted read-only, it cannot be changed or deleted")),

		section(T("2. Destination – where to save the copy")),
		newChoice("dst_type", T("Type"), locTypes, j.Dest.Type).withHelp(T("where to save the copy: a folder of this server (also a mounted USB disk or NAS) or a network folder")),
		newChoice("dst_conn", T("Connection"), connOptions(conns), j.Dest.ConnectionID).when(isSMB("dst_type")).
			withHelp(T("PC, server or NAS to write the copy to")),
		newText("dst_share", T("Share"), j.Dest.Share, T("e.g. Backup")).browsable().when(isSMB("dst_type")).
			withHelp(T("destination shared folder (Enter to list them)")),
		newText("dst_path", T("Folder"), j.Dest.Path, T("empty = root of the share")).browsable().labeled(pathLabel("dst")).
			withHelp(T("Enter to browse the folders")),

		section(T("3. Copy mode")),
		newChoice("mode", T("Mode"), modes, j.Mode).helpFn(func(f *form) string { return config.ModeDescription(f.choice("mode")) }),
		newText("archive_days", T("Keep archive (days)"), arch, T("empty = forever")).
			when(func(f *form) bool { return f.choice("mode") == config.ModeMirrorArchive }).
			withHelp(T("archived versions older than N days are deleted")),
		newChoice("arc_type", T("Deleted items folder"), append([]option{{"", T("Default: inside the destination")}}, locTypes...), arc.Type).
			when(isArchive).helpFn(func(f *form) string {
			if f.choice("arc_type") == "" {
				return Tf("deleted and overwritten files go to %s/<date> in the destination", config.ArchiveDirName)
			}
			return T("deleted and overwritten files go to a dated subfolder of this folder; the same number of days applies")
		}),
		newChoice("arc_conn", T("Connection"), connOptions(conns), arc.ConnectionID).when(and(arcOn, isSMB("arc_type"))).
			withHelp(T("PC, server or NAS where the deleted items are kept")),
		newText("arc_share", T("Share"), arc.Share, T("e.g. Backup")).browsable().when(and(arcOn, isSMB("arc_type"))).
			withHelp(T("shared folder for the deleted items (Enter to list them)")),
		newText("arc_path", T("Folder"), arc.Path, T("e.g. Deleted/Accounting")).browsable().labeled(pathLabel("arc")).when(arcOn).
			withHelp(T("Enter to browse the folders; it can also be a subfolder of the destination")),
		newBool("allow_empty", T("Allow empty source"), j.AllowEmptySource).
			when(func(f *form) bool { return f.choice("mode") != config.ModeAdditive }).
			withHelp(T("normally a mirror with an empty source is blocked so the backup is not wiped")),
		newBool("checksum", T("Compare contents"), j.Checksum).
			withHelp(T("compares the file contents (checksum): finds every change, even with unchanged size and date, but reads all the files on both sides at every run (much slower on large shares); off = compare size and modification time")),
		newText("excludes", T("Exclude"), strings.Join(j.Excludes, ", "), T("e.g. *.tmp, Cache/")).
			withHelp(T("comma-separated patterns (Thumbs.db, desktop.ini, ~$* are already excluded)")),
		newText("bwlimit", T("Bandwidth limit (KB/s)"), bw, T("empty = unlimited")).
			withHelp(T("maximum copy speed, so the network is not slowed down (e.g. 5000 ≈ 40 Mbit/s); empty = no limit")),
		newChoice("log_type", T("Log folder"), append([]option{{"", T("None (logs only in History)")}}, locTypes...), logd.Type).
			withHelp(T("also save the log of every run as a file in a folder of your choice (dry runs too)")),
		newChoice("log_conn", T("Connection"), connOptions(conns), logd.ConnectionID).when(and(logOn, isSMB("log_type"))).
			withHelp(T("PC, server or NAS where the logs are saved")),
		newText("log_share", T("Share"), logd.Share, T("e.g. Backup")).browsable().when(and(logOn, isSMB("log_type"))).
			withHelp(T("shared folder for the logs (Enter to list them)")),
		newText("log_path", T("Folder"), logd.Path, T("e.g. Logs")).browsable().labeled(pathLabel("log")).when(logOn).
			withHelp(T("Enter to browse the folders; one file per run: <job>_<date>.log")),
		newText("log_days", T("Zip logs after (days)"), logDays, T("empty = never")).when(logOn).
			withHelp(T("logs older than N days are moved into one zip per month (<job>_logs_<YYYY-MM>.zip); nothing is deleted")),

		section(schedSection(zone)),
		newChoice("sched", T("When"), scheds, j.Schedule.Type).helpFn(schedHelp),
		newText("every", T("Every (minutes)"), every, T("e.g. 30, 60, 240")).when(sched(config.SchedInterval)).
			withHelp(T("60 = every hour, 240 = every 4 hours")),
		newText("win_from", T("Window from"), j.Schedule.WindowFrom, T("optional, e.g. 08:00")).when(sched(config.SchedInterval)).
			withHelp(T("runs only from this time (HH:MM); empty = all day")),
		newText("win_to", T("Window to"), j.Schedule.WindowTo, T("optional, e.g. 19:00")).when(sched(config.SchedInterval)).
			withHelp(T("last possible run (HH:MM); it can also be after midnight, e.g. 22:00 → 06:00")),
		newText("times", T("Times"), strings.Join(j.Schedule.Times, ", "), T("e.g. 13:00, 22:30")).
			when(sched(config.SchedDaily, config.SchedWeekly)).withHelp(T("one or more comma-separated times")),
		newDays("days", T("Days"), j.Schedule.Days).helpFn(func(f *form) string {
			if f.choice("sched") == config.SchedInterval {
				return T("days on which to repeat the copy; no day selected = every day")
			}
			return T("days of the week on which to run the copy")
		}).when(sched(config.SchedWeekly, config.SchedInterval)),
		newText("cron", T("Cron expression"), cron, T("min hour day month weekday")).when(sched(config.SchedCron)).
			withHelp(T("e.g. \"0 */2 * * 1-5\" = every 2 hours, Mon-Fri")),
	}}
	fm.init()
	return fm
}

func schedHelp(f *form) string {
	switch f.choice("sched") {
	case config.SchedInterval:
		return T("repeats the copy every N minutes, optionally only in a time window and on certain days")
	case config.SchedDaily:
		return T("runs every day at the given times")
	case config.SchedWeekly:
		return T("runs at the given times, only on the selected days")
	case config.SchedCron:
		return T("advanced schedule with a 5-field cron expression")
	case config.SchedManual:
		return T("no automatic runs: it is only started manually (key r)")
	}
	return ""
}

func schedSection(zone string) string {
	if zone == "" {
		return T("4. Schedule – server time")
	}
	return Tf("4. Schedule – server time (%s)", zone)
}

func pathLabel(prefix string) func(*form) string {
	return func(f *form) string {
		if f.choice(prefix+"_type") == config.LocSMB {
			return T("Subfolder")
		}
		return T("Folder (path)")
	}
}

func atoiField(fm *form, key, what string) (int, error) {
	v := fm.val(key)
	if v == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0, errors.New(what + ": " + T("enter a number"))
	}
	return n, nil
}

// jobFromForm builds the job from the form fields (the service does the full validation).
func jobFromForm(fm *form, id string) (config.Job, error) {
	j := config.Job{
		ID:               id,
		Name:             fm.val("name"),
		Enabled:          fm.get("enabled").Bool,
		SourceRO:         fm.get("src_ro").Bool,
		Mode:             fm.choice("mode"),
		AllowEmptySource: fm.get("allow_empty").Bool,
		Checksum:         fm.get("checksum").Bool,
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
	if j.Mode == config.ModeMirrorArchive && fm.choice("arc_type") != "" {
		a := loc("arc")
		j.Archive = &a
	}
	var err error
	if fm.choice("log_type") != "" {
		l := loc("log")
		j.LogDir = &l
		if j.LogCompressDays, err = atoiField(fm, "log_days", T("zip logs after")); err != nil {
			return j, err
		}
	}
	if j.Mode == config.ModeMirrorArchive {
		if j.ArchiveDays, err = atoiField(fm, "archive_days", T("archive days")); err != nil {
			return j, err
		}
	}
	if j.BandwidthKBps, err = atoiField(fm, "bwlimit", T("bandwidth limit")); err != nil {
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
		if s.EveryMinutes, err = atoiField(fm, "every", T("interval")); err != nil {
			return j, err
		}
		if s.EveryMinutes == 0 {
			return j, errors.New(T("interval: enter every how many minutes"))
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

// ---------- connection form ----------

func smbVersions() []option {
	return []option{
		{"", T("Automatic (recommended)")}, {"3.1.1", "SMB 3.1.1"}, {"3.0", "SMB 3.0"},
		{"2.1", "SMB 2.1 (Windows 7 / 2008 R2)"}, {"2.0", "SMB 2.0"}, {"1.0", T("SMB 1 (not recommended, very old systems)")},
	}
}

func newConnForm(c *api.ConnectionView) *form {
	isNew := c == nil
	if isNew {
		c = &api.ConnectionView{}
	}
	title, pwHelp, pwPlace := T("New connection"), T("stored encrypted (AES-256), it will not be visible again"), T("the user's password")
	if !isNew {
		title = T("Edit connection:") + " " + c.Name
		pwPlace = T("(unchanged – leave empty to keep it)")
		pwHelp = T("leave empty to keep the saved one; enter a value only to change it")
	}
	fm := &form{Title: title, Fields: []*field{
		section(T("Server")),
		newText("name", T("Name"), c.Name, T("e.g. Admin Office PC")).withHelp(T("descriptive name, used to choose the connection in the jobs")),
		newText("host", T("Host / IP address"), c.Host, T("e.g. 192.168.1.20 or OFFICE-PC")).
			withHelp(T("IP address or name of the PC/server; if the name is not found use the IP")),
		newChoice("ver", T("SMB version"), smbVersions(), c.SMBVersion).
			withHelp(T("leave it automatic unless there are connection errors")),
		section(T("Credentials")),
		newText("domain", T("Domain / workgroup"), c.Domain, T("optional, e.g. COMPANY or WORKGROUP")).
			withHelp(T("Windows domain of the user; leave empty for a local user of the PC")),
		newText("user", T("User"), c.Username, T("e.g. backup")).withHelp(T("user allowed to read the share (and write, if used as destination)")),
		newPassword("password", T("Password"), pwPlace).withHelp(pwHelp),
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
