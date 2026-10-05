package i18n

// italian maps English texts to their Italian translation. The entries are
// split by area in the it_*.go files.
var italian = map[string]string{}

func addItalian(m map[string]string) {
	for k, v := range m {
		italian[k] = v
	}
}
