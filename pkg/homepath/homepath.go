// Package homepath keeps machine-specific locations out of versioned
// dotfiles. Backups replace the current home and the Homebrew prefix with
// placeholders and restores expand them for the target machine, so configs
// travel between macOS, Linux and Windows without hardcoded paths such as
// /Users/<name> or /opt/homebrew.
package homepath

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Token is the placeholder stored in the dotfiles repository.
const Token = "__HOME__"

// BrewToken is the placeholder stored for the Homebrew prefix.
const BrewToken = "__BREW_PREFIX__"

const (
	binarySniffBytes = 8000
	maxRewriteSize   = 8 << 20 // larger files are copied verbatim
)

// Current returns the home directory of the running user.
func Current() (string, error) {
	return os.UserHomeDir()
}

// queriedBrewPrefix asks the brew on PATH for its prefix, once per process.
var queriedBrewPrefix = sync.OnceValue(func() string {
	brew, err := exec.LookPath("brew")
	if err != nil {
		return ""
	}
	out, err := exec.Command(brew, "--prefix").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
})

// BrewPrefix returns the Homebrew prefix of this machine, or "" when none is
// verified. A non-empty HOMEBREW_PREFIX is authoritative; otherwise brew
// --prefix is asked. Either way the prefix must be an absolute directory
// holding bin/brew, so a stale or guessed value is never used.
func BrewPrefix() string {
	prefix := os.Getenv("HOMEBREW_PREFIX")
	if prefix == "" {
		prefix = queriedBrewPrefix()
	}
	if prefix == "" || !filepath.IsAbs(prefix) {
		return ""
	}
	prefix = filepath.Clean(prefix)
	if prefix == filepath.Dir(prefix) {
		return "" // a filesystem root is never a prefix
	}
	if info, err := os.Stat(filepath.Join(prefix, "bin", "brew")); err != nil || info.IsDir() {
		return ""
	}
	return prefix
}

// IsText reports whether data looks like text (no NUL byte near the start).
func IsText(data []byte) bool {
	n := len(data)
	if n > binarySniffBytes {
		n = binarySniffBytes
	}
	return !bytes.Contains(data[:n], []byte{0})
}

// variants returns the spellings of home that may appear in text files:
// native, forward-slash, and JSON-escaped backslashes. Longest first.
func variants(home string) []string {
	set := map[string]bool{home: true}
	if strings.Contains(home, `\`) {
		set[strings.ReplaceAll(home, `\`, "/")] = true
		set[strings.ReplaceAll(home, `\`, `\\`)] = true
	}
	out := make([]string, 0, len(set))
	for v := range set {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return len(out[i]) > len(out[j]) })
	return out
}

func isNameByte(b byte) bool {
	return b == '_' || b == '-' || b == '.' ||
		(b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// Tokenize replaces occurrences of home with Token and of the Homebrew prefix
// with BrewToken. Only whole path prefixes are replaced (so /Users/dev never
// matches /Users/devx), the longest location first so a prefix inside home
// keeps its own token, and binary data is returned unchanged.
func Tokenize(data []byte, home string) []byte {
	if !IsText(data) {
		return data
	}
	type substitution struct{ needle, token string }
	var subs []substitution
	if home != "" && home != "/" && home != `\` {
		for _, v := range variants(home) {
			subs = append(subs, substitution{v, Token})
		}
	}
	if prefix := BrewPrefix(); prefix != "" && prefix != home {
		subs = append(subs, substitution{prefix, BrewToken})
	}
	sort.SliceStable(subs, func(i, j int) bool { return len(subs[i].needle) > len(subs[j].needle) })
	for _, s := range subs {
		data = replaceBounded(data, []byte(s.needle), s.token)
	}
	return data
}

func replaceBounded(data, needle []byte, token string) []byte {
	if !bytes.Contains(data, needle) {
		return data
	}
	var out bytes.Buffer
	i := 0
	for i < len(data) {
		j := bytes.Index(data[i:], needle)
		if j < 0 {
			out.Write(data[i:])
			break
		}
		start := i + j
		end := start + len(needle)
		prevOK := start == 0 || !isNameByte(data[start-1])
		nextOK := end >= len(data) || !isNameByte(data[end])
		out.Write(data[i:start])
		if prevOK && nextOK {
			out.WriteString(token)
		} else {
			out.Write(needle)
		}
		i = end
	}
	return out.Bytes()
}

// Expand replaces Token with home and BrewToken with the Homebrew prefix.
// Windows homes use forward slashes so the result stays valid inside JSON and
// TOML strings. Without a verified prefix BrewToken is kept, so the file shows
// what is missing instead of pointing at another machine's prefix.
func Expand(data []byte, home string) []byte {
	if !IsText(data) {
		return data
	}
	if home != "" && bytes.Contains(data, []byte(Token)) {
		data = bytes.ReplaceAll(data, []byte(Token), []byte(strings.ReplaceAll(home, `\`, "/")))
	}
	if bytes.Contains(data, []byte(BrewToken)) {
		if prefix := BrewPrefix(); prefix != "" {
			data = bytes.ReplaceAll(data, []byte(BrewToken), []byte(prefix))
		}
	}
	return data
}

// Unresolved reports whether Expand leaves BrewToken in data on this machine
// because no Homebrew prefix is verified here.
func Unresolved(data []byte) bool {
	return IsText(data) && bytes.Contains(data, []byte(BrewToken)) && BrewPrefix() == ""
}

// Equivalent reports whether two file contents are the same once the current
// home is normalized to Token on both sides.
func Equivalent(a, b []byte, home string) bool {
	return bytes.Equal(Tokenize(a, home), Tokenize(b, home))
}

// CopyTokenized copies src to dest, replacing the current home with Token.
func CopyTokenized(src, dest string) error {
	return copyWith(src, dest, Tokenize)
}

// CopyExpanded copies src to dest, replacing Token with the current home.
func CopyExpanded(src, dest string) error {
	return copyWith(src, dest, Expand)
}

// CopyRaw copies src to dest byte for byte.
func CopyRaw(src, dest string) error {
	return copyWith(src, dest, nil)
}

// CopyExpandedNew is CopyExpanded into a newly created dest with perm. It
// fails if dest already exists, so an existing file or symlink is never
// followed or truncated.
func CopyExpandedNew(src, dest string, perm os.FileMode) error {
	return copyNew(src, dest, perm, Expand)
}

// CopyRawNew is CopyRaw into a newly created dest with perm. It fails if dest
// already exists.
func CopyRawNew(src, dest string, perm os.FileMode) error {
	return copyNew(src, dest, perm, nil)
}

func copyNew(src, dest string, perm os.FileMode, transform func([]byte, string) []byte) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	data, err := io.ReadAll(in)
	if err != nil {
		return err
	}
	if home, err := Current(); err == nil && transform != nil && len(data) <= maxRewriteSize {
		data = transform(data, home)
	}
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, perm)
	if err != nil {
		return err
	}
	_, err = out.Write(data)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		// dest was created exclusively above, so only our partial copy is removed.
		os.Remove(dest)
	}
	return err
}

func copyWith(src, dest string, transform func([]byte, string) []byte) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return err
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	info, err := in.Stat()
	if err != nil {
		return err
	}

	home, homeErr := Current()
	if transform == nil || homeErr != nil || info.Size() > maxRewriteSize {
		return writeStream(in, dest)
	}

	data, err := io.ReadAll(in)
	if err != nil {
		return err
	}
	return os.WriteFile(dest, transform(data, home), 0644)
}

func writeStream(in io.Reader, dest string) error {
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
