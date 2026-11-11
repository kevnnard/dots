package restore

import (
	"strings"
	"testing"

	"github.com/kevnnard/dots/pkg/manifest"
)

// A successful real run ends with counts of what was installed, what was
// already present, and what config was deployed, so the user sees the whole
// outcome in one line.
func TestSummaryReportsSuccessCounts(t *testing.T) {
	fx := &fakeExec{}
	probe := func(name string, _ manifest.AppConfig) bool { return name == "node" }
	res, err := Run(npmToolManifest(), []string{"node", "codex"}, plainArch(), RunOptions{
		InstallPackages: true, Probe: probe, Exec: fx.run,
	})
	if err != nil {
		t.Fatal(err)
	}
	out := strings.Join(res.Summary(), "\n")
	want := "📊 Installed: 1 · Already present: 1 · Configured: 0"
	if !strings.Contains(out, want) {
		t.Errorf("success summary missing %q:\n%s", want, out)
	}
}
