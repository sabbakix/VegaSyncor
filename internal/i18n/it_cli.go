package i18n

func init() {
	addItalian(map[string]string{
		`VegaSyncor %s – sync of network folders for backup

Usage:
  vegasyncor                 opens the management interface (TUI)
  vegasyncor --no-mouse      opens the TUI without mouse support
  vegasyncor daemon          starts the service (normally through systemd)
  vegasyncor status          shows the status of the jobs
  vegasyncor check           checks that the system can mount SMB shares
  vegasyncor run <job>       runs a job now (name or ID)
  vegasyncor dry-run <job>   simulates a job without changing anything
  vegasyncor firewall off    disables the firewall (emergency, e.g. from the console)
  vegasyncor export [file]   exports the whole configuration, encrypted with a password
  vegasyncor import <file>   replaces the configuration with an export (new server)
  vegasyncor version         shows the version

The language follows the service setting; VEGASYNCOR_LANG=en|it overrides it.
`: `VegaSyncor %s – sincronizzazione di cartelle di rete per backup

Uso:
  vegasyncor                 apre l'interfaccia di gestione (TUI)
  vegasyncor --no-mouse      apre la TUI senza supporto del mouse
  vegasyncor daemon          avvia il servizio (normalmente tramite systemd)
  vegasyncor status          mostra lo stato dei job
  vegasyncor check           verifica che il sistema possa montare le condivisioni SMB
  vegasyncor run <job>       avvia subito un job (nome o ID)
  vegasyncor dry-run <job>   simula un job senza modificare nulla
  vegasyncor firewall off    disattiva il firewall (emergenza, es. dalla console)
  vegasyncor export [file]   esporta tutta la configurazione, cifrata con una password
  vegasyncor import <file>   sostituisce la configurazione con un'esportazione (nuovo server)
  vegasyncor version         mostra la versione

La lingua segue l'impostazione del servizio; VEGASYNCOR_LANG=en|it la sostituisce.
`,
		"specify the job":               "specificare il job",
		"error:":                        "errore:",
		"job %q not found":              "job %q non trovato",
		"Job %q started. Progress:":     "Job %q avviato. Avanzamento:",
		"Result: %s – %s":               "Esito: %s – %s",
		"completed":                     "completato",
		"with warnings":                 "con avvisi",
		"error":                         "errore",
		"skipped":                       "saltato",
		"running":                       "in corso",
		"WARNING:":                      "ATTENZIONE:",
		"Server time:":                  "Ora del server:",
		"JOB":                           "JOB",
		"STATE":                         "STATO",
		"LAST":                          "ULTIMA",
		"RESULT":                        "ESITO",
		"NEXT":                          "PROSSIMA",
		"active":                        "attivo",
		"paused":                        "sospeso",
		"VegaSyncor environment check":  "Verifica dell'ambiente VegaSyncor",
		"WARN":                          "AVV.",
		"ERR.":                          "ERR.",
		"The system can run the syncs.": "Il sistema può eseguire le sincronizzazioni.",
		"There are problems to fix: syncs with network folders will not work.": "Ci sono problemi da risolvere: le sincronizzazioni con cartelle di rete non funzioneranno.",
	})
}
