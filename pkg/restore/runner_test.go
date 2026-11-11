package restore

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/kevnnard/dots/pkg/manifest"
)

// piConflictFixture is an isolated HOME whose existing Pi settings carry the
// observed duplicate Engram declarations, with a plugin that requires Pi.
func piConflictFixture(t *testing.T, backup string) (*manifest.Manifest, string, string) {
	t.Helper()
	root, home := isolatedHome(t)
	repo := filepath.Join(root, "repo")
	if backup != "" {
		mustWrite(t, filepath.Join(repo, "configs", "pi", "settings.json"), backup)
	}
	target := filepath.Join(home, ".pi", "agent", "settings.json")
	mustWrite(t, target, conflictingPiSettings)
	m := &manifest.Manifest{Apps: map[string]manifest.AppConfig{
		"pi": {
			Paths:   []manifest.PathMapping{{Src: target, Dest: "configs/pi/settings.json"}},
			Install: pacmanRecipe("pi"),
		},
		"pi-plugin": {Requires: []string{"pi"}, Install: pacmanRecipe("pi-plugin")},
	}}
	return m, repo, target
}

func piConflict(target string) string {
	return "existing " + target + " has conflicting Engram package declarations (obsolete duplicate npm:gentle-engram@0.3.0)"
}

func piPreflight(target string) string {
	return piConflict(target) + "; install blocked: restore configuration with replacement of existing files to repair it after a safety backup"
}

func TestRunBlocksPiInstallOnConflictingLocalSettingsWithoutAuthorizedRepair(t *testing.T) {
	dryRun := "; dry run: a real run would back it up and repair it before installing"
	cases := []struct {
		name, local, suffix string
		opts                RunOptions
	}{
		{"stock install", conflictingPiSettings, "", RunOptions{InstallPackages: true}},
		{"config kept", conflictingPiSettings, "", RunOptions{InstallPackages: true, WithConfig: true}},
		{"dry run", conflictingPiSettings, "", RunOptions{InstallPackages: true, WithConfig: true, DryRun: true}},
		{"replacement dry run", conflictingPiSettings, dryRun, RunOptions{InstallPackages: true, WithConfig: true, ReplaceExisting: true, DryRun: true}},
		{"unknown conflict authorized", `{"packages":[{"source":"npm:gentle-engram","extensions":["+index.ts"]},"npm:gentle-engram@0.4.0"]}`, "unknown",
			RunOptions{InstallPackages: true, WithConfig: true, ReplaceExisting: true}},
	}
	for _, c := range cases {
		m, repo, target := piConflictFixture(t, repairedPiSettings)
		mustWrite(t, target, c.local)
		fx := &fakeExec{}
		opts := c.opts
		opts.RepoDir, opts.Probe, opts.Exec = repo, noneInstalled, fx.run
		res, err := Run(m, []string{"pi-plugin"}, plainArch(), opts)
		if err != nil {
			t.Fatal(err)
		}
		if len(fx.calls) != 0 {
			t.Errorf("%s: executed %s despite the preflight conflict", c.name, fx.apps())
		}
		msg := piPreflight(target)
		switch c.suffix {
		case "unknown":
			msg = "existing " + target + " has conflicting Engram package declarations (unrecognized combination); not repaired automatically; keep a single declaration manually, then retry"
		case dryRun:
			msg = piConflict(target) + dryRun
		}
		pi := resultByName(t, res, "pi")
		if pi.Install != OutcomeBlocked || pi.Preflight != msg || pi.Reason != msg || pi.Prepared != "" {
			t.Errorf("%s: pi = %+v", c.name, pi)
		}
		want := []string{"pi: preflight: " + msg}
		if opts.WithConfig {
			want = append(want, "pi: configuration incomplete: pi is blocked")
		}
		want = append(want, "pi-plugin: blocked: requires pi (blocked)")
		if got := res.Problems(); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: problems = %q", c.name, got)
		}
		if readOrEmpty(target) != c.local || len(listDir(t, filepath.Dir(target))) != 1 {
			t.Errorf("%s: local settings were modified", c.name)
		}
	}
}

// recordingExec captures the local settings bytes each installer sees.
func recordingExec(target string, fail string, seen *[]string) Executor {
	return func(req ExecRequest) error {
		*seen = append(*seen, req.AppName+"="+readOrEmpty(target))
		if req.AppName == fail {
			return errors.New("exit status 1")
		}
		return nil
	}
}

func authorizedRun(m *manifest.Manifest, repo string, probe InstalledProbe, exe Executor) (*RunResult, error) {
	return Run(m, []string{"pi-plugin"}, plainArch(), RunOptions{
		RepoDir: repo, InstallPackages: true, WithConfig: true, ReplaceExisting: true, Probe: probe, Exec: exe,
	})
}

