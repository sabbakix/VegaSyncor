package tui

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	zone "github.com/lrstanley/bubblezone"
)

// Filters of the run log: hide the unchanged rows, show only new, updated or
// deleted files, and recognise moved files (rsync reports a move as a deleted
// file plus a new file somewhere else).

type logFilter int

const (
	lfAll logFilter = iota
	lfChanges
	lfNew
	lfUpdated
	lfDeleted
	lfMoved
	logFilterCount
)

func (f logFilter) key() string { return [...]string{"a", "c", "n", "u", "d", "m"}[f] }

func (f logFilter) label() string {
	switch f {
	case lfAll:
		return T("All")
	case lfChanges:
		return T("Changes")
	case lfNew:
		return T("New")
	case lfUpdated:
		return T("Updated")
	case lfDeleted:
		return T("Deleted")
	}
	return T("Moved")
}

type entryKind int

const (
	ekOther     entryKind = iota // messages, rsync command and statistics
	ekProblem                    // errors and warnings: shown with every filter
	ekUnchanged                  // only attributes (e.g. folder times) changed
	ekNew
	ekUpdated
	ekDeleted
	ekMoved     // new file paired with a deleted one
	ekMovedFrom // the deleted half of a move (shown inside the ekMoved row)
)

type logEntry struct {
	kind entryKind
	path string // relative path; folders end with "/"
	from string // ekMoved: previous path
	line string // original line
}

func (e logEntry) isDir() bool { return strings.HasSuffix(e.path, "/") }

// parseLog classifies the lines of a run log and pairs the moved files.
func parseLog(text string) []logEntry {
	lines := strings.Split(text, "\n")
	out := make([]logEntry, 0, len(lines))
	for _, l := range lines {
		e := logEntry{line: l}
		switch {
		case isLogError(l) || isLogWarning(l):
			e.kind = ekProblem
		case strings.HasPrefix(l, "*deleting"):
			e.kind, e.path = ekDeleted, strings.TrimSpace(strings.TrimPrefix(l, "*deleting"))
		case len(l) > 12 && l[11] == ' ' && strings.IndexByte("<>ch.", l[0]) >= 0 && (l[1] == 'f' || l[1] == 'd'):
			// itemized change "YXcstpoguax path"
			e.path = l[12:]
			switch {
			case strings.Contains(l[2:11], "+++++++"):
				e.kind = ekNew
			case l[0] == '.' || l[1] == 'd':
				e.kind = ekUnchanged
			default:
				e.kind = ekUpdated
			}
		}
		out = append(out, e)
	}
	pairMoves(out)
	pairRenamedFolders(out)
	return out
}

// maxMovePairs bounds the comparisons for very common names (e.g. thousands
// of index.html): beyond it those files are left as new + deleted.
const maxMovePairs = 200_000

// pairMoves marks as moved a new file and a deleted file with the same name
// (folders with folders). When several candidates share the name, the pair
// whose paths end with the most equal folders wins (a folder moved elsewhere
// keeps its inner structure); ties are left unpaired rather than guessed.
func pairMoves(es []logEntry) {
	type group struct{ del, add []int }
	groups := map[string]*group{}
	for i, e := range es {
		if e.kind != ekNew && e.kind != ekDeleted {
			continue
		}
		k := strings.ToLower(baseName(e.path))
		if e.isDir() {
			k += "/"
		}
		g := groups[k]
		if g == nil {
			g = &group{}
			groups[k] = g
		}
		if e.kind == ekNew {
			g.add = append(g.add, i)
		} else {
			g.del = append(g.del, i)
		}
	}
	for _, g := range groups {
		if len(g.del) == 0 || len(g.add) == 0 || len(g.del)*len(g.add) > maxMovePairs {
			continue
		}
		used := map[int]bool{}
		for {
			best := 0
			for _, d := range g.del {
				for _, a := range g.add {
					if !used[d] && !used[a] {
						best = max(best, suffixMatch(es[d].path, es[a].path))
					}
				}
			}
			if best == 0 {
				break
			}
			var pairs [][2]int
			nd, na := map[int]int{}, map[int]int{}
			for _, d := range g.del {
				for _, a := range g.add {
					if !used[d] && !used[a] && suffixMatch(es[d].path, es[a].path) == best {
						pairs = append(pairs, [2]int{d, a})
						nd[d]++
						na[a]++
					}
				}
			}
			for _, p := range pairs {
				d, a := p[0], p[1]
				if nd[d] == 1 && na[a] == 1 {
					es[a].kind, es[a].from = ekMoved, es[d].path
					es[d].kind = ekMovedFrom
				}
				used[d], used[a] = true, true
			}
		}
	}
}

