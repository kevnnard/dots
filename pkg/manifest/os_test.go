package manifest

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestAppliesToWithoutOSListMeansEverywhere(t *testing.T) {
	app := AppConfig{}
	path := PathMapping{Src: "~/.x", Dest: "configs/x"}
	for _, goos := range []string{"darwin", "linux", "windows"} {
		if !app.AppliesTo(goos) || !path.AppliesTo(goos) {
			t.Errorf("absent os must apply to %s", goos)
		}
		if got := path.SrcFor(goos); got != "~/.x" {
			t.Errorf("SrcFor(%s) = %q, want the plain src", goos, got)
		}
	}
}

func TestAppliesToHonorsOSList(t *testing.T) {
	app := AppConfig{OS: []string{"darwin"}}
	if !app.AppliesTo("darwin") || app.AppliesTo("linux") {
		t.Errorf("app os [darwin] must only apply to darwin")
	}
	path := PathMapping{Src: "~/Library/x", Dest: "configs/x", OS: []string{"darwin", "linux"}, SrcOS: map[string]string{"linux": "~/.config/x"}}
	if !path.AppliesTo("linux") || path.AppliesTo("windows") {
		t.Errorf("path os [darwin linux] applies to linux only among linux/windows")
	}
	if got := path.SrcFor("linux"); got != "~/.config/x" {
		t.Errorf("SrcFor(linux) = %q, want the src_os entry", got)
	}
	if got := path.SrcFor("darwin"); got != "~/Library/x" {
		t.Errorf("SrcFor(darwin) = %q, want the plain src", got)
	}
}

func TestForOSKeepsApplicablePathsWithEffectiveSrc(t *testing.T) {
	app := AppConfig{Paths: []PathMapping{
		{Src: "~/a", Dest: "a"},
		{Src: "~/b", Dest: "b", OS: []string{"darwin"}},
		{Src: "~/c", Dest: "c", SrcOS: map[string]string{"linux": "~/.config/c"}},
	}}
	got := app.ForOS("linux").Paths
	if len(got) != 2 || got[0].Src != "~/a" || got[1].Src != "~/.config/c" || got[1].Dest != "c" {
		t.Errorf("ForOS(linux).Paths = %+v", got)
	}
	if len(app.Paths) != 3 || app.Paths[2].Src != "~/c" {
		t.Errorf("ForOS must not modify the original app: %+v", app.Paths)
	}
}

func writeManifest(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "manifest.yaml")
	if err := os.WriteFile(p, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadRejectsUnknownOSValues(t *testing.T) {
	cases := map[string]string{
		"app os":     "version: \"1\"\napps:\n  tool:\n    os: [macos]\n    paths: []\n",
		"path os":    "version: \"1\"\napps:\n  tool:\n    paths:\n      - src: ~/.x\n        dest: x\n        os: [bsd]\n",
		"src_os key": "version: \"1\"\napps:\n  tool:\n    paths:\n      - src: ~/.x\n        dest: x\n        src_os:\n          win: ~/AppData/x\n",
	}
	for name, body := range cases {
		_, err := Load(writeManifest(t, body))
		if err == nil {
			t.Errorf("%s: unknown os value must be rejected", name)
			continue
		}
		if !strings.Contains(err.Error(), "tool") || !strings.Contains(err.Error(), "darwin, linux, windows") {
			t.Errorf("%s: error must name the app and the valid values, got %v", name, err)
		}
	}
}

func TestOSFieldsRoundTrip(t *testing.T) {
	body := `version: "1"
apps:
  tool:
    os: [darwin, linux]
    paths:
      - src: ~/Library/tool
        dest: configs/tool
        os: [darwin, linux]
        src_os:
          linux: ~/.config/tool
`
	m, err := Load(writeManifest(t, body))
	if err != nil {
		t.Fatal(err)
	}
	app := m.Apps["tool"]
	if !reflect.DeepEqual(app.OS, []string{"darwin", "linux"}) || app.Paths[0].SrcOS["linux"] != "~/.config/tool" {
		t.Fatalf("loaded %+v", app)
	}
	out := filepath.Join(t.TempDir(), "out.yaml")
	if err := m.Save(out); err != nil {
		t.Fatal(err)
	}
	again, err := Load(out)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(again, m) {
		t.Errorf("round trip changed the manifest:\n%+v\n%+v", m, again)
	}
}

func TestManifestWithoutOSFieldsSavesWithoutThem(t *testing.T) {
	m := &Manifest{Version: "1", Apps: map[string]AppConfig{
		"tool": {Paths: []PathMapping{{Src: "~/.x", Dest: "x"}}},
	}}
	out := filepath.Join(t.TempDir(), "out.yaml")
	if err := m.Save(out); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "os:") || strings.Contains(string(data), "src_os") {
		t.Errorf("absent os fields must be omitted:\n%s", data)
	}
	again, err := Load(out)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(again, m) {
		t.Errorf("round trip changed the manifest:\n%+v\n%+v", m, again)
	}
}
