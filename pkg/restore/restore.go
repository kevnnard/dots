package restore

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/kevnnard/dots/pkg/detector"
	"github.com/kevnnard/dots/pkg/manifest"
)

type AppPlan struct {
	AppName     string `json:"app_name"`
	Description string `json:"description"`
	InstallCmd  string `json:"install_cmd,omitempty"`
	NeedInstall bool   `json:"need_install"`
	RecipeFound bool   `json:"recipe_found"`
}

type ApplyResult struct {
	AppName       string   `json:"app_name"`
	Restored      []string `json:"restored"`
	LocalBackups  []string `json:"local_backups"`
	TemplatesInit []string `json:"templates_initialized"`
	Skipped       []string `json:"skipped"`
	Warnings      []string `json:"warnings"`
}

// PlanApp generates an installation plan for a single application based on current system.
func PlanApp(appName string, app manifest.AppConfig, sys *detector.SystemInfo) *AppPlan {
	plan := &AppPlan{
		AppName:     appName,
		Description: app.Description,
	}

	recipe, found := findBestRecipe(app.Install, sys)
	if !found {
		plan.RecipeFound = false
		return plan
	}

	plan.RecipeFound = true
	plan.InstallCmd = buildInstallCommand(recipe, sys)
	return plan
}

// PlanAll builds installation plans for all apps in the manifest.
func PlanAll(m *manifest.Manifest, sys *detector.SystemInfo) []*AppPlan {
	var plans []*AppPlan
	for name, app := range m.Apps {
		plans = append(plans, PlanApp(name, app, sys))
	}
	return plans
}

// ApplyAppFiles copies configuration files from the dotfiles repository to the destination.
func ApplyAppFiles(appName string, app manifest.AppConfig, repoDir string, dryRun bool) (*ApplyResult, error) {
	res := &ApplyResult{
		AppName: appName,
	}

	for _, mapping := range app.Paths {
		repoPath := filepath.Join(repoDir, mapping.Dest)
		targetExpanded, err := manifest.ExpandPath(mapping.Src)
		if err != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("cannot expand target path %s: %v", mapping.Src, err))
			continue
		}

		info, err := os.Stat(repoPath)
		if err != nil {
			// Check if a .template exists instead
			templatePath := repoPath + ".template"
			if tInfo, tErr := os.Stat(templatePath); tErr == nil && !tInfo.IsDir() {
				if _, targetErr := os.Stat(targetExpanded); os.IsNotExist(targetErr) {
					if !dryRun {
						if err := copyFile(templatePath, targetExpanded); err != nil {
							res.Warnings = append(res.Warnings, fmt.Sprintf("failed deploying template to %s: %v", targetExpanded, err))
						} else {
							res.TemplatesInit = append(res.TemplatesInit, targetExpanded)
						}
					} else {
						res.TemplatesInit = append(res.TemplatesInit, targetExpanded)
					}
					continue
				}
			}

			res.Skipped = append(res.Skipped, fmt.Sprintf("%s (not found in dotfiles repo)", mapping.Dest))
			continue
		}

		if info.IsDir() {
			err := filepath.Walk(repoPath, func(path string, fInfo os.FileInfo, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				rel, err := filepath.Rel(repoPath, path)
				if err != nil || rel == "." {
					return nil
				}

				destFile := filepath.Join(targetExpanded, rel)

				if fInfo.Mode()&os.ModeSymlink != 0 {
					if !dryRun {
						linkTarget, err := os.Readlink(path)
						if err != nil {
							res.Warnings = append(res.Warnings, fmt.Sprintf("cannot read symlink %s: %v", path, err))
							return nil
						}
						_ = os.Remove(destFile)
						if err := os.Symlink(linkTarget, destFile); err != nil {
							res.Warnings = append(res.Warnings, fmt.Sprintf("failed creating symlink %s: %v", destFile, err))
							return nil
						}
					}
					res.Restored = append(res.Restored, destFile)
					return nil
				}

				if fInfo.IsDir() {
					if !dryRun {
						return os.MkdirAll(destFile, 0755)
					}
					return nil
				}

				if strings.HasSuffix(rel, ".template") {
					realDest := strings.TrimSuffix(destFile, ".template")
					if _, err := os.Stat(realDest); os.IsNotExist(err) {
						if !dryRun {
							if err := copyFile(path, realDest); err != nil {
								res.Warnings = append(res.Warnings, fmt.Sprintf("failed deploying template to %s: %v", realDest, err))
							} else {
								res.TemplatesInit = append(res.TemplatesInit, realDest)
							}
						} else {
							res.TemplatesInit = append(res.TemplatesInit, realDest)
						}
					}
					return nil
				}

				if !dryRun {
					if bk, err := makeSafetyBackup(destFile, path); err == nil && bk != "" {
						res.LocalBackups = append(res.LocalBackups, bk)
					}
					if err := copyFile(path, destFile); err != nil {
						res.Warnings = append(res.Warnings, fmt.Sprintf("failed copying %s to %s: %v", path, destFile, err))
						return nil
					}
				}
				res.Restored = append(res.Restored, destFile)
				return nil
			})
			if err != nil {
				res.Warnings = append(res.Warnings, fmt.Sprintf("error restoring dir %s: %v", mapping.Dest, err))
			}
		} else {
			if !dryRun {
				if bk, err := makeSafetyBackup(targetExpanded, repoPath); err == nil && bk != "" {
					res.LocalBackups = append(res.LocalBackups, bk)
				}
				if err := copyFile(repoPath, targetExpanded); err != nil {
					res.Warnings = append(res.Warnings, fmt.Sprintf("failed restoring %s to %s: %v", repoPath, targetExpanded, err))
					continue
				}
			}
			res.Restored = append(res.Restored, targetExpanded)
		}
	}

	return res, nil
}

