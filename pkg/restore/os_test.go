package restore

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/kevnnard/dots/pkg/manifest"
)

// osFixture stages one backed-up file and returns the repository and a home
// directory for targets.
func osFixture(t *testing.T) (repo, home string) {
	t.Helper()
	root := t.TempDir()
	repo, home = filepath.Join(root, "repo"), filepath.Join(root, "home")
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(repo, "configs", "tool"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "configs", "tool", "conf"), []byte("backed up\n"), 0644); err != nil {
		t.Fatal(err)
	}
	return repo, home
}

func TestApplySkipsPathNotForReceivingSystem(t *testing.T) {
	repo, home := osFixture(t)
	target := filepath.Join(home, "Library", "tool", "conf")
	app := manifest.AppConfig{Paths: []manifest.PathMapping{
		{Src: target, Dest: "configs/tool/conf", OS: []string{"darwin"}},
	}}
	res, err := ApplyAppFilesWith("tool", app, repo, ApplyOptions{Replace: true, OS: "linux"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Errorf("a darwin-only path must not be written on linux")
	}
	if len(res.NotForOS) != 1 || !strings.Contains(res.NotForOS[0], "not for linux") {
		t.Errorf("NotForOS = %v, want one \"not for linux\" entry", res.NotForOS)
	}
	if len(res.Warnings) != 0 || len(res.Restored) != 0 {
		t.Errorf("skipping is not a failure: %+v", res)
	}
}

func TestApplyDeploysToSrcOSEntry(t *testing.T) {
	repo, home := osFixture(t)
	darwinTarget := filepath.Join(home, "Library", "tool", "conf")
	linuxTarget := filepath.Join(home, ".config", "tool", "conf")
	app := manifest.AppConfig{Paths: []manifest.PathMapping{
		{Src: darwinTarget, Dest: "configs/tool/conf", SrcOS: map[string]string{"linux": linuxTarget}},
	}}
	res, err := ApplyAppFilesWith("tool", app, repo, ApplyOptions{Replace: true, OS: "linux"})
	if err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(linuxTarget); err != nil || string(data) != "backed up\n" {
		t.Errorf("src_os target = %q, %v", data, err)
	}
	if _, err := os.Lstat(darwinTarget); !os.IsNotExist(err) {
		t.Errorf("the plain src must not be written when src_os names the receiving system")
	}
	if !reflect.DeepEqual(res.Restored, []string{linuxTarget}) {
		t.Errorf("Restored = %v", res.Restored)
	}
}

func TestRunSkipsAppNotForReceivingSystemWithoutFailing(t *testing.T) {
	repo, home := osFixture(t)
	target := filepath.Join(home, ".tool", "conf")
	m := &manifest.Manifest{Apps: map[string]manifest.AppConfig{
		"smartwindow": {
			OS:      []string{"darwin"},
			Paths:   []manifest.PathMapping{{Src: target, Dest: "configs/tool/conf"}},
			Install: map[string]manifest.InstallRecipe{"default": {Manager: "script", Command: "true"}},
		},
	}}
	for _, explicit := range []bool{false, true} {
		fx := &fakeExec{}
		res, err := Run(m, []string{"smartwindow"}, plainArch(), RunOptions{
			RepoDir: repo, WithConfig: true, ReplaceExisting: true, InstallPackages: true,
			Explicit: explicit, Probe: noneInstalled, Exec: fx.run,
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(fx.calls) != 0 {
			t.Errorf("explicit=%v: nothing may be installed for an app not for linux, ran %s", explicit, fx.apps())
		}
		if s := resultByName(t, res, "smartwindow"); s.Install != OutcomeNotForOS || s.Config != ConfigNotForOS {
			t.Errorf("explicit=%v: smartwindow = %+v", explicit, s)
		}
		if p := res.Problems(); len(p) != 0 {
			t.Errorf("explicit=%v: an app not for this system is not a problem, got %v", explicit, p)
		}
		if summary := strings.Join(res.Summary(), "\n"); !strings.Contains(summary, "not for linux") {
			t.Errorf("explicit=%v: summary must report \"not for linux\":\n%s", explicit, summary)
		}
		if _, err := os.Lstat(target); !os.IsNotExist(err) {
			t.Errorf("explicit=%v: configuration of an app not for linux was written", explicit)
		}
	}
}

func TestRunReportsPathsNotForReceivingSystem(t *testing.T) {
	repo, home := osFixture(t)
	target := filepath.Join(home, "Library", "tool", "conf")
	m := &manifest.Manifest{Apps: map[string]manifest.AppConfig{
		"tool": {Paths: []manifest.PathMapping{{Src: target, Dest: "configs/tool/conf", OS: []string{"darwin"}}}},
	}}
	res, err := Run(m, []string{"tool"}, plainArch(), RunOptions{RepoDir: repo, WithConfig: true, Probe: noneInstalled, Exec: (&fakeExec{}).run})
	if err != nil {
		t.Fatal(err)
	}
	if s := resultByName(t, res, "tool"); s.Config != ConfigNotForOS {
		t.Errorf("tool = %+v", s)
	}
	if p := res.Problems(); len(p) != 0 {
		t.Errorf("problems = %v", p)
	}
	if summary := strings.Join(res.Summary(), "\n"); !strings.Contains(summary, "not for linux") {
		t.Errorf("summary must report the skipped path:\n%s", summary)
	}
}

func TestManifestWithoutOSFieldsRestoresAsBefore(t *testing.T) {
	repo, home := osFixture(t)
	target := filepath.Join(home, ".config", "tool", "conf")
	m := &manifest.Manifest{Apps: map[string]manifest.AppConfig{
		"tool": {Paths: []manifest.PathMapping{{Src: target, Dest: "configs/tool/conf"}}, Install: pacmanRecipe("tool")},
	}}
	fx := &fakeExec{}
	res, err := Run(m, []string{"tool"}, plainArch(), RunOptions{
		RepoDir: repo, WithConfig: true, InstallPackages: true, Probe: noneInstalled, Exec: fx.run,
	})
	if err != nil {
		t.Fatal(err)
	}
	s := resultByName(t, res, "tool")
	if s.Install != OutcomeInstalled || s.Config != ConfigDeployed || fx.apps() != "tool" {
		t.Errorf("tool = %+v, ran %q", s, fx.apps())
	}
	if !reflect.DeepEqual(s.Files.Restored, []string{target}) || len(s.Files.NotForOS) != 0 || len(s.Files.Skipped) != 0 {
		t.Errorf("files = %+v", s.Files)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "backed up\n" {
		t.Errorf("target = %q, %v", data, err)
	}
	if p := res.Problems(); len(p) != 0 {
		t.Errorf("problems = %v", p)
	}
}

func TestBuildPlanBlocksDependentsOfAppNotForSystem(t *testing.T) {
	m := &manifest.Manifest{Apps: map[string]manifest.AppConfig{
		"mac-helper": {OS: []string{"darwin"}, Install: pacmanRecipe("helper")},
		"consumer":   {Requires: []string{"mac-helper"}, Install: pacmanRecipe("consumer")},
	}}
	steps, err := BuildPlan(m, []string{"consumer"}, plainArch(), PlanOptions{Probe: noneInstalled})
	if err != nil {
		t.Fatal(err)
	}
	if s := stepByName(t, steps, "mac-helper"); s.Status != StatusNotForOS || s.Reason != "not for linux" {
		t.Errorf("mac-helper = %+v", s)
	}
	if s := stepByName(t, steps, "consumer"); s.Status != StatusBlocked {
		t.Errorf("consumer = %+v, want blocked by an app not for linux", s)
	}
}
