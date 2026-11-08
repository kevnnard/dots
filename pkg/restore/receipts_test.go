package restore

import (
	"testing"

	"github.com/kevnnard/dots/pkg/manifest"
)

func receiptManifest() *manifest.Manifest {
	return &manifest.Manifest{Apps: map[string]manifest.AppConfig{
		"bitwarden": {Check: []string{"/Applications/Bitwarden.app"}, Install: map[string]manifest.InstallRecipe{
			"darwin": {Manager: "brew", Cask: "bitwarden"},
			"pacman": {Manager: "pacman", Package: "bitwarden"},
		}},
		"font-x": {Check: []string{"~/Library/Fonts/X.ttf"}, Install: map[string]manifest.InstallRecipe{
			"pacman": {Manager: "pacman", Package: "ttf-x"},
		}},
	}}
}

func TestPlanTreatsPackageInstalledByItsManagerAsInstalled(t *testing.T) {
	var asked []manifest.InstallRecipe
	receipts := func(r manifest.InstallRecipe) bool {
		asked = append(asked, r)
		return r.Package == "bitwarden"
	}
	steps, err := BuildPlan(receiptManifest(), []string{"bitwarden", "font-x"}, plainArch(), PlanOptions{
		Probe:    func(string, manifest.AppConfig) bool { return false },
		Receipts: receipts,
	})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]PlanStatus{}
	for _, s := range steps {
		got[s.AppName] = s.Status
	}
	if got["bitwarden"] != StatusInstalled || got["font-x"] != StatusInstall {
		t.Errorf("statuses = %v", got)
	}
	if len(asked) != 2 || asked[0].Manager != "pacman" || asked[1].Package != "ttf-x" {
		t.Errorf("only the selected recipe is asked, got %+v", asked)
	}
}

func TestPlanSkipsReceiptsWhenCommandProbeFindsApp(t *testing.T) {
	steps, err := BuildPlan(receiptManifest(), []string{"bitwarden"}, plainArch(), PlanOptions{
		Probe:    func(string, manifest.AppConfig) bool { return true },
		Receipts: func(manifest.InstallRecipe) bool { t.Error("receipts must not be asked"); return false },
	})
	if err != nil || steps[0].Status != StatusInstalled {
		t.Fatalf("%v %+v", err, steps)
	}
}
