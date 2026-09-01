# pism

**Never lose a `pi` session again.**

pism keeps your [pi](https://pi.dev) sessions alive in the background so you can
walk away, close the terminal, drop off SSH — and pick up exactly where you left
off. No tmux, no multiplexer, no prefix-key gymnastics. Just one tiny binary that
runs on **Linux, macOS, and Windows**.

![pism demo](assets/demo.gif)

The best part: every session gets a **memorable name** and is listed by its
**topic** — the first thing you asked pi — so you always know what's running
where, and switching between them is about names, not cryptic ids.

```
$ pism ls
NAME           ID        S     TOPIC                                DIR             AGE
calm-otter     3f9a1c2b  live  design a caching layer for the API   ~/proj/api      2h
brave-falcon   7c1d0e44  live  fix the flaky auth test              ~/proj/web      15m
zippy-rabbit   a0b2f931  dead  migrate the billing schema           ~/proj/billing  1d
```

Use the name anywhere you'd use an id — `pism attach calm-otter` — and rename it
to something meaningful with `pism name calm-otter api`.

---

## Why pism?

- 🧠 **You run more than one pi at a time.** Juggle a dozen sessions across a
  dozen projects and tell them apart at a glance — by topic, not cryptic ids.
- 🔌 **Your connection drops.** Detach with `Ctrl-\`, close the laptop, and pi
  keeps thinking. Re-attach whenever, from wherever.
- 🌍 **Your work lives on servers.** Start, list, and re-attach to sessions on
  any SSH host with the same commands — `pism srv attach 3f9a`.
- 🪶 **You hate heavyweight tooling.** One ~3.5 MB static binary. No daemon, no
  config required, no runtime to install.

If you've ever `tmux`'d just to keep an agent running and then forgotten which
pane was which — pism is the fix.

---

## Get started in 60 seconds

Install (Linux / macOS):

```sh
curl -fsSL https://raw.githubusercontent.com/vandlol/pism/main/scripts/install.sh | sh
```

Windows (PowerShell):

```powershell
irm https://raw.githubusercontent.com/vandlol/pism/main/scripts/install.ps1 | iex
```

Then start working:

```sh
pism new ~/proj/api      # start pi here and attach
#   …do your thing, then press Ctrl-\ to detach — pi keeps running
pism ls                  # see every session by topic
pism attach 3f9a         # jump back in (id prefixes are fine)
pism kill 3f9a           # stop it when you're done
```

That's the whole loop. Everything else is a bonus.

Other install options (manual download, remote hosts, build from source) live in
**[INSTALL.md](INSTALL.md)**.

---

## What else it does

- **Memorable names** — every session gets an `adjective-noun` label
  (`calm-otter`) you can attach/kill/rename by, so cross-host switching never
  leaves you guessing which session is which.
- **Sessions by topic** — `pism ls` reads each pi transcript and shows what the
  conversation is actually about, plus whether it's live or dead.
- **Detach & re-attach** — `Ctrl-\` drops out without stopping pi; re-attach any
  time. The detach key is configurable (or disable it entirely).
- **Hop between sessions** — while attached, `Ctrl-←` / `Ctrl-→` jump straight to
  the previous/next live session without dropping to a shell. Add `--all`
  (`pism attach --all`) and the hop spans **every host** — Mac ↔ Linux ↔ local.
- **Remote over SSH** — put a host in front of any command: `pism srv ls`,
  `pism srv new ~/svc`, `pism srv attach 3f9a`. Uses your own ssh config & keys,
  and detach/switch keys are handled locally just like an on-box session.
- **One list, every host** — `pism ls --all` aggregates your local sessions and
  every ssh-config host that has pism into a single host-tagged table.
- **Fleet-wide updates** — `pism update --all` updates pism on every SSH host that
  has it; `pism config --all <key> <value>` sets config everywhere at once.
- **Stays current** — `pism update` upgrades the binary in place, stable or
  nightly channel, your choice.

Full details and every flag are in **[USAGE.md](USAGE.md)**.

---

## Companion: see the session name *inside* pi

pism names your sessions — [**pi-pism-frame**](https://github.com/vandlol/pi-pism-frame)
puts that name where you can't miss it. It's a small [pi](https://pi.dev)
extension that frames the session with a colored, named header, a status bar, and
the terminal tab title, so while you're attached (or hopping between hosts) you
always see *which* session you're in.

```sh
pi install npm:pi-pism-frame
```

Each session name maps to its own readable pastel, and the style is your choice
(`/pism-frame`). It's optional and stays dormant until a session name is present.

---

## Docs

| Guide | What's inside |
|-------|---------------|
| **[INSTALL.md](INSTALL.md)** | Install, remote install, push a binary, build from source |
| **[USAGE.md](USAGE.md)** | Every command & flag, SSH, config, self-update, how it works, troubleshooting |
| **[CONTRIBUTING.md](CONTRIBUTING.md)** | Branch model, release flow, versioning |
| **[pi-pism-frame ↗](https://github.com/vandlol/pi-pism-frame)** | Companion pi extension: colored named header/bar/title per session |

---

## How it works, in one breath

`pism new` spawns a detached **holder** process that opens a PTY, runs
`pi --session-id <uuid>` inside it, and listens on a per-session socket. `pism
attach` streams your terminal to that holder and back; detaching just disconnects
— the holder (and pi) keep running. `pism ls` reads pi's own transcripts to
recover each session's topic. No central daemon, one holder per session. The
[full architecture](USAGE.md#how-it-works) is in USAGE.md.

Built with Go and [go-pty](https://github.com/aymanbagabas/go-pty) (creack/pty on
Unix, ConPTY on Windows). Static, dependency-free, ~3.5 MB per platform.
