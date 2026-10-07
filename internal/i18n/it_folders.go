package i18n

func init() {
	addItalian(map[string]string{
		// config
		"invalid number of days for the log compression":                                    "numero di giorni non valido per la compressione dei log",
		"it cannot be in the source or contain it":                                          "non può essere nella sorgente né contenerla",
		"it cannot be the destination or contain it (choose a subfolder or another folder)": "non può essere la destinazione né contenerla (scegli una sottocartella o un'altra cartella)",
		"log folder":           "cartella dei log",
		"deleted items folder": "cartella degli eliminati",
		"%s: it overlaps with the destination of the job %q, whose mirror would delete it":                              "%s: si sovrappone alla destinazione del job %q, il cui mirror la cancellerebbe",
		"the destination overlaps with a folder used by the job %q (deleted items or logs): the mirror would delete it": "la destinazione si sovrappone a una cartella usata dal job %q (eliminati o log): il mirror la cancellerebbe",
		"deleted items folder: it is already used by the job %q":                                                        "cartella degli eliminati: è già usata dal job %q",

		// service
		"saving the log":                        "salvataggio del log",
		"warning: compressing the old logs: %v": "attenzione: compressione dei vecchi log: %v",
		"logs: %d logs older than %d days moved into the monthly zip files": "log: %d log più vecchi di %d giorni spostati negli zip mensili",
		"Deleted to:  %s":                                  "Eliminati in: %s",
		"Log copy:    %s":                                  "Copia log:    %s",
		"connecting to the deleted items folder":           "connessione alla cartella degli eliminati",
		"warning: unmounting the deleted items folder: %v": "attenzione: smontaggio della cartella degli eliminati: %v",
		"WARNING: log copy not saved in %s: %v":            "ATTENZIONE: copia del log non salvata in %s: %v",
		"log copy not saved":                               "copia del log non salvata",

		// job form and details
		"Deleted items folder":                                             "Cartella eliminati",
		"Default: inside the destination":                                  "Predefinita: nella destinazione",
		"deleted and overwritten files go to %s/<date> in the destination": "i file eliminati e sovrascritti vanno in %s/<data> nella destinazione",
		"deleted and overwritten files go to a dated subfolder of this folder; the same number of days applies": "i file eliminati e sovrascritti vanno in una sottocartella datata di questa cartella; vale lo stesso numero di giorni",
		"PC, server or NAS where the deleted items are kept":                                                    "PC, server o NAS dove conservare i file eliminati",
		"shared folder for the deleted items (Enter to list them)":                                              "cartella condivisa per i file eliminati (Invio per elencarle)",
		"e.g. Deleted/Accounting": "es. Eliminati/Contabilita",
		"Enter to browse the folders; it can also be a subfolder of the destination": "Invio per sfogliare le cartelle; può anche essere una sottocartella della destinazione",
		"Log folder":                  "Cartella log",
		"None (logs only in History)": "Nessuna (log solo nello Storico)",
		"also save the log of every run as a file in a folder of your choice (dry runs too)": "salva anche il log di ogni esecuzione come file in una cartella a scelta (anche le simulazioni)",
		"PC, server or NAS where the logs are saved":                                         "PC, server o NAS dove salvare i log",
		"shared folder for the logs (Enter to list them)":                                    "cartella condivisa per i log (Invio per elencarle)",
		"e.g. Logs": "es. Log",
		"Enter to browse the folders; one file per run: <job>_<date>.log": "Invio per sfogliare le cartelle; un file per esecuzione: <job>_<data>.log",
		"Zip logs after (days)": "Zip dei log dopo (gg)",
		"empty = never":         "vuoto = mai",
		"logs older than N days are moved into one zip per month (<job>_logs_<YYYY-MM>.zip); nothing is deleted": "i log più vecchi di N giorni vengono spostati in uno zip al mese (<job>_logs_<AAAA-MM>.zip); nulla viene cancellato",
		"zip logs after":      "zip dei log dopo",
		"Deleted to":          "Eliminati in",
		"(zip after %d days)": "(zip dopo %d giorni)",
		"Log copy":            "Copia log",
		"Like Mirror, but deleted or overwritten files are moved to dated subfolders of %s.": "Come Mirror, ma i file eliminati o sovrascritti vengono spostati in sottocartelle datate di %s.",
	})
}