// pairRenamedFolders marks as moved a deleted folder and a new folder with a
// different name (a folder renamed or moved and renamed) when every file moved
// out of the old folder landed directly in the new one and nothing else was
// deleted from it.
func pairRenamedFolders(es []logEntry) {
	newDirs := map[string]int{} // path -> index of an unpaired new folder
	for i, e := range es {
		if e.kind == ekNew && e.isDir() {
			newDirs[e.path] = i
		}
	}
	targets := map[string]map[string]bool{} // old folder -> folders its files moved to
	lost := map[string]bool{}               // old folder with files really deleted
	for _, e := range es {
		switch {
		case e.kind == ekMoved && !e.isDir():
			from := parentDir(e.from)
			if targets[from] == nil {
				targets[from] = map[string]bool{}
			}
			targets[from][parentDir(e.path)] = true
		case e.kind == ekDeleted && !e.isDir():
			lost[parentDir(e.path)] = true
		}
	}
	for i, e := range es {
		if e.kind != ekDeleted || !e.isDir() || lost[e.path] || len(targets[e.path]) != 1 {
			continue
		}
		for to := range targets[e.path] {
			if j, ok := newDirs[to]; ok {
				es[j].kind, es[j].from = ekMoved, e.path
				es[i].kind = ekMovedFrom
				delete(newDirs, to)
			}
		}
	}
}

// parentDir returns the folder of a path, with the trailing "/" ("" at the top).
func parentDir(p string) string {
	p = strings.TrimSuffix(p, "/")
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[:i+1]
	}
	return ""
}

func pathParts(p string) []string {
	return strings.Split(strings.ToLower(strings.TrimSuffix(p, "/")), "/")
}

func baseName(p string) string {
	parts := strings.Split(strings.TrimSuffix(p, "/"), "/")
	return parts[len(parts)-1]
}

// suffixMatch counts the equal trailing path components (case-insensitive, as on SMB).
func suffixMatch(a, b string) int {
	pa, pb := pathParts(a), pathParts(b)
	n := 0
	for n < len(pa) && n < len(pb) && pa[len(pa)-1-n] == pb[len(pb)-1-n] {
		n++
	}
	return n
}

func (e logEntry) visible(f logFilter) bool {
	switch e.kind {
	case ekProblem:
		return f != lfAll
	case ekNew:
		return f == lfChanges || f == lfNew
	case ekUpdated:
		return f == lfChanges || f == lfUpdated
	case ekDeleted:
		return f == lfChanges || f == lfDeleted
	case ekMoved:
		return f == lfChanges || f == lfMoved
	}
	return false
}

// logCounts returns the number of rows of every filter (lfAll: all the file rows).
func logCounts(es []logEntry) [logFilterCount]int {
	var c [logFilterCount]int
	for _, e := range es {
		if e.kind >= ekUnchanged && e.kind != ekMovedFrom {
			c[lfAll]++
		}
		for f := lfChanges; f < logFilterCount; f++ {
			if e.kind != ekProblem && e.visible(f) {
				c[f]++
			}
		}
	}
	return c
}

