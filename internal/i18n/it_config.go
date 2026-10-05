package i18n

func init() {
	addItalian(map[string]string{
		// modes
		"Mirror":           "Mirror",
		"Mirror + archive": "Mirror + archivio",
		"Add only":         "Solo aggiunte",
		"The destination becomes identical to the source: files deleted at the source are deleted.": "La destinazione diventa identica alla sorgente: i file cancellati alla sorgente vengono cancellati.",
		"Like Mirror, but deleted or overwritten files are moved to %s/<date>.":                     "Come Mirror, ma i file cancellati o sovrascritti vengono spostati in %s/<data>.",
		"Copies new and changed files, never deletes anything in the destination.":                  "Copia file nuovi e modificati, non cancella mai nulla nella destinazione.",

		// validation
		"the connection name is required":                        "il nome della connessione è obbligatorio",
		"invalid host (e.g. 192.168.1.10 or server01)":           "host non valido (es. 192.168.1.10 oppure server01)",
		"user or domain contain characters that are not allowed": "utente o dominio contengono caratteri non ammessi",
		"invalid SMB version":                                    "versione SMB non valida",
		"the path cannot leave the share":                        "il percorso non può uscire dalla condivisione",
		"select a connection":                                    "selezionare una connessione",
		"invalid share name":                                     "nome condivisione non valido",
		"the local path must be absolute":                        "il percorso locale deve essere assoluto",
		"the root / cannot be used":                              "non è possibile usare la radice /",
		"unknown type":                                           "tipo sconosciuto",
		"the job name is required":                               "il nome del job è obbligatorio",
		"source":                                                 "sorgente",
		"destination":                                            "destinazione",
		"source and destination are the same":                    "sorgente e destinazione coincidono",
		"source and destination cannot be nested":                "sorgente e destinazione non possono essere annidate",
		"invalid number of archive days":                         "giorni di archivio non validi",
		"invalid mode":                                           "modalità non valida",
		"invalid bandwidth limit":                                "limite di banda non valido",
	})
}

func init() {
	addItalian(map[string]string{
		// schedule types
		"Manual only":      "Solo manuale",
		"At intervals":     "A intervalli",
		"Every day":        "Ogni giorno",
		"Days of the week": "Giorni della settimana",
		"Cron expression":  "Espressione cron",
		// days
		"Sun": "Dom", "Mon": "Lun", "Tue": "Mar", "Wed": "Mer", "Thu": "Gio", "Fri": "Ven", "Sat": "Sab",
		// schedule validation
		"invalid time %q (use HH:MM)":                 "orario %q non valido (usare HH:MM)",
		"invalid day of the week":                     "giorno della settimana non valido",
		"interval: enter the minutes (at least 1)":    "intervallo: indicare i minuti (almeno 1)",
		"time window: enter both start and end":       "fascia oraria: indicare sia inizio che fine",
		"enter at least one time (e.g. 13:00, 22:30)": "indicare almeno un orario (es. 13:00, 22:30)",
		"select at least one day":                     "selezionare almeno un giorno",
		"invalid cron expression: %v":                 "espressione cron non valida: %v",
		"invalid schedule type":                       "tipo di pianificazione non valido",
		// schedule descriptions
		"every day":    "tutti i giorni",
		"Mon-Fri":      "Lun-Ven",
		"manual":       "manuale",
		"every %d h":   "ogni %d h",
		"every %d min": "ogni %d min",
		"daily":        "ogni giorno",
	})
}

func init() {
	addItalian(map[string]string{
		"enter the folder name":                            "inserire il nome della cartella",
		"invalid folder name":                              "nome della cartella non valido",
		`the folder name cannot contain / \ : * ? " < > |`: `il nome della cartella non può contenere / \ : * ? " < > |`,
		"the folder name cannot end with a dot":            "il nome della cartella non può terminare con un punto",
		"the folder name is too long":                      "il nome della cartella è troppo lungo",
	})
}
