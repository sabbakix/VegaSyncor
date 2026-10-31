"""Voice-over of the presentation video: the spoken text of each segment of the
timeline. A segment is a run of frames with the same caption (or the same slide);
the voice starts with the segment, which is lengthened if the voice is longer.
Spelling is phonetic where the voice needs it ("Vega Sync-or")."""

LEAD, TAIL = 0.35, 0.6  # seconds of silence before and after each line


def seg_key(f):
    if f["kind"] == "slide":
        return f["slide"] + str(f.get("reveal", ""))
    return f["cap"]


NARRATION = {
    "title": "This is Vega Sync-or: security-focused backups for Linux servers. It makes scheduled, read-only, "
             "versioned copies of your network shares, to a local disk, a NAS, or OneDrive. And you manage "
             "everything from a terminal interface, even over SSH.",
    "why1": "Why Vega Sync-or? First, sources are read-only. Shares are mounted read-only by the kernel, so a backup "
            "can never change or delete your originals.",
    "why2": "Second, copies are versioned. Deleted and overwritten files go to a dated archive, and they're kept "
            "for as many days as you choose.",
    "why3": "It's fail-safe: if a source turns up empty or unreachable, the mirror is blocked, so your backup is "
            "never wiped.",
    "why4": "Passwords are encrypted, and they never appear in logs, or in the process list.",
    "why5": "Off-site copies to OneDrive can be encrypted on the server, before they're uploaded.",
    "why6": "And the backup server itself can be locked down, with the built-in firewall.",
    "how": "Here's how it works. A service on the backup server reads from Windows PCs, Samba servers, or local "
           "folders. It copies only the differences, and writes to a local disk, a NAS, or OneDrive. "
           "The terminal interface lets you set it up, and watch it work.",

    # installation
    "One command installs VegaSyncor on Debian or Ubuntu (amd64 or arm64).":
        "Installing takes a single command, on Debian or Ubuntu.",
    "The script downloads the package, verifies its checksum and installs the dependencies.":
        "The script downloads the package, verifies its checksum, and installs the dependencies.",
    "Then it starts the vegasyncor service, which runs the scheduled copies in the background.":
        "Then it starts the service, which runs your scheduled copies in the background.",
    "Keep a copy of master.key: it decrypts the saved passwords. Without it they must be entered again.":
        "One important step: keep a copy of the master key file somewhere safe. It decrypts your saved passwords.",
    "Open the management interface with “sudo vegasyncor”: it works on the server console and over SSH.":
        "To open the interface, run: sudo, Vega Sync-or. It works on the server console, and over SSH.",

    # dashboard
    "The Syncs tab lists every backup job: status, source → destination, schedule, last and next run.":
        "The Syncs tab lists every backup job, with its status, source and destination, its schedule, "
        "and the last and next run.",
    "Move with the arrows (or the mouse): the panel below shows the details of the selected job.":
        "Move with the arrow keys, or the mouse. The panel below shows the details of the selected job.",
    "The server clock and time zone are always visible: schedules use the server time.":
        "The server clock and time zone are always visible, because schedules follow the server time.",
    "Tabs: 1 Syncs · 2 Connections · 3 History · 4 Firewall. The interface is also in Italian (L).":
        "There are four tabs: Syncs, Connections, History, and Firewall. The interface is also available in Italian.",

    # connection
    "A connection holds the address and the credentials of a PC or server to back up.":
        "Let's back up a new PC. First, add a connection: it holds the address and the credentials of the computer.",
    "Give it a name, the host name or IP address, the domain (if any), the user and the password.":
        "Give it a name, then the host name or IP address, the domain if there is one, the user, and the password.",
    "Passwords are stored encrypted (AES-256-GCM) and never appear in logs, processes or on screen.":
        "Passwords are stored encrypted, and they never appear on screen, or in the logs.",
    "Ctrl+S saves.": "Control S saves it.",
    "t tests the access and lists the shares of the PC.":
        "Press T to test the connection. It checks the access, and lists the shared folders.",

    # new sync
    "n creates a new sync. The box at the top always summarises SOURCE ──> DESTINATION.":
        "Now create the sync, with N. The box at the top always sums up where the copy goes: "
        "from the source, to the destination.",
    "General: the name shown in lists and logs.": "Give it a name.",
    "Source: choose the connection, then Enter on Share picks it from the list.":
        "For the source, choose the connection. Then press Enter on Share, and pick it from the list.",
    "The source is mounted read-only by the kernel: a backup can never change or delete the originals.":
        "The source is mounted read-only by the kernel, so the backup can never change or delete "
        "the original files.",
    "Destination: a folder of this server (also a USB disk or NAS), a network share, or OneDrive.":
        "The destination can be a folder on this server, like a USB disk, a network share, or OneDrive. "
        "Here, it's a share on the backup NAS.",
    "Enter browses the folders; n creates a new folder, s uses the current one.":
        "Enter browses the folders. N creates a new folder, and S selects the current one.",
    "Mirror + archive (default): an exact copy, but deleted or overwritten files are kept for N days.":
        "The default mode is Mirror plus archive: an exact copy, but deleted or overwritten files are kept aside.",
    "Here they are kept for 60 days: protection from mistakes, and from files overwritten by ransomware.":
        "Here, we keep them for sixty days. That protects you from mistakes, and from files overwritten "
        "by ransomware.",
    "Optional: exclusions, a bandwidth limit, a folder for the deleted items, a copy of the logs.":
        "You can also add exclusions, a bandwidth limit, a folder for deleted items, and a copy of the logs.",
    "Schedule: at intervals, at fixed times, on some days, with cron, or manual only.":
        "Then the schedule: at intervals, at fixed times, on certain days, with cron, or manual only.",
    "Here: Monday to Friday at 12:30 and 19:00. Changed fields are marked with *.":
        "This one runs Monday to Friday, at twelve thirty, and at seven in the evening. "
        "Changed fields are marked with an asterisk.",
    "Ctrl+S saves the sync.": "Control S saves the sync.",

    # first run
    "s runs a dry run: it shows what would be copied or deleted, without touching anything.":
        "Before the first copy, press S for a dry run. It shows what would be copied or deleted, "
        "without touching anything.",
    "l opens the log of the run, file by file.": "L opens the log, file by file.",
    "r starts the copy now: progress, speed and time left are shown live.":
        "Now, press R to start the copy. Progress, speed, and time left are shown live.",
    "Later copies are incremental: only new and changed files are transferred.":
        "From now on, copies are incremental: only new and changed files are transferred.",

    # history
    "The History tab keeps every run, with its result and statistics.":
        "The History tab keeps every run, with its result.",
    "Enter opens the run log. Filters show only what changed:":
        "Press Enter to open a log. Filters let you see only what changed.",
    "n new · u updated · d deleted · m moved (a move is shown once, with the common path compacted).":
        "New files, updated, deleted, and moved. A moved file is shown once, with the common part of "
        "the path shortened.",
    "/ searches the rows as you type.": "And slash searches the rows, as you type.",

    # OneDrive
    "For an off-site copy, add a OneDrive connection: Personal or Microsoft 365 / Business.":
        "For an off-site copy, add a OneDrive connection. Both personal and Microsoft 365 accounts work.",
    "Enter starts the sign-in: open the link in any browser, on a PC or a phone, and sign in.":
        "Press Enter to sign in. Open the link in any browser, on your PC or your phone, and log in.",
    "Then paste back the address the browser lands on: no browser is needed on the server.":
        "Then paste back the address the browser lands on. The server doesn't need a browser at all.",
    "A sync can then copy to OneDrive, with optional client-side encryption:":
        "A sync can then copy to OneDrive, with optional encryption.",
    "files are encrypted on this server before the upload: Microsoft cannot read them.":
        "Files are encrypted on this server before the upload, so Microsoft can't read them.",
    "Keep the encryption password safe: without it the copy cannot be read back.":
        "Keep the encryption password safe: without it, the copy can't be read back.",
    "To restore or check an off-site copy, create a sync with OneDrive as the source.":
        "To restore, or to check an off-site copy, create a sync with OneDrive as the source.",

    # firewall and export
    "The Firewall tab locks the backup server down: incoming only SSH from the admin hosts,":
        "The Firewall tab locks down the backup server. Incoming, only SSH from the admin computers.",
    "outgoing only SMB to the backed-up hosts. A change not confirmed is rolled back automatically.":
        "Outgoing, only file sharing to the backed-up computers. And if a change isn't confirmed, "
        "it's rolled back automatically.",
    "E in Connections exports the whole configuration to an encrypted, password-protected file,":
        "Finally, E in the Connections tab exports the whole configuration, to an encrypted, "
        "password-protected file,",
    "to move VegaSyncor to a new server in minutes (I imports it).":
        "so you can move to a new server in minutes.",

    "end": "That's Vega Sync-or. Install it with one command, add your connections, create your syncs, "
           "try a dry run, and let the schedule do the work. And remember: keep your master key and your "
           "encryption passwords safe. Thanks for watching.",
}
