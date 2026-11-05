"""Point the Homebrew tap at a dots release.

Rewrites the url/version/sha256 of both dots formulas. The tap README is a
generic index of every package and is maintained by hand, so it is not touched
here. Used by scripts/release.sh; it does not commit or push.

Usage: release_tap.py TAP_DIR URL VERSION SHA256
"""
import re
import sys

FORMULAS = ("Formula/dots.rb", "Formula/kevnnard-dots.rb")


def formula_body(url, version, sha):
    return f'  url "{url}"\n  version "{version}"\n  sha256 "{sha}"\n'


def update_formula(path, url, version, sha):
    src = open(path).read()
    pattern = r'  url "https://codeload[^\n]*\n  version "[^"]*"\n  sha256 "[0-9a-f]{64}"\n'
    body = formula_body(url, version, sha)
    src, count = re.subn(pattern, lambda _: body, src, count=1)
    if count == 0:
        src = src.replace('  head "', body + '  head "', 1)
    open(path, "w").write(src)


def main():
    tap_dir, url, version, sha = sys.argv[1:5]
    for formula in FORMULAS:
        update_formula(f"{tap_dir}/{formula}", url, version, sha)


if __name__ == "__main__":
    main()
