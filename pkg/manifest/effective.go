package manifest

import "sort"

// Effective returns the inventory a restore plans with, built from the local
// manifest. Apps the local manifest does not declare stay undeclared, so a
// removed app does not come back. For apps it declares, every install recipe
// the user wrote is kept as-is, and recipes only the shipped default defines
// are added (zed on pacman, for instance), so a manifest copied by an older
// dots still gets them. extended lists the apps that gained a recipe, sorted,
// so callers can report them. Mappings the shipped default marks keep_local
// are kept local here too, whatever the local manifest says.
func Effective(local *Manifest) (*Manifest, []string) {
	shipped := DefaultManifest()
	shippedPaths := map[string][]PathMapping{}
	for name, app := range shipped.Apps {
		shippedPaths[name] = app.Paths
	}
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
	for name, app := range out.Apps {
		if defaults, ok := shippedPaths[name]; ok {
			app.Paths = withShippedKeepLocal(app.Paths, defaults)
			out.Apps[name] = app
		}
	}
	return out, extended
}

// withShippedKeepLocal returns paths with keep_local set on every mapping the
// shipped inventory marks machine-specific, so a manifest written before the
// flag existed still keeps those files. It returns a copy; paths is unchanged.
func withShippedKeepLocal(paths, shipped []PathMapping) []PathMapping {
	keep := map[string]bool{}
	for _, p := range shipped {
		if p.KeepLocal {
			keep[p.Src] = true
		}
	}
	out := append([]PathMapping(nil), paths...)
	for i := range out {
		if keep[out[i].Src] {
			out[i].KeepLocal = true
		}
	}
	return out
}
