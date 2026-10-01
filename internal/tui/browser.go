package tui

import (
	"path"
	"strings"

	"vegasyncor/internal/config"
)

// browser permette di scegliere una cartella navigando (locale o su condivisione).
type browser struct {
	target  string // chiave del campo da compilare
	loc     config.Location
	dirs    []string
	cur     int
	offset  int
	loading bool
	err     string
}

// picker è un semplice elenco da cui scegliere un valore (es. condivisioni).
type picker struct {
	title  string
	target string
	items  []string
	cur    int
}

func (b *browser) atRoot() bool {
	if b.loc.Type == config.LocLocal {
		return b.loc.Path == "/" || b.loc.Path == ""
	}
	return b.loc.Path == ""
}

func (b *browser) items() []string {
	out := []string{"✔ Usa questa cartella"}
	if !b.atRoot() {
		out = append(out, "↰ .. cartella superiore")
	}
	for _, d := range b.dirs {
		out = append(out, "▸ "+d+"/")
	}
	return out
}

// child restituisce il percorso del figlio i-esimo della lista (o "" se non è una cartella).
func (b *browser) child(i int) (string, bool) {
	idx := i - 1
	if !b.atRoot() {
		idx--
	}
	if idx < 0 || idx >= len(b.dirs) {
		return "", false
	}
	if b.loc.Type == config.LocLocal {
		return path.Join(b.loc.Path, b.dirs[idx]), true
	}
	return strings.TrimPrefix(path.Join(b.loc.Path, b.dirs[idx]), "/"), true
}

func (b *browser) parent() string {
	p := path.Dir(b.loc.Path)
	if b.loc.Type == config.LocSMB && (p == "." || p == "/") {
		return ""
	}
	return p
}

func (b *browser) displayPath() string {
	if b.loc.Type == config.LocLocal {
		return b.loc.Path
	}
	p := strings.ReplaceAll(b.loc.Path, "/", `\`)
	return `\` + b.loc.Share + `\` + p
}

func listWindow(cur, offset, n, height int) int {
	if height <= 0 || n <= height {
		return 0
	}
	if cur < offset {
		offset = cur
	}
	if cur >= offset+height {
		offset = cur - height + 1
	}
	return max(0, min(offset, n-height))
}
