// VegaSyncor: sincronizzazione pianificata di cartelle di rete (SMB/CIFS) per backup.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"vegasyncor/internal/api"
	"vegasyncor/internal/daemon"
	"vegasyncor/internal/mount"
	"vegasyncor/internal/paths"
	"vegasyncor/internal/tui"
)

var version = "dev"

const usage = `VegaSyncor %s – sincronizzazione di cartelle di rete per backup

Uso:
  vegasyncor                 apre l'interfaccia di gestione (TUI)
  vegasyncor --no-mouse      apre la TUI senza supporto del mouse
  vegasyncor daemon          avvia il servizio (normalmente tramite systemd)
  vegasyncor status          mostra lo stato dei job
  vegasyncor check           verifica che il sistema possa montare le condivisioni SMB
  vegasyncor run <job>       avvia subito un job (nome o ID)
  vegasyncor dry-run <job>   simula un job senza modificare nulla
  vegasyncor version         mostra la versione
`

func main() {
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
			err = fmt.Errorf("specificare il job")
		} else {
			err = runJob(os.Args[2], cmd == "dry-run")
		}
	case "version", "--version", "-v":
		fmt.Println("vegasyncor", version)
	case "help", "--help", "-h":
		fmt.Printf(usage, version)
	default:
		fmt.Fprintf(os.Stderr, usage, version)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "errore:", err)
		os.Exit(1)
	}
}

func runDaemon() error {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey && os.Getenv("JOURNAL_STREAM") != "" {
				return slog.Attr{} // journald aggiunge già data e ora
			}
			return a
		},
	})))
	d, err := daemon.New(version)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return d.Run(ctx)
}

func findJob(c *api.Client, ref string) (*api.JobStatus, error) {
	st, err := c.Status()
	if err != nil {
		return nil, err
	}
	for i, j := range st.Jobs {
		if j.Job.ID == ref || j.Job.Name == ref {
			return &st.Jobs[i], nil
		}
	}
	return nil, fmt.Errorf("job %q non trovato", ref)
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
	fmt.Printf("Job %q avviato. Avanzamento:\n", j.Job.Name)
	for {
		time.Sleep(time.Second)
		cur, err := findJob(c, j.Job.ID)
		if err != nil {
			return err
		}
		if cur.Current == nil {
			if cur.Last != nil {
				fmt.Printf("\rEsito: %s – %s\n", cur.Last.Status, cur.Last.Message)
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

func printStatus() error {
	st, err := api.NewClient(paths.Socket()).Status()
	if err != nil {
		return err
	}
	for _, w := range st.Warnings {
		fmt.Printf("ATTENZIONE: %s\n\n", w)
	}
	zone := st.ZoneAbbr
	if st.ZoneName != "" && st.ZoneName != st.ZoneAbbr {
		zone = st.ZoneName + " " + st.ZoneAbbr
	}
	fmt.Printf("Ora del server: %s (%s)\n\n", st.ServerTime.Format("02/01/2006 15:04:05"), zone)
	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "JOB\tSTATO\tULTIMA\tESITO\tPROSSIMA")
	for _, j := range st.Jobs {
		state := "attivo"
		if !j.Job.Enabled {
			state = "sospeso"
		}
		if j.Current != nil {
			state = "in corso"
		}
		last, res := "-", "-"
		if j.Last != nil {
			last = j.Last.Start.Format("02/01 15:04")
			res = j.Last.Status
		}
		next := "-"
		if !j.Next.IsZero() {
			next = j.Next.Format("02/01 15:04")
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", j.Job.Name, state, last, res, next)
	}
	return tw.Flush()
}

// runCheck stampa la diagnostica dell'ambiente; restituisce 1 se ci sono problemi bloccanti.
func runCheck() int {
	fmt.Println("Verifica dell'ambiente VegaSyncor")
	fmt.Println()
	code := 0
	for _, c := range mount.Diagnose() {
		icon := "  OK   "
		switch c.Level {
		case mount.CheckWarn:
			icon = "  AVV. "
		case mount.CheckFail:
			icon = "  ERR. "
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
		fmt.Println("Il sistema può eseguire le sincronizzazioni.")
	} else {
		fmt.Println("Ci sono problemi da risolvere: le sincronizzazioni con cartelle di rete non funzioneranno.")
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
