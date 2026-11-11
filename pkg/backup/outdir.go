package backup

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kevnnard/dots/pkg/manifest"
)

// DefaultOutDir is where a backup is staged when no --out is given. It sits
// outside the dotfiles repository on purpose: backups hold personal configs,
// and the repository is public, so it is only for the code.
func DefaultOutDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cache", "dots", "backup"), nil
}

// CheckOutsideRepo refuses an output directory that is the repository or
// inside it, so a backup can never land in a tracked path.
func CheckOutsideRepo(repoDir, outDir string) error {
	absRepo, err := filepath.Abs(repoDir)
	if err != nil {
		return err
	}
	absOut, err := filepath.Abs(outDir)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(absRepo, absOut)
	if err != nil {
		return err
	}
	climbsOut := rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))
	inside := rel == "." || (!climbsOut && !filepath.IsAbs(rel))
	if inside {
		return fmt.Errorf("backup output %s is inside the dotfiles repository %s; use a directory outside it", absOut, absRepo)
	}
	return nil
}

// StageManifest copies the repository manifest next to the staged configs, so
// the staged directory is a complete input for `dots -d <dir> restore`.
func StageManifest(repoDir, outDir string) error {
	return StageManifestApps(repoDir, outDir, nil)
}

func StageManifestApps(repoDir, outDir string, apps map[string]manifest.AppConfig) error {
	data, err := os.ReadFile(filepath.Join(repoDir, "manifest.yaml"))
	if err != nil {
		return fmt.Errorf("reading manifest: %w", err)
	}
	if len(apps) > 0 {
		if data, err = manifest.WithApps(data, apps); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(outDir, "manifest.yaml"), data, 0644)
}

func BackupDiscovered(configs map[string]manifest.AppConfig, outDir string) ([]*Result, error) {
	names := make([]string, 0, len(configs))
	for name := range configs {
		names = append(names, name)
	}
	sort.Strings(names)
	var results []*Result
	for _, name := range names {
		res, err := BackupApp(name, configs[name], outDir)
		if err != nil {
			return nil, err
		}
		results = append(results, res)
	}
	return results, nil
}
