#!/usr/bin/env bash
# Cut a dots release: test, bump the version, tag, publish the GitHub release,
# and point both Homebrew tap formulas and the tap README at it.
#
# Usage: scripts/release.sh [--dry-run] X.Y.Z
# Env:   TAP_DIR  path to the homebrew-tap checkout (default ../homebrew-tap)
set -euo pipefail

DRY_RUN=0
if [[ "${1:-}" == "--dry-run" ]]; then DRY_RUN=1; shift; fi
VERSION="${1:-}"
if [[ ! "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "usage: $0 [--dry-run] X.Y.Z" >&2
  exit 2
fi

REPO_DIR="$(cd "$(dirname "$0")/.." && pwd)"
TAP_DIR="${TAP_DIR:-$REPO_DIR/../homebrew-tap}"
TAG="v$VERSION"
MAIN_GO="$REPO_DIR/cmd/dots/main.go"
URL="https://codeload.github.com/kevnnard/dots/tar.gz/refs/tags/$TAG"

run() {
  if [[ $DRY_RUN -eq 1 ]]; then echo "[dry-run] $*"; else "$@"; fi
}

cd "$REPO_DIR"

# Preflight: fail before changing anything.
[[ "$(git branch --show-current)" == "main" ]] || { echo "release from main only" >&2; exit 1; }
if [[ -n "$(git status --porcelain --untracked-files=no)" ]]; then
  echo "tracked changes present; commit or stash first" >&2; exit 1
fi
if git rev-parse -q --verify "refs/tags/$TAG" >/dev/null || git ls-remote --exit-code --tags origin "refs/tags/$TAG" >/dev/null 2>&1; then
  echo "$TAG already exists" >&2; exit 1
fi
[[ -f "$TAP_DIR/Formula/dots.rb" ]] || { echo "tap formula not found under $TAP_DIR (set TAP_DIR)" >&2; exit 1; }
if [[ -n "$(git -C "$TAP_DIR" status --porcelain --untracked-files=no)" ]]; then
  echo "tap checkout has tracked changes: $TAP_DIR" >&2; exit 1
fi
gh auth status >/dev/null 2>&1 || { echo "gh is not authenticated" >&2; exit 1; }

echo "==> tests"
go test ./pkg/... ./cmd/... -count=1

echo "==> bump version to $VERSION"
run sed -i '' -E "s/^(\tversion[[:space:]]*= )\"[0-9.]+\"/\1\"$VERSION\"/" "$MAIN_GO"
run git add "$MAIN_GO"
run git commit -m "chore(release): $TAG"

echo "==> tag and push"
run git tag -a "$TAG" -m "dots $TAG"
run git push origin main
run git push origin "$TAG"

echo "==> archive hash (the exact tarball Homebrew downloads)"
# Hash codeload's tarball for the tag. A local git archive has different bytes,
# so its hash would fail Homebrew's check.
SHA="DRY-RUN-SHA"
if [[ $DRY_RUN -eq 0 ]]; then
  ARCHIVE="$(mktemp -d)/dots-$TAG.tar.gz"
  for attempt in 1 2 3 4 5; do
    if curl -fsSL "$URL" -o "$ARCHIVE"; then break; fi
    echo "archive not ready (attempt $attempt), retrying" >&2
    sleep 5
  done
  SHA="$(shasum -a 256 "$ARCHIVE" | cut -d' ' -f1)"
fi
echo "sha256 $SHA"

echo "==> GitHub release"
run gh release create "$TAG" ${ARCHIVE:+"$ARCHIVE"} --title "dots $TAG" --generate-notes

echo "==> tap formulas and README"
if [[ $DRY_RUN -eq 1 ]]; then
  echo "[dry-run] would set url $URL, version $VERSION, sha256 $SHA in Formula/dots.rb and Formula/kevnnard-dots.rb"
  echo "[dry-run] would refresh the dots block in README.md, then commit and push the tap"
else
  python3 "$REPO_DIR/scripts/release_tap.py" "$TAP_DIR" "$URL" "$VERSION" "$SHA"
  git -C "$TAP_DIR" add Formula/dots.rb Formula/kevnnard-dots.rb README.md
  git -C "$TAP_DIR" commit -m "dots $TAG"
  git -C "$TAP_DIR" push origin HEAD
fi

echo "==> done: $TAG"
echo "   brew update && brew upgrade kevnnard/tap/dots"
