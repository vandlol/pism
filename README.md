# pism — pi session manager

A cross-platform (**Linux · macOS · Windows**), **tmux-free** session manager for
[pi](https://pi.dev). It keeps pi running in detached PTY holders so you can
disconnect and re-attach reliably — **locally or over SSH** — and it lists every
session by its **topic** (the conversation's first message), so you always know
what's running where.

No multiplexer, no status bar, no prefix keys. One static binary per host.

![pism demo](assets/demo.gif)

```
$ pism ls
ID        S     TOPIC                                DIR                 AGE
3f9a1c2b  live  design a caching layer for the API   ~/proj/api          2h
7c1d0e44  live  fix the flaky auth test              ~/proj/web          15m
a0b2f931  dead  migrate the billing schema           ~/proj/billing      1d
```

---

## Contents

- [Install](#install)
- [Quick start](#quick-start)
- [Remote hosts over SSH](#remote-hosts-over-ssh)
- [Commands](#commands)
- [Detaching](#detaching)
- [Configuration](#configuration)
- [Self-update](#self-update)
- [Build from source](#build-from-source)
- [How it works](#how-it-works)
- [File locations](#file-locations)
- [Troubleshooting](#troubleshooting)

---

## Install

### One-liner (recommended)

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

### Manual download

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

> Requires `pi` on the machine that runs the session, and (for remote use) the
> system `ssh` client — both are already present on typical setups.

---

## Quick start

```sh
pism new ~/proj/api      # start pi in ~/proj/api and attach
# ...work... press Ctrl-\ to detach; pi keeps running
pism ls                  # see it (and everything else) by topic
pism attach 3f9a         # jump back in (id prefixes are fine)
pism kill 3f9a           # done
```

`pism new` passes `--session-id <uuid>` to pi, so each session is linked to its
transcript unambiguously — topics stay correct even across symlinks or when two
sessions share a directory.

---

## Remote hosts over SSH

The **host is the first argument**. If the first word is a known command it runs
locally; otherwise it's treated as an SSH target (any `user@host` or a
`~/.ssh/config` alias):

```sh
pism ls                     # local
pism srv ls                 # list sessions on host 'srv'
pism srv new ~/svc          # start a session on 'srv'
pism srv attach 3f9a        # attach to a remote session (live PTY over ssh -t)
pism srv kill 3f9a          # kill a remote session
```

pism shells out to your system `ssh`, using your existing config, keys and agent —
it never handles credentials.

**SSH config resolution** (highest priority first):

1. `--ssh-config <path>`
2. `$PISM_SSH_CONFIG`
3. a project-local `./ssh_config`, `./.ssh_config`, `./.ssh/config`, or
   `./.pism/ssh_config` in the current directory (auto-detected)
4. otherwise ssh's own defaults (`~/.ssh/config`)

**Install pism on a remote host** over ssh — detects the remote OS and runs the
published installer (works from any client OS, to any host OS):

```sh
pism srv install         # or: pism install srv
```

- POSIX hosts (Linux/macOS): runs `install.sh` via `curl|sh` (wget fallback).
- Windows hosts: runs `install.ps1` via PowerShell.
- Pin a version with `--version <tag>`.

Alternatively, **push a local binary** to a POSIX host (no download needed):

```sh
pism build-all       # or: make dist
pism push srv        # scp the matching binary to srv:~/.local/bin/pism
```

> After install, if the remote's non-interactive shell can't find `pism` (e.g.
> `~/.local/bin` isn't on its `PATH`), add it there or use `--remote-bin`.

---

## Commands

```
pism new [dir] [-d] [-- pi args...]   Start a new pi session (attaches unless -d)
pism ls                               List sessions with topic + liveness
pism attach <id>                      Re-attach (id prefixes work: pism a 3f9a)
pism kill <id> [id...]                Terminate session(s)
pism gc                               Drop metadata for dead sessions
pism topic <id>                       Print a session's topic (for scripts)
pism config <key> [value]             Get/set config (--list, --unset, --path)
pism update                           Update pism in place from the update server
pism <host> install                   Install pism on a remote host over ssh
pism push <host> [dest]               Copy the matching binary to a host
pism build-all                        Cross-compile binaries into ./dist
pism version
```

Any of `ls new attach kill gc topic` can be prefixed with a host to run remotely.

### Flags

```
--remote-bin <path>    pism path on the remote host (default: pism)
--pi <cmd>             command used to launch pi (default: pi)
--detach-key <spec>    detach key: ^\ , ctrl-o, a char, a code, or "none"
--topic-len <n>        max topic width in `ls` (default: 40)
--ssh-config <path>    ssh config file to use (-F)
--update-url <url>     custom base URL for `pism update` (overrides channel)
--pre / --stable       one-off update channel for `pism update`
--dist <dir>           output/source dir for build-all & push (default: dist)
```

Flags override [config](#configuration), which overrides built-in defaults.

---

## Detaching

Press **Ctrl-\\** to detach — pi keeps running; reconnect with `pism attach <id>`.

Detach ≠ kill. Use `pism kill <id>` to actually stop a session.

Change the detach key per-attach or permanently:

```sh
pism attach 3f9a --detach-key ctrl-o    # Ctrl-o for this attach
pism config detach-key ctrl-o           # make it permanent
pism attach 3f9a --detach-key none      # disable (only pi exit ends it)
```

---

## Configuration

A config file is created automatically on first run, at the OS-correct location:

| OS | Path |
|----|------|
| Linux | `~/.config/pism/config` |
| macOS | `~/Library/Application Support/pism/config` |
| Windows | `%AppData%\pism\config` |

Override the whole path with `$PISM_CONFIG`. Manage it git-style:

```sh
pism config --list                 # show set values
pism config detach-key             # get one
pism config detach-key ctrl-o      # set
pism config topic-len 60           # set (validated)
pism config --unset pi             # remove
pism config --path                 # print the file path
```

**Keys:** `pi`, `detach-key`, `topic-len`, `remote-bin`, `ssh-config`, `update-url`, `update-channel`, `ready-timeout`.

**Precedence:** command-line flag **>** config file **>** built-in default.
Config is per-machine and is *not* pushed to remote hosts (each host reads its own).

The file is plain, hand-editable `key = value` with `#` comments.

---

## Self-update

```sh
pism update            # update on your configured channel
```

Downloads the matching build from GitHub releases, validates it runs, and swaps it
in atomically (rename on Unix; move-aside + rename on Windows).

### Channels

| Channel | Gets | How |
|---------|------|-----|
| **stable** (default, aka `latest`) | newest **non-pre-release** | `…/releases/latest/download` |
| **unstable** (aka `dev` / `nightly`) | newest release **incl. pre-releases** | GitHub API |

```sh
pism config update-channel unstable   # or: stable / dev / nightly / latest
pism update                           # respects the channel
pism update --pre                     # one-off: grab the latest dev build
pism update --stable                  # one-off: force stable
```

Pre-releases come from merges into `dev`; stable releases from merges into `main`
(see [CONTRIBUTING.md](CONTRIBUTING.md)). Override the source entirely with
`--update-url` / `$PISM_UPDATE_URL` / `config update-url`, or point at a different
repo with `$PISM_UPDATE_REPO`.

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

Targets built by `build-all`/`make dist`: `linux/amd64`, `linux/arm64`,
`darwin/amd64`, `darwin/arm64`, `windows/amd64`, `windows/arm64` — static
(`CGO_ENABLED=0`), ~3.5 MB each.

**Publish a new release** — push a tag and GitHub Actions builds every platform
and attaches the binaries (plus `SHA256SUMS.txt`) to a GitHub Release. Clients
then get it via `pism update` or the install script.

Releases are automated from the branch flow — you don't tag by hand. Merging a
PR into `dev` publishes a **patch pre-release**; merging `dev` into `main`
publishes a **minor** (or **major**, via a `release:major` label) **stable**
release. Tags are plain SemVer (no `v` prefix) and baked into the binary
(`pism version`). See [CONTRIBUTING.md](CONTRIBUTING.md) for the full model.

---

## How it works

```
pism new ─┐
          ├─ spawns a detached "holder" process (setsid / DETACHED_PROCESS)
          │     ├─ opens a PTY, runs `pi --session-id <uuid>` in it
          │     ├─ listens on a unix socket (Unix) / named pipe (Windows),
          │     │  keyed by the session id, guarded by a per-session token
          │     └─ keeps a replay ring buffer of recent output
          │
pism attach ─ dials the holder, streams your terminal <-> the PTY, forwards
              SIGWINCH/console resizes, intercepts the detach key
              (holder survives → pi keeps running)

pism ls ─ reads session metadata + probes liveness + reads each pi transcript
          (by uuid) to show the topic
```

- **PTY**: [go-pty](https://github.com/aymanbagabas/go-pty) (creack/pty on Unix,
  ConPTY on Windows).
- **Control channel**: unix domain socket (Unix) / named pipe (Windows), with a
  per-session token handshake.
- **Remote**: shells out to the system `ssh` (and `scp` for `push`).

---

## File locations

| What | Unix | Windows |
|------|------|---------|
| Config | `~/.config/pism/config` (macOS: `~/Library/Application Support/pism/config`) | `%AppData%\pism\config` |
| Session state (metadata, logs) | `~/.local/state/pism` | `%LOCALAPPDATA%\pism` |
| Control sockets | `$XDG_RUNTIME_DIR/pism` or `/tmp/pism-<uid>` | `\\.\pipe\pism-<id>` |
| pi transcripts (read for topics) | `~/.pi/agent/sessions` | `%USERPROFILE%\.pi\agent\sessions` |

Overrides: `$PISM_STATE_DIR`, `$PISM_CONFIG`, `$PISM_PI_SESSIONS_DIR`.

---

## Troubleshooting

**`holder: exec: "pi": executable file not found` over SSH.**
`ssh host pism ...` runs a *non-interactive, non-login* shell, which on zsh sources
only `~/.zshenv` (not `.zprofile`/`.zshrc`). Make sure both `pism` and `pi` are on
PATH there — e.g. add to `~/.zshenv`:

```sh
for d in "$HOME/.local/bin" /opt/homebrew/bin; do
  case ":$PATH:" in *":$d:"*) ;; *) [ -d "$d" ] && export PATH="$d:$PATH";; esac
done
```

Or point pism at an absolute pi: `pism config pi /opt/homebrew/bin/pi`.

**Can't connect to a remote Mac.** Enable **Remote Login** (System Settings →
General → Sharing → Remote Login) and allow `sshd` through the firewall.

**`bind: invalid argument` creating a session (macOS).** Unix socket paths are
capped at ~104 bytes; pism keeps sockets in a short dir (`/tmp/pism-<uid>`) to
avoid this. If you set `$PISM_STATE_DIR` to a very deep path it won't matter —
sockets don't live there.

**Attach shows a blank screen.** pism replays the recent buffer and resizes the PTY
to trigger a repaint; if a full-screen app doesn't redraw, resize your terminal or
press a key.

---

## Notes & limits

- The replay buffer is the recent screen (256 KiB), not full scrollback — enough
  for a TUI like pi to repaint on attach.
- Multiple clients can attach to one session simultaneously (shared view).
- Killing a session terminates pi; there is no auto-restart (by design).
