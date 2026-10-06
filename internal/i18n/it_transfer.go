package i18n

func init() {
	addItalian(map[string]string{
		// package transfer
		"wrong password or damaged file":                                           "password errata o file danneggiato",
		"the password must be at least %d characters long":                         "la password deve essere lunga almeno %d caratteri",
		"this is not a VegaSyncor export file":                                     "questo non è un file di esportazione di VegaSyncor",
		"export file made by a newer VegaSyncor version: update this server first": "file esportato da una versione più recente di VegaSyncor: aggiorna prima questo server",
		"duplicate job name %q in the export file":                                 "nome di job %q duplicato nel file di esportazione",
		"job": "job",

		// service and API
		"some syncs are running: wait for them to finish or stop them before importing":                                   "alcune sincronizzazioni sono in corso: attendi che finiscano o fermale prima di importare",
		"Imported %d connections and %d syncs.":                                                                           "Importate %d connessioni e %d sincronizzazioni.",
		"The firewall of this server is active: its settings were kept.":                                                  "Il firewall di questo server è attivo: le sue impostazioni sono state mantenute.",
		"The firewall settings were imported but the firewall is off: check them in the Firewall tab before enabling it.": "Le impostazioni del firewall sono state importate ma il firewall è spento: verificale nella scheda Firewall prima di attivarlo.",

		// command line
		"The export file contains the passwords of the connections: they are encrypted with the password you choose now, which will be asked when importing.": "Il file di esportazione contiene le password delle connessioni: sono cifrate con la password che scegli ora, che verrà chiesta durante l'importazione.",
		"Password for the export file: ":          "Password per il file di esportazione: ",
		"Repeat the password: ":                   "Ripeti la password: ",
		"Configuration exported to %s":            "Configurazione esportata in %s",
		"usage: vegasyncor import <file> [--yes]": "uso: vegasyncor import <file> [--yes]",
		"Export of %s, made on %s.":               "Esportazione di %s, creata il %s.",
		"The import replaces ALL connections, syncs and settings of this server. Continue? [y/N] ": "L'importazione sostituisce TUTTE le connessioni, sincronizzazioni e impostazioni di questo server. Continuare? [s/N] ",
		"import cancelled":              "importazione annullata",
		"Password of the export file: ": "Password del file di esportazione: ",
		"The service is not running: the configuration was updated directly and is used at the next start.": "Il servizio non è in esecuzione: la configurazione è stata aggiornata direttamente e sarà usata al prossimo avvio.",

		// TUI
		"Export configuration":   "Esporta configurazione",
		"Import configuration":   "Importa configurazione",
		"File":                   "File",
		"Export file":            "File di esportazione",
		"Protection":             "Protezione",
		"Repeat password":        "Ripeti password",
		"at least %d characters": "almeno %d caratteri",
		"connections with their passwords, syncs and settings; copy this file to the new server and import it there": "connessioni con le loro password, sincronizzazioni e impostazioni; copia questo file sul nuovo server e importalo lì",
		"encrypts the file (AES-256): it will be asked when importing; without it the file cannot be opened":         "cifra il file (AES-256): verrà chiesta durante l'importazione; senza di essa il file non può essere aperto",
		"file made with Export on the old server; it replaces ALL connections, syncs and settings of this server":    "file creato con Esporta sul vecchio server; sostituisce TUTTE le connessioni, sincronizzazioni e impostazioni di questo server",
		"the password chosen when the file was exported":                                                             "la password scelta quando il file è stato esportato",
		"enter the file name":                                              "inserisci il nome del file",
		"the passwords do not match":                                       "le password non coincidono",
		"%s already exists: choose another name":                           "%s esiste già: scegli un altro nome",
		"Import the configuration of %s exported on %s?":                   "Importare la configurazione di %s esportata il %s?",
		"ALL connections, syncs and settings of this server are replaced.": "TUTTE le connessioni, sincronizzazioni e impostazioni di questo server vengono sostituite.",
		"Configuration exported":                                           "Configurazione esportata",
		"Configuration imported":                                           "Configurazione importata",
		"File: %s":                                                         "File: %s",
		"Copy it to the new server, install VegaSyncor there and use Import (key I in the Connections tab, or: vegasyncor import <file>). Keep the file and its password safe.": "Copialo sul nuovo server, installa lì VegaSyncor e usa Importa (tasto I nella scheda Connessioni, oppure: vegasyncor import <file>). Conserva al sicuro il file e la sua password.",
		"Moving from another server? Press %s to import its configuration.":                                                                                                     "Stai migrando da un altro server? Premi %s per importarne la configurazione.",
		"n new · I import · tab next tab · L language · q quit":                                                                                                                 "n nuova · I importa · tab scheda · L lingua · q esci",
	})
}
