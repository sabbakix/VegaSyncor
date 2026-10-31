"""Records the presentation video: drives the real TUI (demo started by demo.sh)
in the tmux session "t" and writes the timeline of frames to $OUT/video/timeline.json
(rendered by render.py). Each frame is a terminal screen (ANSI), a slide or an
image, with its duration, the chapter, the caption and the key just pressed."""
import hashlib, json, os, re, pathlib, shutil, subprocess, sys, time

REPO = pathlib.Path(__file__).resolve().parents[4]
SHOTS = pathlib.Path(os.environ.get("SHOTS_DIR", "/tmp/vegasyncor-shots"))
DEMO = SHOTS / "demo"
OUT = SHOTS / "out" / "video"
W, H = 120, 40

env = dict(os.environ)
for part in (DEMO / "env.sh").read_text().replace("export", "").replace("\\\n", " ").split():
    k, _, v = part.partition("=")
    env[k] = v
SMB = pathlib.Path(env["VEGASYNCOR_DEV_SMB_ROOT"])

screens, frames = {}, []
state = {"chapter": "", "cap": "", "key": None}


def tmux(*a, out=False):
    r = subprocess.run(["tmux", *a], capture_output=True, text=True)
    return r.stdout if out else None


def screen():
    return tmux("capture-pane", "-t", "t", "-p", "-e", out=True)


def text():
    return tmux("capture-pane", "-t", "t", "-p", out=True)


def add(kind, dur, **kw):
    frames.append({"kind": kind, "dur": dur, "chapter": state["chapter"], "cap": state["cap"],
                   "key": state.pop("key", None), **kw})
    state["key"] = None


HOST = os.uname().nodename


def scrub(raw):
    """keeps this machine's name and home folder out of the video."""
    # the header shows the host name when there is room
    first, _, rest = raw.partition("\n")
    raw = first.replace(HOST, " " * len(HOST)) + "\n" + rest
    # default export file: the home folder and the host name in its name
    old, new = f"{pathlib.Path.home()}/vegasyncor-{HOST}-", "/root/vegasyncor-backup01-"
    lines = raw.split("\n")
    for i, l in enumerate(lines):
        if old in l:
            l = l.replace(old, new)
            # same width: the difference goes into the last run of spaces
            d = len(old) - len(new)
            j = l.rfind("   ")
            lines[i] = l[:j] + " " * d + l[j:] if d > 0 and j >= 0 else l
    return "\n".join(lines)


def snap(dur, raw=None):
    if raw is None:
        raw = scrub(screen())
    h = hashlib.sha1(raw.encode()).hexdigest()[:16]
    screens[h] = raw
    add("term", dur, screen=h)


def slide(name, dur, **kw):
    add("slide", dur, slide=name, **kw)


def image(src, dur):
    add("image", dur, src=str(src))


def chapter(name):
    state["chapter"] = name


def say(cap, hold=0.0):
    state["cap"] = cap
    if hold:
        snap(hold)


def key(k, label=None, hold=0.9, wait=0.35):
    tmux("send-keys", "-t", "t", k)
    time.sleep(wait)
    state["key"] = label or k
    snap(hold)


def keys(*ks, hold=0.5):
    for k in ks:
        key(k, hold=hold)


def typ(s, step=0.07):
    for ch in s:
        tmux("send-keys", "-t", "t", "-l", ch)
        time.sleep(0.03)
        snap(step)
    time.sleep(0.2)


def live(secs, every=0.5):
    end = time.time() + secs
    while time.time() < end:
        t0 = time.time()
        snap(every)
        time.sleep(max(0, every - (time.time() - t0)))


def until(pattern, tries=40, k="Down"):
    """presses k until the cursor line matches pattern (quietly between frames)."""
    import re
    for _ in range(tries):
        if any(re.match(r"^ >[ *]*" + pattern, l) for l in text().splitlines()):
            return
        tmux("send-keys", "-t", "t", k)
        time.sleep(0.25)
        snap(0.25)
    sys.exit("not found: " + pattern)


def select(name):
    """moves the list selection to the row of name."""
    for k in ["Up"] * 10 + ["Down"] * 10:
        if any("48;2;44;73;120m" + name + " " in l for l in screen().splitlines()):
            return
        tmux("send-keys", "-t", "t", k)
        time.sleep(0.25)
        if k == "Down":
            snap(0.3)
    sys.exit("row not found: " + name)


def start_tui():
    tmux("kill-session", "-t", "t")
    tmux("new-session", "-d", "-s", "t", "-x", str(W), "-y", str(H),
         f"bash -c '. {DEMO}/env.sh; {REPO}/bin/vegasyncor'")
    time.sleep(1.5)


