package clean

import (
	"testing"

	"github.com/kevnnard/dots/pkg/manifest"
)

func TestInstalledAsksOnlyThatRecipe(t *testing.T) {
	p, f := nativeProbe(t, arch(), map[string]Result{
		"pacman -Q bitwarden":            ok("bitwarden 2026.3.1-2\n"),
		"pacman -Q ttf-iosevkaterm-nerd": missing("error: package 'ttf-iosevkaterm-nerd' was not found"),
		"pacman -Q weird":                exit1("garbage"),
	})
	cases := []struct {
		recipe manifest.InstallRecipe
		want   bool
	}{
		{manifest.InstallRecipe{Manager: "pacman", Package: "bitwarden"}, true},
		{manifest.InstallRecipe{Manager: "yay", Package: "bitwarden"}, true},
		{manifest.InstallRecipe{Manager: "pacman", Package: "ttf-iosevkaterm-nerd"}, false},
		{manifest.InstallRecipe{Manager: "pacman", Package: "weird"}, false}, // unconfirmed is not installed
		{manifest.InstallRecipe{Manager: "script", Command: "curl x | sh"}, false},
		{manifest.InstallRecipe{Manager: "pacman", Package: "-Syu"}, false},
	}
	for _, tc := range cases {
		if got := p.Installed(tc.recipe); got != tc.want {
			t.Errorf("Installed(%+v) = %v, want %v", tc.recipe, got, tc.want)
		}
	}
	if len(f.calls) != 4 {
		t.Errorf("calls = %q", f.calls)
	}
}
