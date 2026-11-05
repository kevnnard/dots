#!/usr/bin/env bash
# Cut a dots release: bump the version, tag it, publish the GitHub release
# with its source archive, and point the Homebrew tap's stable formula at it.
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
FORMULA="$TAP_DIR/Formula/dots.rb"

run() {
  if [[ $DRY_RUN -eq 1 ]]; then echo "[dry-run] $*"; else "$@"; fi
}

cd "$REPO_DIR"

# Preflight: fail before touching anything.
[[ "$(git branch --show-current)" == "main" ]] || { echo "release from main only" >&2; exit 1; }
if [[ -n "$(git status --porcelain --untracked-files=no)" ]]; then
  echo "tracked changes present; commit or stash first" >&2; exit 1
fi
if git rev-parse -q --verify "refs/tags/$TAG" >/dev/null || git ls-remote --exit-code --tags origin "refs/tags/$TAG" >/dev/null 2>&1; then
  echo "$TAG already exists" >&2; exit 1
fi
[[ -f "$FORMULA" ]] || { echo "tap formula not found: $FORMULA (set TAP_DIR)" >&2; exit 1; }
if [[ -n "$(git -C "$TAP_DIR" status --porcelain --untracked-files=no)" ]]; then
  echo "tap checkout has changes: $TAP_DIR" >&2; exit 1
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

echo "==> source archive (committed content only)"
ARCHIVE="$(mktemp -d)/dots-$TAG.tar.gz"
run git archive --format=tar.gz --prefix="dots-$VERSION/" -o "$ARCHIVE" "$TAG"
SHA="$( [[ $DRY_RUN -eq 1 ]] && echo DRY-RUN-SHA || shasum -a 256 "$ARCHIVE" | cut -d' ' -f1 )"
echo "sha256 $SHA"

echo "==> GitHub release"
run gh release create "$TAG" "$ARCHIVE" --title "dots $TAG" --generate-notes

echo "==> tap formula"
URL="https://github.com/kevnnard/dots/archive/refs/tags/$TAG.tar.gz"
if [[ $DRY_RUN -eq 1 ]]; then
  echo "[dry-run] would set stable url $URL sha256 $SHA in $FORMULA"
else
  python3 - "$FORMULA" "$URL" "$SHA" <<'PY'
import re, sys
path, url, sha = sys.argv[1:]
src = open(path).read()
stable = f'  stable do\n    url "{url}"\n    sha256 "{sha}"\n  end\n\n'
if "  stable do" in src:
    src = re.sub(r"  stable do\n.*?\n  end\n\n", stable, src, count=1, flags=re.S)
else:
    src = src.replace('  head "', stable + '  head "', 1)
open(path, "w").write(src)
PY
  python3 - "$TAP_DIR/README.md" "$VERSION" <<'PY'
import sys
path, version = sys.argv[1:]
block = (
    "<!-- dots:start -->\n"
    f"## dots {version}\n\n"
    "```bash\n"
    "brew tap kevnnard/tap\n"
    "brew install kevnnard/tap/dots\n"
    "brew upgrade kevnnard/tap/dots\n"
    "```\n"
    "<!-- dots:end -->\n"
)
src = open(path).read()
if "<!-- dots:start -->" in src:
    start = src.index("<!-- dots:start -->")
    end = src.index("<!-- dots:end -->") + len("<!-- dots:end -->\n")
    src = src[:start] + block + src[end:]
else:
    src = src.rstrip("\n") + "\n\n" + block
open(path, "w").write(src)
PY
  git -C "$TAP_DIR" add Formula/dots.rb README.md
  git -C "$TAP_DIR" commit -m "dots $TAG"
  git -C "$TAP_DIR" push origin HEAD
fi

echo "==> done: $TAG"
echo "   brew update && brew upgrade kevnnard/tap/dots"