def mk(p, size):
    p.parent.mkdir(parents=True, exist_ok=True)
    p.write_bytes(os.urandom(size))


def cli(*a):
    subprocess.run([str(REPO / "bin/vegasyncor"), *a], env=env, capture_output=True)


# ---- fake shell, for the installation -------------------------------------------------
class Shell:
    PROMPT = "\x1b[1;38;2;126;231;135madmin@backup01\x1b[0m:\x1b[1;38;2;108;182;255m~\x1b[0m$ "

    def __init__(self):
        self.lines = []

    def raw(self, cursor=True):
        ls = self.lines[-(H - 1):] if len(self.lines) >= H else list(self.lines)
        if cursor and ls:
            ls[-1] += "\x1b[48;2;220;227;238m \x1b[0m"
        return "\n".join(ls + [""] * (H - len(ls)))

    def out(self, line, dur=0.12):
        self.lines.append(line)
        snap(dur, self.raw(False))

    def cmd(self, c, step=0.045, hold=0.8):
        self.lines.append(self.PROMPT)
        snap(0.6, self.raw())
        # long commands wrap at the terminal width
        plain = len("admin@backup01:~$ ")
        for i, ch in enumerate(c):
            if (plain + i) % W == 0 and plain + i > 0:
                self.lines.append("")
            self.lines[-1] += ch
            snap(step, self.raw())
        snap(hold, self.raw())


def install():
    chapter("Installation")
    sh = Shell()
    say("One command installs VegaSyncor on Debian or Ubuntu (amd64 or arm64).")
    sh.cmd("curl -fsSL https://github.com/sabbakix/VegaSyncor/releases/latest/download/vegasyncor-install.sh | sudo sh",
           step=0.03, hold=2.5)
    say("The script downloads the package, verifies its checksum and installs the dependencies.")
    dim = lambda s: "\x1b[38;2;142;154;176m" + s + "\x1b[0m"
    for l in ["→ download VegaSyncor 0.7.1 (amd64)", "→ verifying checksum", "vegasyncor_0.7.1_amd64.deb: OK",
              "→ installing (with dependencies rsync, cifs-utils, smbclient, nftables, rclone)"]:
        sh.out(l, 0.5)
    for l in ["Reading package lists...", "Building dependency tree...", "The following NEW packages will be installed:",
              "  cifs-utils nftables rclone rsync smbclient vegasyncor", "Unpacking vegasyncor (0.7.1) ...",
              "Setting up rsync ...", "Setting up cifs-utils ...", "Setting up rclone ...",
              "Setting up vegasyncor (0.7.1) ...",
              "Created symlink /etc/systemd/system/multi-user.target.wants/vegasyncor.service → /lib/systemd/system/vegasyncor.service."]:
        sh.out(dim(l), 0.18)
    say("Then it starts the vegasyncor service, which runs the scheduled copies in the background.")
    sh.out("")
    sh.out("\x1b[38;2;126;231;135m●\x1b[0m vegasyncor.service - VegaSyncor - sync of network folders for backup", 0.2)
    sh.out("     Loaded: loaded (/lib/systemd/system/vegasyncor.service; enabled; preset: enabled)", 0.2)
    sh.out("     Active: \x1b[1;38;2;126;231;135mactive (running)\x1b[0m since Sat 2026-10-31 09:30:12 GMT; 1s ago", 0.2)
    for l in ["", "VegaSyncor 0.7.1 installed.", "", "  Management:   sudo vegasyncor", "  Quick status: sudo vegasyncor status",
              "  Service log:  journalctl -u vegasyncor -f", ""]:
        sh.out(l, 0.15)
    sh.out("\x1b[1;38;2;255;196;87mIMPORTANT: keep a copy of /etc/vegasyncor/master.key in a safe place.\x1b[0m", 3.5)
    say("Keep a copy of master.key: it decrypts the saved passwords. Without it they must be entered again.", 5)
    sh.lines.append(Shell.PROMPT)
    say("Open the management interface with “sudo vegasyncor”: it works on the server console and over SSH.")
    sh.lines.pop()
    sh.cmd("sudo vegasyncor", step=0.09, hold=2.5)


def dashboard():
    chapter("The dashboard")
    start_tui()
    say("The Syncs tab lists every backup job: status, source → destination, schedule, last and next run.", 5)
    say("Move with the arrows (or the mouse): the panel below shows the details of the selected job.")
    keys("Down", "Down", "Down", hold=1.0)
    key("Down", hold=3)
    say("The server clock and time zone are always visible: schedules use the server time.", 4.5)
    say("Tabs: 1 Syncs · 2 Connections · 3 History · 4 Firewall. The interface is also in Italian (L).", 4.5)