// Packages install before configuration is deployed, so an authorized run
// repairs the known conflict in the local file itself, after a safety backup,
// before the first installer reads it.
func TestRunAuthorizedRunRepairsLocalPiSettingsBeforeFirstInstaller(t *testing.T) {
	// The backup still carries the conflict: deployment must not reintroduce it.
	m, repo, target := piConflictFixture(t, conflictingPiSettings)
	if err := os.Chmod(target, 0600); err != nil {
		t.Fatal(err)
	}
	var seen []string
	res, err := authorizedRun(m, repo, noneInstalled, recordingExec(target, "", &seen))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"pi=" + repairedPiSettings, "pi-plugin=" + repairedPiSettings}
	if !reflect.DeepEqual(seen, want) {
		t.Errorf("installers saw %q", seen)
	}
	names := listDir(t, filepath.Dir(target))
	if len(names) != 2 || readOrEmpty(filepath.Join(filepath.Dir(target), names[1])) != conflictingPiSettings {
		t.Fatalf("dir = %q", names)
	}
	bk := filepath.Join(filepath.Dir(target), names[1])
	if info, _ := os.Stat(target); info.Mode().Perm() != 0600 || readOrEmpty(target) != repairedPiSettings {
		t.Errorf("final settings perm=%v", info.Mode().Perm())
	}
	pi := resultByName(t, res, "pi")
	prepared := target + ": " + piRepairNote + " before installing; safety backup: " + bk + " (copy it back to undo)"
	if pi.Prepared != prepared || pi.Preflight != "" || pi.Config != ConfigDeployed || len(pi.Files.LocalBackups) != 0 {
		t.Errorf("pi = %+v files=%+v", pi, pi.Files)
	}
	if p := res.Problems(); len(p) != 0 {
		t.Errorf("problems = %q", p)
	}
	if !strings.Contains(strings.Join(res.Summary(), "\n"), "\n      🩹 repaired before install: "+prepared+"\n") {
		t.Errorf("summary:\n%s", strings.Join(res.Summary(), "\n"))
	}

	seen = nil
	res, err = authorizedRun(m, repo, noneInstalled, recordingExec(target, "", &seen))
	if err != nil {
		t.Fatal(err)
	}
	if pi := resultByName(t, res, "pi"); pi.Prepared != "" || len(listDir(t, filepath.Dir(target))) != 2 || !reflect.DeepEqual(seen, want) {
		t.Errorf("rerun not idempotent: pi=%+v", pi)
	}
}

func TestRunKeepsPreInstallRepairAndBackupWhenInstallerFails(t *testing.T) {
	m, repo, target := piConflictFixture(t, repairedPiSettings)
	var seen []string
	res, err := authorizedRun(m, repo, noneInstalled, recordingExec(target, "pi", &seen))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(seen, []string{"pi=" + repairedPiSettings}) || readOrEmpty(target) != repairedPiSettings {
		t.Errorf("seen=%q", seen)
	}
	pi := resultByName(t, res, "pi")
	if !strings.HasPrefix(pi.Prepared, target+": "+piRepairNote+" before installing; safety backup: ") || len(listDir(t, filepath.Dir(target))) != 2 {
		t.Errorf("pi = %+v", pi)
	}
	want := []string{
		"pi: install failed: exit status 1",
		"pi: configuration incomplete: pi is failed",
		"pi-plugin: blocked: requires pi (failed)",
	}
	if got := res.Problems(); !reflect.DeepEqual(got, want) {
		t.Errorf("problems = %q", got)
	}
	if !strings.Contains(strings.Join(res.Summary(), "\n"), "      🩹 repaired before install: "+pi.Prepared) {
		t.Error("summary must report the repair and its backup")
	}
}

