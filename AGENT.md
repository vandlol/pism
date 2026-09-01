# AGENT.md

Guidance for AI agents working in this repository.

## Read this first

- **Read [CONTRIBUTING.md](CONTRIBUTING.md) before making changes.** It defines
  the branch model (`feature/* → dev → main`), the PR-based release flow, and
  SemVer/versioning rules. Never push directly to `dev` or `main`, and never PR a
  feature branch straight into `main`.

## Project

`pism` is a cross-platform (Linux · macOS · Windows), tmux-free session manager
for `pi`. It keeps pi alive in detached PTY holders, supports reliable
re-attach locally and over SSH, and lists sessions by their topic.

## Keep docs in sync with capabilities

The user-facing surfaces must reflect the code's *current* capabilities. When you
add, change, or remove a command, flag, or config key, update **all three**:

- **`usage()` in `main.go`** — the `pism help` output.
- **`README.md`** — Commands, Flags, Configuration, and the relevant prose.
- **`internal/config/config.go`** `Keys` — the canonical config-key list (drives
  validation, the generated config template, and `pism config` descriptions).

> Status: `pism help` and `README.md` were last revisited and updated to match the
> current capabilities (commands incl. `logs`, `config --all`, `update --all`;
> `new -w/--wait`; `-v/-vv/-vvv` verbosity; `--connect-timeout`; the full
> config-key set incl. `update-channel`/`update-exclude`/`ready-timeout`). Keep
> them in sync on every capability change.

For the build/verify workflow, release model, and branch rules, see
[CONTRIBUTING.md](CONTRIBUTING.md).
