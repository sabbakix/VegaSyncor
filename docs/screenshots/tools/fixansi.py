"""tmux capture-pane -e emits colour changes as a stream that continues across
lines, while freeze starts each line from scratch: re-emit the active state at
every line start. --trim-top drops the empty lines at the top (dialogs)."""
import re, sys

s = open(sys.argv[1], encoding="utf-8").read()
FG, BG = "38;2;201;209;217", "48;2;17;21;28"
sgr = re.compile(r"\x1b\[([0-9;]*)m")
state = {"fg": None, "bg": None, "bold": False, "rev": False}

def apply(params):
    p = params.split(";") if params else ["0"]
    i = 0
    while i < len(p):
        x = p[i]
        if x in ("38", "48") and i + 1 < len(p):
            n = 5 if p[i + 1] == "2" else 3
            state["fg" if x == "38" else "bg"] = ";".join(p[i:i + n]); i += n; continue
        if x in ("0", ""):
            state.update(fg=None, bg=None, bold=False, rev=False)
        elif x == "1": state["bold"] = True
        elif x == "22": state["bold"] = False
        elif x == "7": state["rev"] = True
        elif x == "27": state["rev"] = False
        elif x == "39": state["fg"] = None
        elif x == "49": state["bg"] = None
        i += 1

def current():
    parts = ["0", "1"] if state["bold"] else ["0"]
    fg, bg = state["fg"] or FG, state["bg"] or BG
    if state["rev"]:
        fg, bg = "38" + bg[2:], "48" + fg[2:]
    return "\x1b[" + ";".join(parts + [fg, bg]) + "m"

lines = s.split("\n")
while lines and lines[-1].strip() == "":
    lines.pop()
visible = lambda l: sgr.sub("", l).strip()
if "--trim-top" in sys.argv:
    while lines and visible(lines[0]) == "":
        for m in sgr.finditer(lines[0]): apply(m.group(1))
        lines.pop(0)
out = []
for line in lines:
    prefix = current()
    def repl(m):
        apply(m.group(1)); return current()
    out.append(prefix + sgr.sub(repl, line))
open(sys.argv[2], "w", encoding="utf-8").write("\n".join(out))