func TestRunPreInstallRepairFailureRunsNoInstaller(t *testing.T) {
	cases := []struct {
		name   string
		setup  func(t *testing.T, target string)
		reason string // prefix after "pre-install repair failed: "
	}{
		{"backup fails", func(t *testing.T, target string) {
			dir := filepath.Dir(target)
			if err := os.Chmod(dir, 0555); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.Chmod(dir, 0755) })
		}, "safety backup failed: "},
		{"write fails", func(t *testing.T, target string) {
			old := persistRepaired
			persistRepaired = func(string, []byte, os.FileMode) error { return errors.New("disk full") }
			t.Cleanup(func() { persistRepaired = old })
		}, "disk full; original kept, safety backup "},
	}
	for _, c := range cases {
		m, repo, target := piConflictFixture(t, repairedPiSettings)
		c.setup(t, target)
		var seen []string
		res, err := authorizedRun(m, repo, noneInstalled, recordingExec(target, "", &seen))
		if err != nil {
			t.Fatal(err)
		}
		pi := resultByName(t, res, "pi")
		prefix := piConflict(target) + "; pre-install repair failed: " + c.reason
		if len(seen) != 0 || pi.Install != OutcomeBlocked || !strings.HasPrefix(pi.Preflight, prefix) || pi.Prepared != "" {
			t.Errorf("%s: seen=%d pi=%+v", c.name, len(seen), pi)
		}
		if readOrEmpty(target) != conflictingPiSettings {
			t.Errorf("%s: local settings changed", c.name)
		}
	}
}

func TestRunRefusesPreInstallRepairThroughSymlinkEscapingHome(t *testing.T) {
	m, repo, target := piConflictFixture(t, repairedPiSettings)
	home := filepath.Dir(filepath.Dir(filepath.Dir(target)))
	outside := filepath.Join(filepath.Dir(home), "outside")
	mustWrite(t, filepath.Join(outside, "agent", "settings.json"), conflictingPiSettings)
	if err := os.RemoveAll(filepath.Join(home, ".pi")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(home, ".pi")); err != nil {
		t.Fatal(err)
	}
	var seen []string
	res, err := authorizedRun(m, repo, noneInstalled, recordingExec(target, "", &seen))
	if err != nil {
		t.Fatal(err)
	}
	pi := resultByName(t, res, "pi")
	if len(seen) != 0 || !strings.HasPrefix(pi.Preflight, piConflict(target)+"; pre-install repair failed: ") {
		t.Errorf("seen=%d pi=%+v", len(seen), pi)
	}
	if names := listDir(t, filepath.Join(outside, "agent")); len(names) != 1 || readOrEmpty(filepath.Join(outside, "agent", "settings.json")) != conflictingPiSettings {
		t.Errorf("outside home modified: %q", names)
	}
}

func TestRunAuthorizedRunRepairsInstalledPiBeforeOtherInstallers(t *testing.T) {
	m, repo, target := piConflictFixture(t, repairedPiSettings)
	installed := func(name string, _ manifest.AppConfig) bool { return name == "pi" }
	var seen []string
	res, err := authorizedRun(m, repo, installed, recordingExec(target, "", &seen))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(seen, []string{"pi-plugin=" + repairedPiSettings}) {
		t.Errorf("seen = %q", seen)
	}
	if pi := resultByName(t, res, "pi"); pi.Prepared == "" || pi.Preflight != "" || len(res.Problems()) != 0 {
		t.Errorf("pi = %+v problems=%q", pi, res.Problems())
	}
}

func TestRunReportsPreflightForInstalledPiWithoutBlockingOtherApps(t *testing.T) {
	m, repo, target := piConflictFixture(t, "")
	fx := &fakeExec{}
	installed := func(name string, _ manifest.AppConfig) bool { return name == "pi" }
	res, err := Run(m, []string{"pi-plugin"}, plainArch(), RunOptions{
		RepoDir: repo, InstallPackages: true, Probe: installed, Exec: fx.run,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := fx.apps(); got != "pi-plugin" {
		t.Errorf("executed %q", got)
	}
	if got, want := res.Problems(), []string{"pi: preflight: " + piPreflight(target)}; !reflect.DeepEqual(got, want) {
		t.Errorf("problems = %q", got)
	}
	if readOrEmpty(target) != conflictingPiSettings {
		t.Error("local settings were modified")
	}
}

func TestRunPreInstallRepairKeepsThreeNewestSafetyCopies(t *testing.T) {
	m, repo, target := piConflictFixture(t, repairedPiSettings)
	for _, s := range []string{".20200101_000000.bk", ".20200102_000000.bk", ".20200103_000000.bk"} {
		mustWrite(t, target+s, "old")
	}
	res, err := authorizedRun(m, repo, noneInstalled, (&fakeExec{}).run)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(target + ".20200101_000000.bk"); !os.IsNotExist(err) {
		t.Errorf("oldest copy kept: %q", listDir(t, filepath.Dir(target)))
	}
	if n := len(listDir(t, filepath.Dir(target))); n != 4 {
		t.Errorf("dir = %q", listDir(t, filepath.Dir(target)))
	}
	pi := resultByName(t, res, "pi")
	if !strings.HasSuffix(pi.Prepared, " (copy it back to undo); removed 1 older safety backup(s)") {
		t.Errorf("prepared = %q", pi.Prepared)
	}
}
