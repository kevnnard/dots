package manifest

import "sort"

// Effective returns the inventory a restore plans with, built from the local
// manifest. Apps the local manifest does not declare stay undeclared, so a
// removed app does not come back. For apps it declares, every install recipe
// the user wrote is kept as-is, and recipes only the shipped default defines
// are added (zed on pacman, for instance), so a manifest copied by an older
// dots still gets them. extended lists the apps that gained a recipe, sorted,
// so callers can report them.
func Effective(local *Manifest) (*Manifest, []string) {
	shipped := DefaultManifest()
	var extended []string
	for name, app := range local.Apps {
		shippedApp, ok := shipped.Apps[name]
		if !ok {
			continue
		}
		added := false
		if app.Install == nil {
			app.Install = map[string]InstallRecipe{}
		}
		for key, recipe := range shippedApp.Install {
			if _, has := app.Install[key]; !has {
				app.Install[key] = recipe
				added = true
			}
		}
		shipped.Apps[name] = app
		if added {
			extended = append(extended, name)
		}
	}
	sort.Strings(extended)
	out := &Manifest{Version: local.Version, DotfilesDir: local.DotfilesDir, Apps: map[string]AppConfig{}}
	for name, app := range local.Apps {
		out.Apps[name] = app
	}
	for _, name := range extended {
		out.Apps[name] = shipped.Apps[name]
	}
	return out, extended
}
