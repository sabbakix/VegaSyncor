package i18n

func init() {
	addItalian(map[string]string{
		// mount errors and hints
		"the password cannot contain line breaks":                          "la password non può contenere a capo",
		"wrong user or password, or insufficient permissions on the share": "utente o password errati, oppure permessi insufficienti sulla condivisione",
		"share does not exist or wrong path":                               "condivisione inesistente o percorso errato",
		"server unreachable or SMB version not supported (try setting it)": "server non raggiungibile oppure versione SMB non supportata (provare a impostarla)",
		"host name cannot be resolved: try the IP address":                 "nome host non risolvibile: provare con l'indirizzo IP",
		"SMB version not supported by the server: set it explicitly":       "versione SMB non supportata dal server: impostarla esplicitamente",
		"server unreachable on the network":                                "server non raggiungibile in rete",
		"the server does not accept SMB connections (port 445)":            "il server non accetta connessioni SMB (porta 445)",
		"server unreachable (timeout)":                                     "server non raggiungibile (timeout)",
		"the cifs-utils package is missing (apt install cifs-utils)":       "manca il pacchetto cifs-utils (apt install cifs-utils)",
		"privileged LXC container: enable the container's SMB/CIFS feature (Proxmox: pct set <ID> --features mount=cifs, then restart it); if already enabled, check user, password and domain": "container LXC privilegiato: abilitare la funzionalità SMB/CIFS del container (Proxmox: pct set <ID> --features mount=cifs, poi riavviarlo); se è già attiva, controllare utente, password e dominio",
		"the service runs in a container (%s) that may not allow mounts: %s":                                                                           "il servizio gira in un container (%s) che potrebbe non consentire i montaggi: %s",
		"the service is not running as root (start it with systemctl start vegasyncor)":                                                                "il servizio non è in esecuzione come root (avviarlo con systemctl start vegasyncor)",
		"the server refused access: check user, password and domain (leave the domain empty for a local Windows user) and try setting the SMB version": "il server ha rifiutato l'accesso: controllare utente, password e dominio (per un utente locale di Windows lasciare vuoto il dominio) e provare a impostare la versione SMB",
		"development mode: set VEGASYNCOR_DEV_SMB_ROOT to simulate SMB shares":                                                                         "modalità sviluppo: impostare VEGASYNCOR_DEV_SMB_ROOT per simulare le condivisioni SMB",
		"missing commands: %s (apt install cifs-utils rsync)":                                                                                          "comandi mancanti: %s (apt install cifs-utils rsync)",
		"%s is already mounted": "%s risulta già montato",
		"server unreachable":    "server non raggiungibile",
		"smbclient is not installed (apt install smbclient): enter the share name manually": "smbclient non installato (apt install smbclient): inserire il nome della condivisione a mano",

		// environment
		"use a privileged container with the SMB/CIFS feature enabled (Proxmox: restore the container backup with --unprivileged 0, then pct set <ID> --features mount=cifs) or a virtual machine": "usare un container privilegiato con la funzionalità SMB/CIFS attiva (Proxmox: ripristinare il backup del container con --unprivileged 0, poi pct set <ID> --features mount=cifs) oppure una macchina virtuale",
		"start the container with --privileged (or --cap-add SYS_ADMIN --cap-add DAC_READ_SEARCH) or install VegaSyncor directly on the host":                                                      "avviare il container con --privileged (oppure --cap-add SYS_ADMIN --cap-add DAC_READ_SEARCH) o installare VegaSyncor direttamente sull'host",
		"use a privileged container or a virtual machine": "usare un container privilegiato oppure una macchina virtuale",
		"an unprivileged container":                       "un container non privilegiato",
		"an unprivileged %s container":                    "un container %s non privilegiato",
		"VegaSyncor runs in %s: the kernel does not allow mounting network shares, so syncs with SMB folders will fail. Solution: %s": "VegaSyncor gira in %s: il kernel non consente di montare condivisioni di rete, quindi le sincronizzazioni con cartelle SMB falliranno. Soluzione: %s",
		"the service is not running as root: mounts will fail":                                                                        "il servizio non è in esecuzione come root: i montaggi falliranno",

		// check
		"Development mode":                 "Modalità sviluppo",
		"VEGASYNCOR_DEV=1: no real mounts": "VEGASYNCOR_DEV=1: nessun montaggio reale",
		"Root user":                        "Utente root",
		"run as root (sudo); the systemd service already runs as root": "eseguire come root (sudo); il servizio systemd gira già come root",
		"Mounting SMB shares":      "Montaggio condivisioni SMB",
		"Privileged LXC container": "Container LXC privilegiato",
		"make sure the SMB/CIFS feature is enabled (Proxmox: pct set <ID> --features mount=cifs)": "verificare che sia attiva la funzionalità SMB/CIFS (Proxmox: pct set <ID> --features mount=cifs)",
		"SMB mounts may not be allowed:": "i montaggi SMB potrebbero non essere consentiti:",
		"Environment":                    "Ambiente",
		"physical or virtual machine":    "macchina fisica o virtuale",
		"not installed (apt install %s)": "non installato (apt install %s)",
		"not installed: the list of shares will not be available (apt install smbclient)": "non installato: l'elenco delle condivisioni non sarà disponibile (apt install smbclient)",
	})
}