def connection():
    chapter("Step 1 · Add a connection")
    key("2", hold=1.5)
    say("A connection holds the address and the credentials of a PC or server to back up.", 3.5)
    key("n", hold=2.5)
    say("Give it a name, the host name or IP address, the domain (if any), the user and the password.")
    key("Down", hold=0.4)
    typ("Reception PC")
    key("Down", hold=0.4)
    typ("RECEPTION")
    keys("Down", "Down", hold=0.4)
    typ("COMPANY")
    key("Down", hold=0.4)
    typ("backup")
    key("Down", hold=0.4)
    typ("R3cept!on-2026")
    say("Passwords are stored encrypted (AES-256-GCM) and never appear in logs, processes or on screen.", 4.5)
    say("Ctrl+S saves.")
    key("C-s", "Ctrl+S", hold=2.5)
    say("t tests the access and lists the shares of the PC.")
    for _ in range(6):
        key("Down", hold=0.3)
    snap(1)
    key("t", hold=4)
    key("Enter", hold=1)


def new_sync():
    chapter("Step 2 · Create a sync")
    key("1", hold=1)
    say("n creates a new sync. The box at the top always summarises SOURCE ──> DESTINATION.")
    key("n", hold=4)
    say("General: the name shown in lists and logs.")
    typ("Reception")
    snap(1)
    say("Source: choose the connection, then Enter on Share picks it from the list.")
    keys("Down", "Down", "Down", hold=0.5)
    key("Left", "←", hold=1.5)
    key("Down", hold=0.6)
    key("Enter", hold=2.5)
    key("Enter", hold=2)
    say("The source is mounted read-only by the kernel: a backup can never change or delete the originals.")
    until("Read-only")
    snap(4.5)
    say("Destination: a folder of this server (also a USB disk or NAS), a network share, or OneDrive.")
    until("Type ")
    snap(1.5)
    key("Right", "→", hold=1.5)
    key("Right", "→", hold=2)
    key("Down", hold=0.5)
    for _ in range(6):
        if "Backup NAS" in [l for l in text().splitlines() if l.startswith(" >")][0]:
            break
        key("Right", "→", hold=0.5)
    snap(1)
    key("Down", hold=0.5)
    key("Enter", hold=1.5)
    key("Down", hold=0.5)
    key("Enter", hold=1)
    say("Enter browses the folders; n creates a new folder, s uses the current one.")
    until("Subfolder")
    key("Enter", hold=2.5)
    key("n", hold=1)
    typ("Reception")
    key("Enter", hold=2)
    key("s", hold=2)
    say("Mirror + archive (default): an exact copy, but deleted or overwritten files are kept for N days.")
    until("Mode")
    snap(5)
    say("Here they are kept for 60 days: protection from mistakes, and from files overwritten by ransomware.")
    until("Keep archive")
    keys("End", "BSpace", "BSpace", hold=0.3)
    typ("60")
    snap(3.5)
    say("Optional: exclusions, a bandwidth limit, a folder for the deleted items, a copy of the logs.")
    until("Exclude")
    typ("*.tmp, ~$*", step=0.09)
    key("Down", hold=0.5)
    typ("500")
    snap(2.5)
    say("Schedule: at intervals, at fixed times, on some days, with cron, or manual only.")
    until("Times")
    snap(1)
    tmux("send-keys", "-t", "t", "C-u")
    time.sleep(0.2)
    typ("12:30, 19:00", step=0.1)
    say("Here: Monday to Friday at 12:30 and 19:00. Changed fields are marked with *.", 4)
    say("Ctrl+S saves the sync.")
    key("C-s", "Ctrl+S", hold=3)


def first_run():
    chapter("Step 3 · Dry run and first copy")
    select("Reception")
    snap(1)
    say("s runs a dry run: it shows what would be copied or deleted, without touching anything.")
    key("s", hold=3.5)
    say("l opens the log of the run, file by file.")
    key("l", hold=4)
    key("Escape", "Esc", hold=1)
    say("r starts the copy now: progress, speed and time left are shown live.")
    key("r", hold=0.5, wait=0.5)
    live(4)
    for _ in range(30):
        if not any("Reception" in l and "running" in l for l in text().splitlines()):
            break
        live(1)
    snap(1.5)
    say("Later copies are incremental: only new and changed files are transferred.", 4)


