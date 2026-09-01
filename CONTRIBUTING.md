# Contributing to pism

Thanks for hacking on pism. This document explains the branch model, how
releases and versioning work, and the day-to-day workflow.

## TL;DR

```
feature/*  ──PR──▶  dev  ──PR──▶  main
  (work)         (integration)   (stable)
```

- Do your work on a **feature branch** off `dev`.
- Open a **PR into `dev`**. Merging it cuts a **patch pre-release**.
- To ship stable, open a **PR from `dev` into `main`**. Merging it cuts a
  **minor** (default) or **major** release.
- You never push directly to `dev` or `main`, and you never PR a feature branch
  straight into `main`.

## Branches

| Branch | Purpose | How it changes |
|--------|---------|----------------|
| `feature/*`, `fix/*`, … | your work | you push freely |
| `dev` | integration / pre-release channel | **PRs from feature branches only** |
| `main` | stable releases (default branch) | **PRs from `dev` only** |

Both `dev` and `main` are protected: no direct pushes, PR required, CI must be
green. A `guard` check rejects any PR into `main` whose source branch isn't
`dev`.

## Workflow

1. Branch off `dev`:
   ```sh
   git switch dev && git pull
   git switch -c feature/my-thing
   ```
2. Make changes. Keep it green locally:
   ```sh
   make vet test        # go vet + go test
   make build           # local binary
   ```
3. Push and open a PR **into `dev`**. CI runs vet/test/cross-compile.
4. Merge (you can self-merge; no approvals required). → a **patch pre-release**
   is published automatically.
5. When `dev` is ready to ship, open a PR **from `dev` into `main`**. Add a
   release label if you want to override the bump (see below), then merge. → a
   **stable release** is published automatically.

Prefer **squash merges** to keep history readable.

## Versioning (SemVer, no `v` prefix)

Versions are plain `MAJOR.MINOR.PATCH` git tags (e.g. `0.3.0`) — **no `v`
prefix**. The version is derived from tags and baked into the binary at build
time (`pism version`). The next version is computed from the highest existing
tag by `scripts/next-version.sh`.

| Merge | Bump | Example (from `0.3.0`) | GitHub release |
|-------|------|------------------------|----------------|
| feature → `dev` | **patch** | `0.3.1`, `0.3.2`, … | **pre-release** |
| `dev` → `main` | **minor** (default) | `0.4.0` | **stable** |
| `dev` → `main` + `release:major` | **major** | `1.0.0` | **stable** |
| `dev` → `main` + `release:patch` | **patch** | `0.3.1` | **stable** |

Example timeline:

```
0.3.0                         (stable)
  feature → dev   -> 0.3.1    (pre-release)
  feature → dev   -> 0.3.2    (pre-release)
  dev → main      -> 0.4.0    (stable)          ← minor
  dev → main +major-> 1.0.0   (stable)          ← override
```

### Overriding the bump on a stable release

Add exactly one label to the `dev → main` PR:

- `release:major` — breaking changes → `X` bumps
- `release:minor` — features (default; label optional)
- `release:patch` — fixes only

No label ⇒ **minor**.

## Release channels & `pism update`

Releases feed the self-updater:

- **stable** (default) — `pism update` pulls the newest **non-pre-release** via
  `…/releases/latest/download`.
- **unstable** — `pism update --pre` (or `pism config update-channel unstable`)
  pulls the newest release **including pre-releases** via the GitHub API.

```sh
pism config update-channel stable     # or: latest
pism config update-channel unstable   # or: dev / nightly
pism update                           # respects the configured channel
pism update --pre                     # one-off: grab the latest dev build
pism update --stable                  # one-off: force stable
```

So merging into `dev` gives dev-channel testers a fresh build via
`pism update --pre`, while stable users only move when you merge into `main`.

## CI / automation

Workflows in `.github/workflows/`:

- `ci.yml` — vet, test, cross-compile on every PR/push to `dev`/`main`.
- `guard.yml` — rejects PRs into `main` that don't come from `dev`.
- `release-dev.yml` — on merge to `dev`: tag patch, build, publish pre-release.
- `release.yml` — on merge to `main`: tag minor/major (per label), build,
  publish stable release.

All builds are static (`CGO_ENABLED=0`) for linux/darwin/windows × amd64/arm64,
produced by `make dist`, with the version injected via
`-ldflags -X main.version=<tag>`.

## Local development

```sh
make build      # ./pism for your platform
make install    # -> ~/.local/bin/pism
make dist       # cross-compile everything into ./dist
make vet test   # checks
```

Requires Go 1.26+. pism itself has no runtime dependencies; it execs `pi`
directly in a PTY and never goes through a shell.

## Code layout

```
main.go, cli_helpers.go        CLI dispatch, flag/arg parsing
internal/holder/               detached PTY holder (spawns pi) + launch
internal/client/               attach: raw-mode terminal proxy, detach, resize
internal/proto/                framed client<->holder protocol
internal/transport/            unix socket (Unix) / named pipe (Windows)
internal/session/              metadata, state dirs, topic reader
internal/manager/              liveness, ls rows, kill, gc
internal/config/               git-style config file
internal/update/               self-updater (stable + unstable channels)
internal/remote/               ssh/scp forwarding + push
internal/ui/                   ls table rendering
```
