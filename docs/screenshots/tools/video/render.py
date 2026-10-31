"""Renders the timeline written by record.py to frames (headless Chromium) and
encodes them to MP4 (ffmpeg from imageio-ffmpeg).
    render.py TIMELINE.json OUT.mp4"""
import base64, hashlib, html, json, pathlib, re, subprocess, sys
import imageio_ffmpeg
from playwright.sync_api import sync_playwright

tl = json.loads(pathlib.Path(sys.argv[1]).read_text())
OUTFILE = pathlib.Path(sys.argv[2]).resolve()
WORK = OUTFILE.parent / "frames"
WORK.mkdir(parents=True, exist_ok=True)
COLS, ROWS = tl["cols"], tl["rows"]
FG, BG = "#DCE3EE", "#1B2333"
CHAPTERS = ["Installation", "The dashboard", "Step 1 · Add a connection", "Step 2 · Create a sync",
            "Step 3 · Dry run and first copy", "History and logs", "Off-site copy to OneDrive",
            "Firewall and configuration export"]

CSS = f"""
* {{ box-sizing: border-box; margin: 0; padding: 0; }}
html, body {{ width: 1920px; height: 1080px; overflow: hidden; }}
body {{ background: radial-gradient(1400px 900px at 50% 20%, #17223a 0%, #0b101b 70%);
  color: {FG}; font-family: 'DejaVu Sans', sans-serif; }}
.acc {{ color: #6CB6FF; }}
.side {{ position: absolute; left: 36px; top: 64px; width: 290px; }}
.brand {{ font-size: 30px; font-weight: bold; letter-spacing: .5px; margin-bottom: 34px; }}
.brand small {{ display: block; font-size: 15px; font-weight: normal; color: #8E9AB0; margin-top: 6px; letter-spacing: 0; }}
.ch {{ font-size: 18px; color: #66738c; padding: 9px 0 9px 16px; border-left: 3px solid #26324a; line-height: 1.3; }}
.ch.done {{ color: #8E9AB0; }}
.ch.on {{ color: #fff; border-left-color: #6CB6FF; font-weight: bold; }}
.win {{ position: absolute; left: 350px; top: 46px; border-radius: 12px; background: {BG};
  box-shadow: 0 24px 70px rgba(0,0,0,.55), 0 0 0 1px #2c3852; overflow: hidden; }}
.bar {{ height: 34px; background: #222c40; display: flex; align-items: center; padding: 0 14px; gap: 8px;
  font-size: 14px; color: #8E9AB0; }}
.dot {{ width: 13px; height: 13px; border-radius: 50%; }}
.bar .t {{ margin-left: 14px; }}
.term {{ width: calc({COLS}ch + 24px); padding: 10px 12px 12px; font-family: 'DejaVu Sans Mono', monospace; font-size: 17px; }}
.term div {{ height: 20px; line-height: 20px; white-space: pre; }}
.img {{ position: absolute; left: 350px; top: 40px; width: 1254px; height: 890px; object-fit: contain; }}
.cap {{ position: absolute; left: 350px; width: 1254px; top: 948px; height: 110px; display: flex;
  align-items: center; justify-content: center; text-align: center; font-size: 29px; line-height: 1.35;
  color: #fff; padding: 0 30px; }}
.key {{ position: absolute; left: 1632px; top: 420px; width: 250px; text-align: center; }}
.key .l {{ font-size: 15px; color: #8E9AB0; letter-spacing: 2px; margin-bottom: 14px; }}
.key .k {{ display: inline-block; min-width: 92px; padding: 18px 24px; border-radius: 14px; font-size: 40px;
  font-weight: bold; font-family: 'DejaVu Sans Mono', monospace; color: #0b101b; background: #DCE3EE;
  box-shadow: 0 6px 0 #8E9AB0, 0 10px 30px rgba(0,0,0,.5); }}
.slide {{ position: absolute; inset: 0; padding: 110px 150px; }}
h1 {{ font-size: 120px; letter-spacing: 2px; }}
h2 {{ font-size: 58px; margin-bottom: 50px; }}
.sub {{ font-size: 44px; margin-top: 24px; color: #fff; }}
.lead {{ font-size: 28px; margin-top: 36px; color: #B4BECE; line-height: 1.5; max-width: 1300px; }}
.foot {{ position: absolute; left: 150px; bottom: 90px; font-size: 24px; color: #8E9AB0; }}
.grid {{ display: grid; grid-template-columns: 1fr 1fr; gap: 34px 60px; }}
.it {{ display: flex; gap: 24px; align-items: flex-start; transition: none; }}
.it .n {{ flex: none; width: 58px; height: 58px; border-radius: 14px; background: #2D4A78; color: #fff;
  font-size: 28px; font-weight: bold; display: flex; align-items: center; justify-content: center; }}
.it b {{ display: block; font-size: 31px; margin-bottom: 8px; }}
.it span {{ font-size: 23px; color: #B4BECE; line-height: 1.4; }}
.hidden {{ opacity: .0; }}
.flow {{ display: flex; align-items: center; gap: 0; margin-top: 30px; }}
.box {{ border: 2px solid #34425f; border-radius: 18px; padding: 28px 30px; background: #141c2c; width: 450px; }}
.box.mid {{ border-color: #6CB6FF; background: #16233a; width: 470px; }}
.box h3 {{ font-size: 21px; letter-spacing: 3px; color: #8E9AB0; margin-bottom: 20px; }}
.box.mid h3 {{ color: #6CB6FF; }}
.box p {{ font-size: 25px; padding: 9px 0; border-top: 1px solid #26324a; }}
.box p:first-of-type {{ border-top: 0; }}
.box p em {{ font-style: normal; color: #8E9AB0; font-size: 20px; display: block; margin-top: 3px; }}
.arr {{ width: 150px; text-align: center; font-size: 19px; color: #8E9AB0; }}
.arr div {{ font-size: 54px; color: #6CB6FF; line-height: 1; }}
.code {{ font-family: 'DejaVu Sans Mono', monospace; font-size: 22px; background: #141c2c; border: 1px solid #34425f;
  border-radius: 12px; padding: 22px 26px; margin-top: 18px; color: #fff; }}
.code .p {{ color: #7EE787; }}
.cols {{ display: flex; gap: 70px; margin-top: 46px; font-size: 25px; color: #B4BECE; line-height: 1.7; }}
"""


