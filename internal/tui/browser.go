package tui

import (
	"path"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"

	"vegasyncor/internal/config"
)

// browser lets the user choose a folder by navigating (local or on a share).
type browser struct {
	target  string // key of the field to fill in
	loc     config.Location
	dirs    []string
	cur     int
	offset  int
	loading bool
	err     string

	// allowNew enables "New folder" (destination folders only)
	allowNew bool
	naming   bool // typing the name of the new folder
	input    textinput.Model
}

// picker is a simple list to choose a value from (e.g. shares).
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

// List layout: "Use this folder", then "New folder" (if allowed),
// then ".." (if not at the root), then the subfolders.

func (b *browser) newIndex() int {
	if b.allowNew {
		return 1
	}
	return -1
}

func (b *browser) parentIndex() int {
	if b.atRoot() {
		return -1
	}
	if b.allowNew {
		return 2
	}
	return 1
}

func (b *browser) firstDirIndex() int {
	n := 1
	if b.allowNew {
		n++
	}
	if !b.atRoot() {
		n++
	}
	return n
}

func (b *browser) items() []string {
	out := []string{"[ " + T("Use this folder") + " ]"}
	if b.allowNew {
		out = append(out, "[ + "+T("New folder")+" ]")
	}
	if !b.atRoot() {
		out = append(out, ".. ("+T("parent folder")+")")
	}
	for _, d := range b.dirs {
		out = append(out, "   "+d+"/")
	}
	return out
}

// startNaming opens the input for the name of a new folder.
func (b *browser) startNaming() {
	b.input = textinput.New()
	b.input.Prompt = ""
	b.input.Placeholder = T("e.g. Backup 2026")
	b.input.CharLimit = 200
	b.input.Width = 40
	b.input.Focus()
	b.naming = true
	b.err = ""
}

// child returns the path of the i-th child in the list (or "" if it is not a folder).
func (b *browser) child(i int) (string, bool) {
	idx := i - b.firstDirIndex()
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
