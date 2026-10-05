// VegaSyncor: scheduled sync of network folders (SMB/CIFS) for backup.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"vegasyncor/internal/api"
	"vegasyncor/internal/config"
	"vegasyncor/internal/daemon"
	"vegasyncor/internal/i18n"
	"vegasyncor/internal/mount"
	"vegasyncor/internal/paths"
	"vegasyncor/internal/tui"
)

var version = "dev"

const usage = `VegaSyncor %s – sync of network folders for backup

Usage:
  vegasyncor                 opens the management interface (TUI)
  vegasyncor --no-mouse      opens the TUI without mouse support
  vegasyncor daemon          starts the service (normally through systemd)
  vegasyncor status          shows the status of the jobs
  vegasyncor check           checks that the system can mount SMB shares
  vegasyncor run <job>       runs a job now (name or ID)
  vegasyncor dry-run <job>   simulates a job without changing anything
  vegasyncor version         shows the version

The language follows the service setting; VEGASYNCOR_LANG=en|it overrides it.
`

func main() {
	initLanguage()
	cmd := "tui"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	var err error
	switch cmd {
	case "tui", "--no-mouse":
		mouse := cmd != "--no-mouse" && !hasArg("--no-mouse") && os.Getenv("VEGASYNCOR_NO_MOUSE") == ""
		err = tui.Run(api.NewClient(paths.Socket()), version, mouse)
	case "daemon":
		err = runDaemon()
	case "status":
		err = printStatus()
	case "check":
		os.Exit(runCheck())
	case "run", "dry-run":
		if len(os.Args) < 3 {
			err = errors.New(T("specify the job"))
		} else {
			err = runJob(os.Args[2], cmd == "dry-run")
		}
	case "version", "--version", "-v":
		fmt.Println("vegasyncor", version)
	case "help", "--help", "-h":
		fmt.Printf(T(usage), version)
	default:
		fmt.Fprintf(os.Stderr, T(usage), version)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, T("error:"), err)
		os.Exit(1)
	}
}

// initLanguage picks the language before contacting the service:
// VEGASYNCOR_LANG, otherwise the configuration (if readable), otherwise English.
func initLanguage() {
	if env := i18n.FromEnv(); env != "" {
		i18n.SetLang(env)
		return
	}
	if cfg, err := config.Load(paths.ConfigFile()); err == nil {
		i18n.SetLang(cfg.Language)
	}
}

// followService uses the language of the service, unless VEGASYNCOR_LANG is set.
func followService(st *api.Status) {
	if i18n.FromEnv() == "" && st.Language != "" {
		i18n.SetLang(st.Language)
	}
}

func runDaemon() error {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey && os.Getenv("JOURNAL_STREAM") != "" {
				return slog.Attr{} // journald already adds date and time
			}
			return a
		},
	})))
	d, err := daemon.New(version)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	return d.Run(ctx)
}

func findJob(c *api.Client, ref string) (*api.JobStatus, error) {
	st, err := c.Status()
	if err != nil {
		return nil, err
	}
	followService(st)
	for i, j := range st.Jobs {
		if j.Job.ID == ref || j.Job.Name == ref {
			return &st.Jobs[i], nil
		}
	}
	return nil, errors.New(Tf("job %q not found", ref))
}

func runJob(ref string, dry bool) error {
	c := api.NewClient(paths.Socket())
	j, err := findJob(c, ref)
	if err != nil {
		return err
	}
	if err := c.RunJob(j.Job.ID, dry); err != nil {
		return err
	}
	fmt.Println(Tf("Job %q started. Progress:", j.Job.Name))
	for {
		time.Sleep(time.Second)
		cur, err := findJob(c, j.Job.ID)
		if err != nil {
			return err
		}
		if cur.Current == nil {
			if cur.Last != nil {
				fmt.Printf("\r%s\n", Tf("Result: %s – %s", statusText(cur.Last.Status), cur.Last.Message))
				if cur.Last.Status == api.StatusError {
					os.Exit(1)
				}
			}
			return nil
		}
		line := cur.Current.Phase
		if p := cur.Current.Progress; p != nil {
			line = fmt.Sprintf("%3d%%  %s  %s  ETA %s", p.Percent, api.HumanBytes(p.Bytes), p.Speed, p.ETA)
		}
		fmt.Printf("\r%-70s", line)
	}
}

func statusText(s string) string {
	switch s {
	case api.StatusOK:
		return T("completed")
	case api.StatusWarning:
		return T("with warnings")
	case api.StatusError:
		return T("error")
	case api.StatusCancelled:
		return T("cancelled")
	case api.StatusSkipped:
		return T("skipped")
	case api.StatusRunning:
		return T("running")
	}
	return s
}

func printStatus() error {
	st, err := api.NewClient(paths.Socket()).Status()
	if err != nil {
		return err
	}
	followService(st)
	for _, w := range st.Warnings {
		fmt.Printf("%s %s\n\n", T("WARNING:"), w)
	}
	zone := st.ZoneAbbr
	if st.ZoneName != "" && st.ZoneName != st.ZoneAbbr {
		zone = st.ZoneName + " " + st.ZoneAbbr
	}
	fmt.Printf("%s %s (%s)\n\n", T("Server time:"), st.ServerTime.Format(i18n.DateTimeLayout()), zone)
	short := i18n.ShortDateTimeLayout()
	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, strings.Join([]string{T("JOB"), T("STATE"), T("LAST"), T("RESULT"), T("NEXT")}, "\t"))
	for _, j := range st.Jobs {
		state := T("active")
		if !j.Job.Enabled {
			state = T("paused")
		}
		if j.Current != nil {
			state = T("running")
		}
		last, res := "-", "-"
		if j.Last != nil {
			last = j.Last.Start.Format(short)
			res = statusText(j.Last.Status)
		}
		next := "-"
		if !j.Next.IsZero() {
			next = j.Next.Format(short)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", j.Job.Name, state, last, res, next)
	}
	return tw.Flush()
}

// runCheck prints the environment diagnostics; returns 1 if there are blocking problems.
func runCheck() int {
	fmt.Println(T("VegaSyncor environment check"))
	fmt.Println()
	code := 0
	for _, c := range mount.Diagnose() {
		icon := "  OK   "
		switch c.Level {
		case mount.CheckWarn:
			icon = "  " + T("WARN") + " "
		case mount.CheckFail:
			icon = "  " + T("ERR.") + " "
			code = 1
		}
		fmt.Printf("%s %s\n", icon, c.Name)
		if c.Detail != "" {
			for _, l := range wrapText(c.Detail, 70) {
				fmt.Printf("         %s\n", l)
			}
		}
	}
	fmt.Println()
	if code == 0 {
		fmt.Println(T("The system can run the syncs."))
	} else {
		fmt.Println(T("There are problems to fix: syncs with network folders will not work."))
	}
	return code
}

func wrapText(s string, width int) []string {
	var lines []string
	line := ""
	for _, w := range strings.Fields(s) {
		if line != "" && len([]rune(line))+1+len([]rune(w)) > width {
			lines = append(lines, line)
			line = w
		} else if line == "" {
			line = w
		} else {
			line += " " + w
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}

func hasArg(a string) bool {
	for _, v := range os.Args[1:] {
		if v == a {
			return true
		}
	}
	return false
}
