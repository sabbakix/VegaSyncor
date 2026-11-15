# End-to-end tests

They install the real `.deb` package in an LXD container and test it as root
against a real SMB server: read-only mounts, mirror + archive, incremental runs,
modification times, empty-source protection, error messages, service restart and
that passwords never end up on disk in plain text.

## Requirements

- LXD (`sudo snap install lxd && sudo lxd init --auto`) and membership of the
  `lxd` group. A container is used, not a VM, so no KVM is needed; it is
  **privileged** (CIFS mounts need it), so root in it is root on the host:
  use it for these tests only.
- An SMB server with two shares the test user can read and write. The tests
  create and remove only `vs-test*` folders: the files in the source share are
  removed at the end, the copies in the destination share are kept for
  inspection and replaced on the next run.
- Go and `make` on the host (the package is built from the working tree).

## Configuration

```bash
cp .env.example .env
chmod 600 .env    # .env is git-ignored: never commit it
```

and set `SMB_HOST`, `SMB_USER`, `SMB_PASS` and the share names.

## Running

```bash
test/e2e/run.sh
```

The first run creates the `vs-test` container (Ubuntu 26.04). Every run
rebuilds the package, reinstalls it with an empty configuration and runs
`e2e.sh` inside the container; the exit code is non-zero if a check fails.

The credentials are copied into the container as a `0600` file only for the
duration of the run, and passed to `smbclient` through a credentials file, never
on the command line.
