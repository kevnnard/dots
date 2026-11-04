package homepath

import "testing"

func TestTokenize(t *testing.T) {
	cases := []struct {
		name, in, home, want string
	}{
		{"plain path", `"/Users/kevnnard/.codex/hooks.json"`, "/Users/kevnnard", `"__HOME__/.codex/hooks.json"`},
		{"home alone", `dir=/Users/kevnnard`, "/Users/kevnnard", `dir=__HOME__`},
		{"multiple occurrences", "/Users/kevnnard/a /Users/kevnnard/b", "/Users/kevnnard", "__HOME__/a __HOME__/b"},
		{"longer user name untouched", "/Users/kevnnardx/a", "/Users/kevnnard", "/Users/kevnnardx/a"},
		{"other user untouched", "/Users/anua/.vim", "/Users/kevnnard", "/Users/anua/.vim"},
		{"linux home", "/home/dev/.local/bin", "/home/dev", "__HOME__/.local/bin"},
		{"windows backslash", `C:\Users\dev\x`, `C:\Users\dev`, `__HOME__\x`},
		{"windows json escaped", `"C:\\Users\\dev\\x"`, `C:\Users\dev`, `"__HOME__\\x"`},
		{"windows forward slash", `C:/Users/dev/x`, `C:\Users\dev`, `__HOME__/x`},
		{"empty home is a no-op", "/a/b", "", "/a/b"},
		{"root home is a no-op", "/a/b", "/", "/a/b"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := string(Tokenize([]byte(tc.in), tc.home)); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTokenizeLeavesBinaryUntouched(t *testing.T) {
	bin := []byte("\x00\x01/Users/kevnnard/x")
	if got := Tokenize(bin, "/Users/kevnnard"); string(got) != string(bin) {
		t.Errorf("binary data must not be rewritten, got %q", got)
	}
}

func TestExpand(t *testing.T) {
	if got := string(Expand([]byte(`"__HOME__/.codex"`), "/home/dev")); got != `"/home/dev/.codex"` {
		t.Errorf("got %q", got)
	}
	// Windows homes are expanded with forward slashes so JSON stays valid.
	if got := string(Expand([]byte(`"__HOME__/x"`), `C:\Users\dev`)); got != `"C:/Users/dev/x"` {
		t.Errorf("got %q", got)
	}
	bin := []byte("\x00__HOME__")
	if got := Expand(bin, "/home/dev"); string(got) != string(bin) {
		t.Errorf("binary data must not be rewritten, got %q", got)
	}
}

func TestRoundTripAcrossMachines(t *testing.T) {
	src := []byte(`{"cmd":"bash '/Users/kevnnard/.claude/hooks/x.sh'"}`)
	repo := Tokenize(src, "/Users/kevnnard")
	got := Expand(repo, "/home/dev")
	want := `{"cmd":"bash '/home/dev/.claude/hooks/x.sh'"}`
	if string(got) != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
