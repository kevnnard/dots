package homepath

import (
	"os"
	"path/filepath"
	"testing"
)

// fakeBrewPrefix creates a directory that holds bin/brew, as a real Homebrew
// prefix does, and makes it the prefix of this test.
func fakeBrewPrefix(t *testing.T) string {
	t.Helper()
	prefix := filepath.Join(t.TempDir(), "homebrew")
	if err := os.MkdirAll(filepath.Join(prefix, "bin"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(prefix, "bin", "brew"), []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOMEBREW_PREFIX", prefix)
	return prefix
}

// noBrewPrefix makes this test run on a machine without a verified prefix.
func noBrewPrefix(t *testing.T) {
	t.Helper()
	t.Setenv("HOMEBREW_PREFIX", filepath.Join(t.TempDir(), "absent"))
}

func TestBrewPrefixRequiresVerifiedInstallation(t *testing.T) {
	prefix := fakeBrewPrefix(t)
	if got := BrewPrefix(); got != prefix {
		t.Errorf("BrewPrefix() = %q, want %q", got, prefix)
	}

	noBrewPrefix(t)
	if got := BrewPrefix(); got != "" {
		t.Errorf("a prefix without bin/brew must be refused, got %q", got)
	}

	t.Setenv("HOMEBREW_PREFIX", "relative/homebrew")
	if got := BrewPrefix(); got != "" {
		t.Errorf("a relative prefix must be refused, got %q", got)
	}
}

func TestTokenizeReplacesBrewPrefix(t *testing.T) {
	prefix := fakeBrewPrefix(t)
	cases := []struct{ name, in, want string }{
		{"command path", `command = "` + prefix + `/bin/engram"`, `command = "__BREW_PREFIX__/bin/engram"`},
		{"prefix alone", `BREW=` + prefix, `BREW=__BREW_PREFIX__`},
		{"longer name untouched", prefix + `x/bin/node`, prefix + `x/bin/node`},
		{"home and prefix together", `/Users/dev/a ` + prefix + `/bin/b`, `__HOME__/a __BREW_PREFIX__/bin/b`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := string(Tokenize([]byte(tc.in), "/Users/dev")); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTokenizePrefersBrewPrefixInsideHome(t *testing.T) {
	home := t.TempDir()
	prefix := filepath.Join(home, "homebrew")
	if err := os.MkdirAll(filepath.Join(prefix, "bin"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(prefix, "bin", "brew"), nil, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOMEBREW_PREFIX", prefix)
	in := prefix + "/bin/node " + home + "/.config"
	if got := string(Tokenize([]byte(in), home)); got != "__BREW_PREFIX__/bin/node __HOME__/.config" {
		t.Errorf("got %q", got)
	}
}

func TestTokenizeLeavesBrewPrefixInBinaryUntouched(t *testing.T) {
	prefix := fakeBrewPrefix(t)
	bin := []byte("\x00" + prefix + "/bin/x")
	if got := Tokenize(bin, "/Users/dev"); string(got) != string(bin) {
		t.Errorf("binary data must not be rewritten, got %q", got)
	}
}

func TestExpandBrewPrefix(t *testing.T) {
	prefix := fakeBrewPrefix(t)
	in := []byte(`command = "__BREW_PREFIX__/bin/engram" # __HOME__/x`)
	want := `command = "` + prefix + `/bin/engram" # /home/dev/x`
	if got := string(Expand(in, "/home/dev")); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if Unresolved(in) {
		t.Error("a token with a verified prefix is resolved")
	}
}

func TestExpandKeepsBrewTokenWithoutPrefix(t *testing.T) {
	noBrewPrefix(t)
	in := []byte(`command = "__BREW_PREFIX__/bin/engram" # __HOME__/x`)
	want := `command = "__BREW_PREFIX__/bin/engram" # /home/dev/x`
	if got := string(Expand(in, "/home/dev")); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if !Unresolved(in) {
		t.Error("the token must be reported unresolved without a verified prefix")
	}
	if Unresolved([]byte("no token here")) {
		t.Error("data without the token is never unresolved")
	}
}

func TestBrewPrefixRoundTripAcrossMachines(t *testing.T) {
	mac := fakeBrewPrefix(t)
	repo := Tokenize([]byte(`{"node":"`+mac+`/bin/node"}`), "/Users/dev")
	linux := fakeBrewPrefix(t)
	if got, want := string(Expand(repo, "/home/dev")), `{"node":"`+linux+`/bin/node"}`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	// The deployed file is equivalent to the backup on the destination.
	if !Equivalent([]byte(`{"node":"`+linux+`/bin/node"}`), repo, "/home/dev") {
		t.Error("a deployed file must be equivalent to its tokenized backup")
	}
}