// renderLog shows the rows of a filter (lfAll is the plain log, see colorizeLog).
func renderLog(es []logEntry, f logFilter) string {
	var b strings.Builder
	rows := 0
	for _, e := range es {
		if !e.visible(f) {
			continue
		}
		tag := func(s lipgloss.Style, sign, word string) string { return s.Render(sign + " " + pad(word, 10)) }
		switch e.kind {
		case ekProblem:
			b.WriteString(colorizeLog(e.line))
		case ekNew:
			if e.isDir() {
				b.WriteString(tag(sOK, "+", T("folder")) + e.path)
			} else {
				b.WriteString(tag(sOK, "+", T("new")) + e.path)
			}
			rows++
		case ekUpdated:
			b.WriteString(tag(sRun, "~", T("update")) + e.path)
			rows++
		case ekDeleted:
			b.WriteString(tag(sErr, "-", T("delete")) + e.path)
			rows++
		case ekMoved:
			b.WriteString(tag(sMove, ">", T("moved")) + renderMove(e.from, e.path))
			rows++
		}
		b.WriteByte('\n')
	}
	if rows == 0 {
		b.WriteString(sMuted.Render(Tf("No rows for the filter %q in this run.", f.label())) + "\n")
	}
	return b.String()
}

// compactMove splits a move into the folders both paths share at the start
// (prefix) and at the end, file name included (suffix), and the parts that
// differ; an empty part means "this folder" and is shown as ".".
func compactMove(from, to string) (prefix, oldMid, newMid, suffix string) {
	dir := strings.HasSuffix(to, "/")
	a := strings.Split(strings.TrimSuffix(from, "/"), "/")
	b := strings.Split(strings.TrimSuffix(to, "/"), "/")
	s := 0
	for s < len(a) && s < len(b) && a[len(a)-1-s] == b[len(b)-1-s] {
		s++
	}
	p := 0
	for p < len(a)-s && p < len(b)-s && a[p] == b[p] {
		p++
	}
	prefix = strings.Join(a[:p], "/")
	oldMid = strings.Join(a[p:len(a)-s], "/")
	newMid = strings.Join(b[p:len(b)-s], "/")
	suffix = strings.Join(a[len(a)-s:], "/")
	if dir {
		suffix += "/"
	}
	if oldMid == "" {
		oldMid = "."
	}
	if newMid == "" {
		newMid = "."
	}
	return
}

// renderMove writes a move like git does for renames: the shared parts once and
// only what changed inside braces, e.g. Projects/{2025 -> Archive/2025}/plan.dwg.
func renderMove(from, to string) string {
	prefix, oldMid, newMid, suffix := compactMove(from, to)
	var b strings.Builder
	if prefix != "" {
		b.WriteString(prefix + "/")
	}
	b.WriteString(sMuted.Render("{") + sErr.Render(oldMid) + sMuted.Render(" -> ") + sOK.Render(newMid) + sMuted.Render("}"))
	if suffix != "" && suffix != "/" {
		b.WriteString("/" + suffix)
	} else {
		b.WriteString(suffix)
	}
	return b.String()
}

// viewLogFilters is the bar of the filters, with the number of rows of each one.
func (m *Model) viewLogFilters() string {
	counts := logCounts(m.logEntries)
	var parts []string
	for f := lfAll; f < logFilterCount; f++ {
		text := f.label()
		if f != lfAll {
			text += " " + strconv.Itoa(counts[f])
		}
		item := sKey.Render(f.key()) + " " + sMuted.Render(text)
		if f == m.logFilter {
			item = sTabOn.Render(f.key() + " " + text)
		}
		parts = append(parts, zone.Mark("logf:"+f.key(), item))
	}
	return " " + sMuted.Render(T("Show:")) + " " + strings.Join(parts, sSep.Render(" │ "))
}

func (m *Model) setLogFilter(f logFilter) {
	m.logFilter = f
	m.refreshLog()
}

// refreshLog renders the log with the current filter: the plain log opens at the
// end (result and statistics), a filtered list at the top.
func (m *Model) refreshLog() {
	if m.logFilter == lfAll {
		m.logView.SetContent(colorizeLog(m.logText))
		m.logView.GotoBottom()
		return
	}
	m.logView.SetContent(renderLog(m.logEntries, m.logFilter))
	m.logView.GotoTop()
}

// logFilterKey handles the filter keys of the log view.
func (m *Model) logFilterKey(key string) bool {
	if key == "f" {
		m.setLogFilter((m.logFilter + 1) % logFilterCount)
		return true
	}
	for f := lfAll; f < logFilterCount; f++ {
		if key == f.key() {
			m.setLogFilter(f)
			return true
		}
	}
	return false
}
