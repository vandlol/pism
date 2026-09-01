#!/bin/sh
# next-version.sh <patch|minor|major>
#
# Prints the next semantic version based on the highest existing git tag.
# Tags are plain numbers (no "v" prefix); a legacy leading "v" is tolerated on
# read. Any pre-release suffix (e.g. -dev.3) is stripped before bumping.
#
#   patch  X.Y.Z   -> X.Y.(Z+1)     (dev channel)
#   minor  X.Y.Z   -> X.(Y+1).0     (stable release)
#   major  X.Y.Z   -> (X+1).0.0     (override)
set -eu

bump="${1:?usage: next-version.sh <patch|minor|major>}"

# highest semver tag, v-prefix tolerated, sorted by version
latest=$(git tag -l 2>/dev/null | sed 's/^v//' \
  | grep -E '^[0-9]+\.[0-9]+\.[0-9]+' | sort -V | tail -n1 || true)
[ -n "$latest" ] || latest="0.0.0"

base=${latest%%-*}                 # strip -dev.N / pre-release suffix
major=$(printf %s "$base" | cut -d. -f1)
minor=$(printf %s "$base" | cut -d. -f2)
patch=$(printf %s "$base" | cut -d. -f3)

case "$bump" in
  major) major=$((major + 1)); minor=0; patch=0 ;;
  minor) minor=$((minor + 1)); patch=0 ;;
  patch) patch=$((patch + 1)) ;;
  *) echo "unknown bump: $bump" >&2; exit 2 ;;
esac

printf '%s.%s.%s\n' "$major" "$minor" "$patch"