// ExecuteInstallCommand executes an installation command through shell.
func ExecuteInstallCommand(cmdStr string) error {
	cmd := exec.Command("sh", "-c", cmdStr)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func findBestRecipe(recipes map[string]manifest.InstallRecipe, sys *detector.SystemInfo) (manifest.InstallRecipe, bool) {
	if len(recipes) == 0 {
		return manifest.InstallRecipe{}, false
	}

	// 1. Try exact distro match (e.g. "ubuntu", "arch", "debian")
	if sys.Distro != "" && sys.Distro != detector.DistroUnknown {
		if r, ok := recipes[string(sys.Distro)]; ok {
			return r, true
		}
	}

	// 2. Try OS match (e.g. "darwin", "linux")
	if r, ok := recipes[sys.OS]; ok {
		return r, true
	}

	return manifest.InstallRecipe{}, false
}

func buildInstallCommand(recipe manifest.InstallRecipe, sys *detector.SystemInfo) string {
	if recipe.Command != "" {
		return recipe.Command
	}

	switch recipe.Manager {
	case "brew":
		if recipe.Cask != "" {
			return fmt.Sprintf("brew install --cask %s", recipe.Cask)
		}
		if recipe.Package != "" {
			return fmt.Sprintf("brew install %s", recipe.Package)
		}
	case "apt":
		if recipe.Package != "" {
			return fmt.Sprintf("sudo apt-get install -y %s", recipe.Package)
		}
	case "pacman":
		if recipe.Package != "" {
			return fmt.Sprintf("sudo pacman -S --noconfirm %s", recipe.Package)
		}
	case "yay":
		if recipe.Package != "" {
			return fmt.Sprintf("yay -S --noconfirm %s", recipe.Package)
		}
	case "dnf":
		if recipe.Package != "" {
			return fmt.Sprintf("sudo dnf install -y %s", recipe.Package)
		}
	case "pnpm":
		if recipe.Package != "" {
			return fmt.Sprintf("pnpm add -g %s", recipe.Package)
		}
	case "npm":
		if recipe.Package != "" {
			// Prefer pnpm if available on the system
			if sys != nil && sys.HasManager(detector.ManagerPnpm) {
				return fmt.Sprintf("pnpm add -g %s", recipe.Package)
			}
			return fmt.Sprintf("npm install -g %s", recipe.Package)
		}
	}

	return ""
}

func copyFile(src, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return err
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

func makeSafetyBackup(targetPath, repoSourcePath string) (string, error) {
	info, err := os.Stat(targetPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	if info.IsDir() {
		return "", nil
	}

	// If contents are already identical, no backup is needed
	targetBytes, err := os.ReadFile(targetPath)
	if err == nil {
		sourceBytes, err := os.ReadFile(repoSourcePath)
		if err == nil && bytes.Equal(targetBytes, sourceBytes) {
			return "", nil
		}
	}

	ts := time.Now().Format("20060102_150405")
	backupPath := fmt.Sprintf("%s.%s.bk", targetPath, ts)
	if err := copyFile(targetPath, backupPath); err != nil {
		return "", err
	}
	return backupPath, nil
}
