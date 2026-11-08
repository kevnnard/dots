package manifest

import (
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Package links are added to the named apps only; every other field of the
// manifest, including fields this dots does not know, is kept.
func TestWithPackageLinksSetsOnlyTheNamedApps(t *testing.T) {
	data := []byte(`version: "1.0.0"
# kept comment
apps:
    claude:
        paths:
            - src: ~/.claude/skills
              dest: configs/claude/skills
        package_links:
            - path: configs/claude/skills/stale
              manager: homebrew
              kind: cask
              package: stale
              resource: r
        future_field: kept
    zsh:
        paths:
            - src: ~/.zshrc
              dest: configs/zsh/.zshrc
`)
	link := PackageLink{Path: "configs/claude/skills/notes", Manager: "homebrew", Kind: "cask", Package: "acme-notes", Version: "1.0.0", Resource: "acme-notes/skills/notes", Type: "dir"}
	out, err := WithPackageLinks(data, map[string][]PackageLink{"claude": {link}})
	if err != nil {
		t.Fatal(err)
	}
	var m Manifest
	if err := yaml.Unmarshal(out, &m); err != nil {
		t.Fatalf("result is not a manifest: %v\n%s", err, out)
	}
	if got := m.Apps["claude"].PackageLinks; !reflect.DeepEqual(got, []PackageLink{link}) {
		t.Errorf("claude package_links = %+v, want only %+v", got, link)
	}
	if len(m.Apps["zsh"].PackageLinks) != 0 || m.Apps["zsh"].Paths[0].Dest != "configs/zsh/.zshrc" {
		t.Errorf("zsh changed: %+v", m.Apps["zsh"])
	}
	for _, kept := range []string{"# kept comment", "future_field: kept"} {
		if !strings.Contains(string(out), kept) {
			t.Errorf("%q lost:\n%s", kept, out)
		}
	}
	if _, err := WithPackageLinks(data, map[string][]PackageLink{"nope": {link}}); err == nil {
		t.Error("an app the manifest does not declare must be refused")
	}
	if _, err := WithPackageLinks([]byte("apps: [\n"), map[string][]PackageLink{"claude": {link}}); err == nil {
		t.Error("an unreadable manifest must be refused")
	}
}
