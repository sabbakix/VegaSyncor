package i18n

func init() {
	addItalian(map[string]string{
		"writing master key: %w": "scrittura chiave master: %w",
		"the master key %s is accessible to other users (permissions %o): use chmod 600": "la chiave master %s è accessibile ad altri utenti (permessi %o): usare chmod 600",
		"invalid master key %s": "chiave master %s non valida",
		"unknown secret format": "formato segreto sconosciuto",
		"truncated secret":      "segreto troncato",
		"cannot decrypt the password (has the master key changed?)": "impossibile decifrare la password (chiave master cambiata?)",
	})
}
