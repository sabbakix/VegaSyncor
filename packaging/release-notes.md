{{CHANGES}}

**VegaSyncor** is a Debian/Ubuntu service that syncs SMB/CIFS network folders (Windows PCs and servers, Linux servers with Samba) to a backup server on a schedule. It is managed from a TUI that also works over SSH, in English or Italian.

## Quick install (Debian / Ubuntu)

```bash
curl -fsSL https://github.com/sabbakix/VegaSyncor/releases/latest/download/vegasyncor-install.sh | sudo sh
```

The script:
- detects the architecture (amd64 / arm64);
- downloads the `.deb` package and verifies its checksum;
- installs the dependencies (`rsync`, `cifs-utils`, `smbclient`);
- starts the `vegasyncor` service (run it again to update).

### Manual install of the package

```bash
# amd64 (x86_64 PCs/servers). For Raspberry Pi 4/5 and ARM servers use _arm64.deb
wget https://github.com/sabbakix/VegaSyncor/releases/download/v{{VERSION}}/vegasyncor_{{VERSION}}_amd64.deb
sudo apt install ./vegasyncor_{{VERSION}}_amd64.deb smbclient
```

### Without the package (other distributions with systemd)

```bash
tar xzf vegasyncor_{{VERSION}}_linux_amd64.tar.gz
cd vegasyncor_{{VERSION}}_linux_amd64
sudo ./install.sh ./vegasyncor
```

On distributions without `apt`, install `rsync`, `cifs-utils` and `smbclient` manually.

## After installing

```bash
sudo vegasyncor           # opens the management interface
sudo vegasyncor status    # short status of the jobs
sudo vegasyncor check     # checks that the system can mount SMB shares
journalctl -u vegasyncor -f
```

1. Tab **2 Connections**, key `n`: enter address, user and password of the PC/server. `t` tests the access.
2. Tab **1 Syncs**, key `n`: choose source, destination, mode and schedule, then `Ctrl+S` to save.
3. Key `s` for a **dry run**, then `l` to see what would be copied or deleted.

The language (English / Italian) can be switched with the `EN|IT` selector next to the clock or the `L` key.

> ⚠️ **Back up `/etc/vegasyncor/master.key`**: without the key the saved passwords can no longer be read.

## Features

- **Read-only source enforced by the kernel**: it is mounted with `mount.cifs -o ro`, so no file on the source can be changed or deleted.
- **Encrypted passwords** (AES-256-GCM): they never appear in logs, processes or the TUI.
- **Modes per job**:
  - *Mirror + archive*: deleted or overwritten files are kept for N days in a dated folder;
  - *Mirror*: the destination becomes an exact copy;
  - *Add only*: never deletes anything.
- **Destination** local or on an SMB share.
- **Scheduling**:
  - at intervals, with optional time window and days;
  - daily, at one or more times;
  - on chosen days of the week;
  - with a cron expression, or manual only.
- **Dry run** with a detailed list of the changes.
- **Safety block**: a mirror with an empty source is stopped so the backup is not wiped.
- Incremental copy with `rsync`, bandwidth limit, exclusions, history and log of every run.

## Release files

| File | Description |
|---|---|
| `vegasyncor-install.sh` | automatic install/update |
| `vegasyncor_{{VERSION}}_amd64.deb` | package for Debian/Ubuntu x86_64 |
| `vegasyncor_{{VERSION}}_arm64.deb` | package for Debian/Ubuntu ARM64 |
| `vegasyncor_{{VERSION}}_linux_*.tar.gz` | static binary, systemd unit and `install.sh` |
| `SHA256SUMS` | checksums (`sha256sum -c SHA256SUMS`) |

**Requirements**: Debian 11+ / Ubuntu 20.04+ (or another distribution with systemd), run as root. In Proxmox/LXC containers the container must be *privileged* with the SMB/CIFS feature enabled (see the README).
