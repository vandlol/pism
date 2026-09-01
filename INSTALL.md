# Installing pism

pism ships as a single static binary — no runtime, no dependencies. Pick the
path that suits you.

- [One-liner install](#one-liner-install) (recommended)
- [Manual download](#manual-download)
- [Install on a remote host](#install-on-a-remote-host)
- [Build from source](#build-from-source)

> **Requirements:** `pi` on the machine that runs the session, and — for remote
> use — the system `ssh` client. Both are already present on typical setups.

---

## One-liner install

**Linux / macOS:**

```sh
curl -fsSL https://raw.githubusercontent.com/vandlol/pism/main/scripts/install.sh | sh
```

**Windows (PowerShell):**

```powershell
irm https://raw.githubusercontent.com/vandlol/pism/main/scripts/install.ps1 | iex
```

The installer detects your OS/arch, downloads the matching binary, drops it in
`~/.local/bin` (Unix) or `%LOCALAPPDATA%\pism\bin` (Windows), and puts it on PATH.

Override with env vars:

| Var | Default | Meaning |
|-----|---------|---------|
| `PISM_BASE_URL` | the latest GitHub release | download server |
| `PISM_INSTALL_DIR` | `~/.local/bin` / `%LOCALAPPDATA%\pism\bin` | install dir |

```sh
PISM_INSTALL_DIR=/usr/local/bin curl -fsSL https://raw.githubusercontent.com/vandlol/pism/main/scripts/install.sh | sh
```

Already installed? Keep it current with [`pism update`](USAGE.md#self-update).

---

## Manual download

Grab the binary for your platform, make it executable, and put it on PATH.

| Platform | File |
|----------|------|
| Linux x86-64 | https://github.com/vandlol/pism/releases/latest/download/pism-linux-amd64 |
| Linux ARM64 | https://github.com/vandlol/pism/releases/latest/download/pism-linux-arm64 |
| macOS Apple Silicon | https://github.com/vandlol/pism/releases/latest/download/pism-darwin-arm64 |
| macOS Intel | https://github.com/vandlol/pism/releases/latest/download/pism-darwin-amd64 |
| Windows x86-64 | https://github.com/vandlol/pism/releases/latest/download/pism-windows-amd64.exe |
| Windows ARM64 | https://github.com/vandlol/pism/releases/latest/download/pism-windows-arm64.exe |

```sh
# example: Linux x86-64
curl -fsSL https://github.com/vandlol/pism/releases/latest/download/pism-linux-amd64 -o ~/.local/bin/pism
chmod +x ~/.local/bin/pism
```

---

## Install on a remote host

Install pism on another machine over ssh — pism detects the remote OS and runs
the published installer (works from any client OS, to any host OS):

```sh
pism srv install         # or: pism install srv
```

- POSIX hosts (Linux/macOS): runs `install.sh` via `curl|sh` (wget fallback).
- Windows hosts: runs `install.ps1` via PowerShell.
- Pin a version with `--version <tag>`.

Alternatively, **push a local binary** to a POSIX host (no download needed):

```sh
pism build-all       # or: make dist  — cross-compile every target into ./dist
pism push srv        # scp the matching binary to srv:~/.local/bin/pism
```

> After install, if the remote's non-interactive shell can't find `pism` (e.g.
> `~/.local/bin` isn't on its `PATH`), add it there or use `--remote-bin`. See
> [Troubleshooting](USAGE.md#troubleshooting).

---

## Build from source

Requires Go 1.26+.

```sh
git clone https://github.com/vandlol/pism.git && cd pism
make build            # -> ./pism
make install          # -> ~/.local/bin/pism
make dist             # cross-compile all targets into ./dist
make test             # run tests
```

Targets built by `build-all` / `make dist`: `linux/amd64`, `linux/arm64`,
`darwin/amd64`, `darwin/arm64`, `windows/amd64`, `windows/arm64` — static
(`CGO_ENABLED=0`), ~3.5 MB each.

### Releasing

Releases are automated from the branch flow — you don't tag by hand. Merging a
PR into `dev` publishes a **patch pre-release**; merging `dev` into `main`
publishes a **minor** (or **major**, via a `release:major` label) **stable**
release. Tags are plain SemVer (no `v` prefix) and baked into the binary
(`pism version`). See [CONTRIBUTING.md](CONTRIBUTING.md) for the full model.