def change_source():
    s = SMB / "RECEPTION/Shared"
    for i in (1, 2, 3):
        mk(s / f"Orders/Order_2026-10-3{i}.pdf", 90000)
    mk(s / "Customers/Document_2.pdf", 130000)
    mk(s / "Price lists/Document_5.pdf", 125000)
    (s / "Customers/Document_7.pdf").unlink()
    (s / "Orders/Archive 2025").mkdir(exist_ok=True)
    for i in (8, 9):
        (s / f"Orders/Document_{i}.pdf").rename(s / f"Orders/Archive 2025/Document_{i}.pdf")
    cli("run", "Reception")


def history():
    chapter("History and logs")
    change_source()
    key("3", hold=1.5)
    say("The History tab keeps every run, with its result and statistics.", 4)
    say("Enter opens the run log. Filters show only what changed:")
    key("Enter", hold=3)
    key("c", hold=3)
    say("n new · u updated · d deleted · m moved (a move is shown once, with the common path compacted).")
    key("n", hold=2)
    key("u", hold=2)
    key("d", hold=2)
    key("m", hold=3.5)
    say("/ searches the rows as you type.")
    key("a", hold=1)
    key("/", hold=0.8)
    typ("Orders", step=0.15)
    snap(3.5)
    key("Escape", "Esc", hold=0.5)
    key("Escape", "Esc", hold=1)


def onedrive():
    chapter("Off-site copy to OneDrive")
    key("2", hold=1)
    say("For an off-site copy, add a OneDrive connection: Personal or Microsoft 365 / Business.")
    key("n", hold=1)
    key("Right", "→", hold=2.5)
    key("Down", hold=0.5)
    key("Down", hold=1)
    say("Enter starts the sign-in: open the link in any browser, on a PC or a phone, and sign in.")
    key("Enter", hold=5)
    say("Then paste back the address the browser lands on: no browser is needed on the server.")
    typ("http://localhost:53682/?code=M.C540_BAY.2.U.d1f5a0c2-7e1b-4a8c-9e3f-0b6d5c4a3e21&state=pX2kq9", step=0.012)
    snap(2)
    key("Enter", hold=4)
    key("Escape", "Esc", hold=0.5)
    start_tui()
    snap(0.5)
    say("A sync can then copy to OneDrive, with optional client-side encryption:")
    select("Accounting off-site")
    snap(1)
    key("Enter", hold=2)
    until("Encrypted copy")
    say("files are encrypted on this server before the upload: Microsoft cannot read them.", 4.5)
    say("Keep the encryption password safe: without it the copy cannot be read back.", 4.5)
    say("To restore or check an off-site copy, create a sync with OneDrive as the source.", 4.5)
    key("Escape", "Esc", hold=1)


def firewall_export():
    chapter("Firewall and configuration export")
    say("The Firewall tab locks the backup server down: incoming only SSH from the admin hosts,")
    image(REPO / "docs/screenshots/07-firewall.png", 4.5)
    say("outgoing only SMB to the backed-up hosts. A change not confirmed is rolled back automatically.")
    image(REPO / "docs/screenshots/07-firewall.png", 5)
    start_tui()
    say("E in Connections exports the whole configuration to an encrypted, password-protected file,")
    key("2", hold=0.5)
    key("E", hold=4)
    say("to move VegaSyncor to a new server in minutes (I imports it).", 4)
    key("Escape", "Esc", hold=1)


def main():
    subprocess.run([str(REPO / "docs/screenshots/tools/demo.sh")], check=True)
    s = SMB / "RECEPTION/Shared"
    for d in ("Orders", "Customers", "Price lists"):
        for i in range(1, 13):
            mk(s / d / f"Document_{i}.pdf", 120000)
    (SMB / "RECEPTION/Users").mkdir(exist_ok=True)
    (s / "Templates").mkdir(exist_ok=True)

    chapter("")
    slide("title", 7)
    slide("why", 0, reveal=0)
    for i in range(1, 7):
        slide("why", 4.2 if i < 6 else 6, reveal=i)
    slide("how", 12)
    install()
    dashboard()
    connection()
    new_sync()
    first_run()
    history()
    onedrive()
    firewall_export()
    chapter("")
    slide("end", 10)
    tmux("kill-session", "-t", "t")

    frames[:] = [f for f in frames if f["dur"] > 0]
    OUT.mkdir(parents=True, exist_ok=True)
    (OUT / "timeline.json").write_text(json.dumps({"cols": W, "rows": H, "screens": screens, "frames": frames}))
    total = sum(f["dur"] for f in frames)
    print(f"{len(frames)} frames, {len(screens)} screens, {int(total // 60)}:{int(total % 60):02d}")


if __name__ == "__main__":
    main()
