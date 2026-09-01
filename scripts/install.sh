#!/bin/sh
# pism installer for Linux & macOS.
#
#   curl -fsSL https://raw.githubusercontent.com/vandlol/pism/main/scripts/install.sh | sh
#
# Env overrides:
#   PISM_BASE_URL     download base (default: latest GitHub release)
#   PISM_VERSION      release tag to install (default: latest)
#   PISM_INSTALL_DIR  install directory (default: ~/.local/bin)
set -eu

REPO="vandlol/pism"
if [ -n "${PISM_VERSION:-}" ]; then
  DEFAULT_BASE="https://github.com/${REPO}/releases/download/${PISM_VERSION}"
else
  DEFAULT_BASE="https://github.com/${REPO}/releases/latest/download"
fi
BASE_URL="${PISM_BASE_URL:-$DEFAULT_BASE}"
INSTALL_DIR="${PISM_INSTALL_DIR:-$HOME/.local/bin}"

say()  { printf '%s\n' "$*"; }
die()  { printf 'pism-install: %s\n' "$*" >&2; exit 1; }

# --- detect platform ---------------------------------------------------------
os="$(uname -s)"
arch="$(uname -m)"
case "$os" in
  Linux)  goos=linux ;;
  Darwin) goos=darwin ;;
  *) die "unsupported OS: $os (use the PowerShell installer on Windows)" ;;
esac
case "$arch" in
  x86_64|amd64)   goarch=amd64 ;;
  aarch64|arm64)  goarch=arm64 ;;
  *) die "unsupported architecture: $arch" ;;
esac

asset="pism-${goos}-${goarch}"
url="${BASE_URL%/}/${asset}"

# --- pick a downloader -------------------------------------------------------
if command -v curl >/dev/null 2>&1; then
  fetch() { curl -fsSL "$1" -o "$2"; }
elif command -v wget >/dev/null 2>&1; then
  fetch() { wget -qO "$2" "$1"; }
else
  die "need curl or wget"
fi

# --- download + install ------------------------------------------------------
tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT
say "downloading ${asset} from ${BASE_URL%/} ..."
fetch "$url" "$tmp" || die "download failed: $url"

# sanity: reject tiny/HTML error bodies
size="$(wc -c < "$tmp" | tr -d ' ')"
[ "$size" -gt 1024 ] || die "downloaded file too small ($size bytes) — wrong URL?"

chmod +x "$tmp"
mkdir -p "$INSTALL_DIR"
dest="${INSTALL_DIR}/pism"
mv "$tmp" "$dest"
trap - EXIT

say "installed: $dest"
"$dest" version 2>/dev/null || true

# --- PATH hint ---------------------------------------------------------------
case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *)
    say ""
    say "NOTE: $INSTALL_DIR is not on your PATH. Add it, e.g.:"
    say "  echo 'export PATH=\"$INSTALL_DIR:\$PATH\"' >> ~/.profile"
    ;;
esac

say ""
say "try:  pism new        (start + attach a pi session)"
say "      pism ls         (list sessions by topic)"
say "      pism update     (self-update in place)"