# ---- ANSI -> HTML -----------------------------------------------------------------------
SGR = re.compile(r"\x1b\[([0-9;]*)m")
XTERM16 = ["#000000", "#cd3131", "#0dbc79", "#e5e510", "#2472c8", "#bc3fbc", "#11a8cd", "#e5e5e5",
           "#666666", "#f14c4c", "#23d18b", "#f5f543", "#3b8eea", "#d670d6", "#29b8db", "#ffffff"]


def c256(n):
    if n < 16:
        return XTERM16[n]
    if n < 232:
        n -= 16
        v = [0, 95, 135, 175, 215, 255]
        return "#%02x%02x%02x" % (v[n // 36], v[n // 6 % 6], v[n % 6])
    g = 8 + (n - 232) * 10
    return "#%02x%02x%02x" % (g, g, g)


def ansi_html(raw):
    st = {"fg": None, "bg": None, "b": False, "i": False, "u": False, "r": False, "d": False}

    def apply(params):
        p = [int(x) if x else 0 for x in (params.split(";") if params else ["0"])]
        i = 0
        while i < len(p):
            x = p[i]
            if x in (38, 48) and i + 1 < len(p):
                if p[i + 1] == 2:
                    col, n = "#%02x%02x%02x" % tuple(p[i + 2:i + 5]), 5
                else:
                    col, n = c256(p[i + 2]), 3
                st["fg" if x == 38 else "bg"] = col
                i += n
                continue
            if x == 0:
                st.update(fg=None, bg=None, b=False, i=False, u=False, r=False, d=False)
            elif x == 1: st["b"] = True
            elif x == 2: st["d"] = True
            elif x == 3: st["i"] = True
            elif x == 4: st["u"] = True
            elif x == 7: st["r"] = True
            elif x == 22: st["b"] = st["d"] = False
            elif x == 23: st["i"] = False
            elif x == 24: st["u"] = False
            elif x == 27: st["r"] = False
            elif x == 39: st["fg"] = None
            elif x == 49: st["bg"] = None
            elif 30 <= x <= 37: st["fg"] = XTERM16[x - 30]
            elif 90 <= x <= 97: st["fg"] = XTERM16[x - 82]
            elif 40 <= x <= 47: st["bg"] = XTERM16[x - 40]
            elif 100 <= x <= 107: st["bg"] = XTERM16[x - 92]
            i += 1

    def style():
        fg, bg = st["fg"] or FG, st["bg"]
        if st["r"]:
            fg, bg = bg or BG, fg
        s = f"color:{fg};"
        if bg:
            s += f"background:{bg};"
        if st["b"]: s += "font-weight:bold;"
        if st["i"]: s += "font-style:italic;"
        if st["u"]: s += "text-decoration:underline;"
        if st["d"]: s += "opacity:.6;"
        return s

    lines = raw.split("\n")[:ROWS]
    lines += [""] * (ROWS - len(lines))
    out = []
    for line in lines:
        parts, pos = [], 0
        for m in SGR.finditer(line):
            if m.start() > pos:
                parts.append(f'<span style="{style()}">{html.escape(line[pos:m.start()])}</span>')
            apply(m.group(1))
            pos = m.end()
        rest = re.sub(r"\x1b\[[0-9;?]*[A-Za-z]", "", line[pos:])
        if rest:
            parts.append(f'<span style="{style()}">{html.escape(rest)}</span>')
        out.append("<div>" + "".join(parts) + "</div>")
    return "".join(out)


# ---- pages ------------------------------------------------------------------------------
def side(chapter):
    idx = CHAPTERS.index(chapter) if chapter in CHAPTERS else -1
    items = "".join(f'<div class="ch {"on" if i == idx else "done" if i < idx else ""}">{html.escape(c)}</div>'
                    for i, c in enumerate(CHAPTERS))
    return f'<div class="side"><div class="brand">Vega<span class="acc">Syncor</span><small>Security-focused backups</small></div>{items}</div>'


def caption(c):
    return f'<div class="cap">{html.escape(c)}</div>' if c else ""


def keycap(k):
    if not k:
        return ""
    names = {"Down": "↓", "Up": "↑", "Enter": "Enter", "Escape": "Esc", "BSpace": "⌫", "End": "End"}
    return f'<div class="key"><div class="l">KEY</div><div class="k">{html.escape(names.get(k, k))}</div></div>'


def term_page(f, key):
    title = "admin@backup01: ~" if f["chapter"] == "Installation" and "VegaSyncor   1 Syncs" not in tl["screens"][f["screen"]] \
        else "admin@backup01: sudo vegasyncor"
    return (side(f["chapter"]) +
            f'<div class="win"><div class="bar"><span class="dot" style="background:#ff5f57"></span>'
            f'<span class="dot" style="background:#febc2e"></span><span class="dot" style="background:#28c840"></span>'
            f'<span class="t">{title}</span></div><div class="term">{ansi_html(tl["screens"][f["screen"]])}</div></div>'
            + caption(f["cap"]) + keycap(key))


IMG_CACHE = {}


def image_page(f):
    if f["src"] not in IMG_CACHE:
        IMG_CACHE[f["src"]] = base64.b64encode(pathlib.Path(f["src"]).read_bytes()).decode()
    return (side(f["chapter"]) + f'<img class="img" src="data:image/png;base64,{IMG_CACHE[f["src"]]}">'
            + caption(f["cap"]))


WHY = [("Read-only sources", "Shares are mounted read-only by the kernel: a backup can never change or delete the originals."),
       ("Versioned copies", "Deleted and overwritten files are moved to a dated archive and kept for N days."),
       ("Fail-safe", "An empty or unreachable source never wipes the backup: the mirror is blocked."),
       ("Encrypted secrets", "Passwords are encrypted with AES-256-GCM and never appear in logs or processes."),
       ("Encrypted off-site copy", "Copies to OneDrive, encrypted on the server before the upload."),
       ("Hardened backup server", "Firewall lockdown: in only SSH from the admins, out only SMB to the backed-up hosts.")]


def slide_page(f):
    s = f["slide"]
    if s == "title":
        return ('<div class="slide" style="padding-top:300px"><h1>Vega<span class="acc">Syncor</span></h1>'
                '<div class="sub">Security-focused backups for Linux servers</div>'
                '<div class="lead">Scheduled, read-only, versioned copies of network shares and folders —<br>'
                'to a local disk, a NAS or OneDrive — managed from a terminal interface, also over SSH.</div>'
                '<div class="foot">v0.7.1 · Debian / Ubuntu · amd64 / arm64 · English and Italian</div></div>')
    if s == "why":
        items = "".join(f'<div class="it{"" if i < f["reveal"] else " hidden"}"><div class="n">{i + 1}</div>'
                        f'<div><b>{html.escape(t)}</b><span>{html.escape(d)}</span></div></div>' for i, (t, d) in enumerate(WHY))
        return f'<div class="slide"><h2>Why Vega<span class="acc">Syncor</span>: backups you can trust</h2><div class="grid">{items}</div></div>'
    if s == "how":
        return ('<div class="slide"><h2>How it works</h2><div class="flow">'
                '<div class="box"><h3>SOURCES</h3><p>Windows PCs and servers<em>SMB / CIFS shares</em></p>'
                '<p>Linux servers with Samba</p><p>Folders of the server<em>or OneDrive, to restore</em></p></div>'
                '<div class="arr"><div>→</div>read-only</div>'
                '<div class="box mid"><h3>VEGASYNCOR SERVICE</h3><p>Scheduler<em>intervals, times, days, cron</em></p>'
                '<p>rsync / rclone<em>only the differences</em></p><p>History and logs</p><p>Terminal UI<em>local or over SSH</em></p></div>'
                '<div class="arr"><div>→</div>mirror + archive</div>'
                '<div class="box"><h3>DESTINATIONS</h3><p>Disk of the server<em>also USB disks</em></p><p>NAS or network share</p>'
                '<p>OneDrive, off-site<em>with optional encryption</em></p></div>'
                '</div><div class="lead" style="margin-top:60px">A systemd service runs the copies on schedule; '
                'the TUI configures and monitors it.</div></div>')
    if s == "end":
        return ('<div class="slide" style="padding-top:150px"><h2>Get Vega<span class="acc">Syncor</span></h2>'
                '<div class="code"><span class="p">$</span> curl -fsSL https://github.com/sabbakix/VegaSyncor/releases/latest/download/vegasyncor-install.sh | sudo sh</div>'
                '<div class="code"><span class="p">$</span> sudo vegasyncor</div>'
                '<div class="cols"><div><b style="color:#fff">In short</b><br>1 · add the connections<br>'
                '2 · create the syncs<br>3 · dry run, then let the schedule work<br>4 · check the History</div>'
                '<div><b style="color:#fff">Remember</b><br>keep a copy of /etc/vegasyncor/master.key<br>'
                'keep the OneDrive encryption password<br>test a restore from time to time</div></div>'
                '<div class="foot">github.com/sabbakix/VegaSyncor · documentation, releases, source code</div></div>')
    raise ValueError(s)


def page(f, key):
    if f["kind"] == "term":
        return term_page(f, key)
    if f["kind"] == "image":
        return image_page(f)
    return slide_page(f)


# key badge: stays from the key press until the next key or 1.6 s later
def with_keys(frames):
    last, since = None, 0.0
    for f in frames:
        if f.get("key"):
            last, since = f["key"], 0.0
        elif f["kind"] != "term":
            last = None
        yield f, (last if last and since < 1.6 else None)
        since += f["dur"]


def main():
    cache, seq = {}, []
    with sync_playwright() as p:
        b = p.chromium.launch()
        pg = b.new_page(viewport={"width": 1920, "height": 1080})
        pg.set_content(f"<html><head><style>{CSS}</style></head><body></body></html>")
        for f, key in with_keys(tl["frames"]):
            body = page(f, key)
            h = hashlib.sha1(body.encode()).hexdigest()[:16]
            if h not in cache:
                png = WORK / f"{h}.png"
                if not png.exists():
                    pg.evaluate("h => document.body.innerHTML = h", body)
                    pg.screenshot(path=str(png))
                cache[h] = png
            if seq and seq[-1][0] == cache[h]:
                seq[-1][1] += f["dur"]
            else:
                seq.append([cache[h], f["dur"]])
        b.close()
    lst = WORK / "list.txt"
    with lst.open("w") as o:
        for png, d in seq:
            o.write(f"file '{png}'\nduration {d:.3f}\n")
        o.write(f"file '{seq[-1][0]}'\n")
    subprocess.run([imageio_ffmpeg.get_ffmpeg_exe(), "-y", "-loglevel", "error", "-f", "concat", "-safe", "0",
                    "-i", str(lst), "-vf", "fps=30,format=yuv420p", "-c:v", "libx264", "-preset", "slow",
                    "-tune", "stillimage", "-crf", "18", "-movflags", "+faststart", str(OUTFILE)], check=True)
    total = sum(d for _, d in seq)
    print(f"{OUTFILE}: {len(cache)} images, {int(total // 60)}:{int(total % 60):02d}")


if __name__ == "__main__":
    main()
