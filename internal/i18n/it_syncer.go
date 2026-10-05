package i18n

func init() {
	addItalian(map[string]string{
		"some files were not copied (locked or without permission): see the log": "alcuni file non sono stati copiati (bloccati o senza permessi): vedere il log",
		"some files vanished during the copy":                                    "alcuni file sono spariti durante la copia",
		"rsync exit code %d: %s":                                                 "rsync codice %d: %s",
	})
}
