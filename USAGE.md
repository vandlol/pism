# Using pism

The complete reference for day-to-day pism. New here? Start with the
[README](README.md); install steps live in [INSTALL.md](INSTALL.md).

- [Quick start](#quick-start)
- [Remote hosts over SSH](#remote-hosts-over-ssh)
- [Commands](#commands)
- [Flags](#flags)
- [Detaching](#detaching)
- [Switching sessions](#switching-sessions)
- [Configuration](#configuration)
- [Self-update](#self-update)
- [How it works](#how-it-works)
- [File locations](#file-locations)
- [Troubleshooting](#troubleshooting)
- [Notes & limits](#notes--limits)

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

By default `new` waits up to 30s for pi to come up before attaching; tune it per
run with `-w/--wait` (`pism new ~/proj -w 5m`, `-w 0` waits forever) or
permanently via `pism config ready-timeout`.

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
pism srv update --pre       # update pism on host 'srv' over ssh
```

`pism <host> update` runs the remote's own `pism update`, so it respects that
host's configured channel unless you override it with `--pre` / `--stable` /
`--update-url`.

**Fan out across every host at once** — both `pism update` and `pism config`
take an `--all` flag that enumerates the hosts in your ssh config, probes each
for a pism binary, and runs the command on the ones that have it (unreachable
hosts or hosts without pism are skipped, not failed):

```sh
pism update --all                       # update every ssh-config host with pism
pism update --all --pre                 # ...on the unstable channel
pism update --all --include 'prod-*'    # only hosts matching prod-*
pism update --all --exclude ci-1,ci-2   # all except these
pism config update-exclude 'ci-*,lab'   # persist a skip list

pism config --all update-channel unstable   # set a key on every host
pism config --all update-channel             # read the key from every host
pism config --all --include 'prod-*' pi /opt/homebrew/bin/pi
```

- Works for `update` and `config`; the same host-selection flags apply to both.
- `--include` / `--exclude` take comma/space-separated **glob** patterns
  (`*`, `?`) and may be repeated; excludes always win over includes.
- Everything after the selection flags is forwarded verbatim to each remote —
  the `update` channel flags (`--pre`, `--stable`, `--update-url`), or the
  `config` key/value.
- `--connect-timeout <secs>` bounds the reachability probe (default 10).

pism shells out to your system `ssh`, using your existing config, keys and agent —
it never handles credentials.

**SSH config resolution** (highest priority first):

1. `--ssh-config <path>`
2. `$PISM_SSH_CONFIG`
3. a project-local `./ssh_config`, `./.ssh_config`, `./.ssh/config`, or
   `./.pism/ssh_config` in the current directory (auto-detected)
4. otherwise ssh's own defaults (`~/.ssh/config`)

**See every host's sessions at once** — `pism ls --all` aggregates the local
machine plus every ssh-config host that has pism into a single host-tagged table:

```sh
pism ls --all                        # local + all ssh-config hosts
pism ls --all --include 'prod-*'     # only hosts matching prod-*
pism ls --all --exclude ci-1,ci-2    # all except these
pism ls --all --connect-timeout 5    # bound the per-host reachability probe
```

```
HOST   ID        S     TOPIC                      DIR          AGE
local  3f9a1c2b  live  design a caching layer     ~/proj/api   2h
local  7c1d0e44  live  fix the flaky auth test    ~/proj/web   15m
mac    a0b2f931  live  port the UI to resource…   ~/burger     1d
srv    e4d5c6b7  dead  migrate the billing schema ~/billing    3d
```

Unreachable hosts and hosts without pism are noted on stderr and skipped (never
failed). Under the hood each host runs `pism ls --porcelain` (a stable
tab-separated `id⇥state⇥age⇥dir⇥topic` format you can also script against
directly). Selection uses the same `--include`/`--exclude` globs as
`update --all`.

> This is a read-only view today — *attaching* to and *switching between* sessions
> across hosts are the next steps on the roadmap.

Installing pism on a remote host (`pism srv install`) and pushing a local binary
(`pism push srv`) are covered in [INSTALL.md](INSTALL.md#install-on-a-remote-host).

---

## Commands

```
pism new [dir] [-d] [-w dur] [-- pi args...]  Start a new session (attaches unless -d;
                                      -w/--wait sets the ready timeout, 0=forever)
pism ls [--all] [--porcelain]         List sessions with topic + liveness
                                      (--all aggregates every ssh host; see below)
pism attach <id>                      Re-attach (id prefixes work: pism a 3f9a)
pism kill <id> [id...]                Terminate session(s)
pism gc                               Drop metadata for dead sessions
pism topic <id>                       Print a session's topic (for scripts)
pism logs <id>                        Print a session's holder log (diagnostics)
pism config <key> [value]             Get/set config (--list, --unset, --path)
pism config --all <key> [value]       Get/set a key on every ssh-config host
pism update [--pre|--stable]          Update pism in place from the update server
pism update --all                     Update every ssh-config host that has pism
pism <host> install                   Install pism on a remote host over ssh
pism push <host> [dest]               Copy the matching binary to a host
pism build-all                        Cross-compile binaries into ./dist
pism version
```

Any of `ls new attach kill gc topic update logs` can be prefixed with a host to
run remotely (e.g. `pism srv ls`, `pism srv update --pre`).

## Flags

```
--remote-bin <path>    pism path on the remote host (default: pism)
--pi <cmd>             command used to launch pi (default: pi)
--detach-key <spec>    detach key: ^\ , ctrl-o, a char, a code, or "none"
--switch-prev-key <spec>  switch to previous session (default: ctrl-left)
--switch-next-key <spec>  switch to next session (default: ctrl-right)
--topic-len <n>        max topic width in `ls` (default: 40)
--ssh-config <path>    ssh config file to use (-F)
--update-url <url>     custom base URL for `pism update` (overrides channel)
--pre / --stable       one-off update channel for `pism update`
--connect-timeout <n>  reachability-probe timeout for `--all` fan-out (default 10s)
--dist <dir>           output/source dir for build-all & push (default: dist)
-v / -vv / -vvv        verbosity: info / debug / trace (see `pism logs <id>`)
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

## Switching sessions

While attached, jump straight to another live session without dropping to the
shell:

- **Ctrl-Left** → attach to the previous live session
- **Ctrl-Right** → attach to the next live session

Ordering is newest-first with wraparound, and dead sessions are skipped. The
keys are configurable (per-attach or permanently), and accept named keys like
`ctrl-left`/`alt-right`, function keys (`f16`), control chars (`ctrl-o`), raw
escape sequences (`\x1b[1;5D`), or `none` to disable:

```sh
pism attach 3f9a --switch-prev-key alt-left --switch-next-key alt-right
pism config switch-prev-key ctrl-left     # make it permanent
pism config switch-next-key none          # disable next-switch
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

**Keys:** `pi`, `detach-key`, `switch-prev-key`, `switch-next-key`, `topic-len`, `remote-bin`, `ssh-config`, `update-url`, `update-channel`, `update-exclude`, `ready-timeout`.

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
pism srv update --pre                 # update pism on a remote host over ssh
pism update --all                     # update every ssh-config host with pism
pism config --all update-channel unstable  # set a key on every host
```

Pre-releases come from merges into `dev`; stable releases from merges into `main`
(see [CONTRIBUTING.md](CONTRIBUTING.md)). Override the source entirely with
`--update-url` / `$PISM_UPDATE_URL` / `config update-url`, or point at a different
repo with `$PISM_UPDATE_REPO`.

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

**Diagnosing a session.** Each holder writes a log you can inspect with
`pism logs <id>` (add `-v`/`-vv`/`-vvv` when creating/attaching for more detail).

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
