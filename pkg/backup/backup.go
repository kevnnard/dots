package backup

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/kevnnard/dots/pkg/manifest"
)

type Result struct {
	AppName       string   `json:"app_name"`
	Copied        []string `json:"copied"`
	Skipped       []string `json:"skipped"`
	SecretsMasked []string `json:"secrets_masked"`
	Warnings      []string `json:"warnings"`
}

type TotalResult struct {
	Results  map[string]*Result `json:"results"`
	Total    int                `json:"total_copied"`
	Warnings []string           `json:"warnings"`
}

// BackupApp backs up the files and directories for a single app config.
func BackupApp(appName string, app manifest.AppConfig, destRepoDir string) (*Result, error) {
	res := &Result{
		AppName: appName,
	}

	// Special case: Homebrew Brewfile dump
	if appName == "brew" && runtime.GOOS == "darwin" {
		if err := dumpBrewfile(destRepoDir); err != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("brew bundle dump failed: %v", err))
		} else {
			res.Copied = append(res.Copied, "Brewfile")
		}
		return res, nil
	}

	for _, mapping := range app.Paths {
		srcExpanded, err := manifest.ExpandPath(mapping.Src)
		if err != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("cannot expand source path %s: %v", mapping.Src, err))
			continue
		}

		info, err := os.Stat(srcExpanded)
		if err != nil {
			if os.IsNotExist(err) {
				res.Skipped = append(res.Skipped, fmt.Sprintf("%s (does not exist)", mapping.Src))
				continue
			}
			res.Warnings = append(res.Warnings, fmt.Sprintf("cannot stat %s: %v", mapping.Src, err))
			continue
		}

		destFull := filepath.Join(destRepoDir, mapping.Dest)

		if info.IsDir() {
			if err := copyDirectory(srcExpanded, destFull, app.Exclude, app.Secrets, res); err != nil {
				res.Warnings = append(res.Warnings, fmt.Sprintf("failed copying dir %s: %v", mapping.Src, err))
			}
		} else {
			if isSecret(srcExpanded, app.Secrets) {
				templateDest := destFull + ".template"
				if strings.HasSuffix(destFull, ".env") {
					templateDest = destFull + ".template"
				}
				if err := sanitizeSecretFile(srcExpanded, templateDest); err != nil {
					res.Warnings = append(res.Warnings, fmt.Sprintf("failed sanitizing secret %s: %v", srcExpanded, err))
				} else {
					res.SecretsMasked = append(res.SecretsMasked, templateDest)
				}
			} else {
				if err := copyFile(srcExpanded, destFull); err != nil {
					res.Warnings = append(res.Warnings, fmt.Sprintf("failed copying file %s: %v", srcExpanded, err))
				} else {
					res.Copied = append(res.Copied, destFull)
				}
			}
		}
	}

	return res, nil
}

// BackupAll iterates through all apps declared in the manifest and backs them up.
func BackupAll(m *manifest.Manifest, destRepoDir string) (*TotalResult, error) {
	total := &TotalResult{
		Results: make(map[string]*Result),
	}

	for name, app := range m.Apps {
		res, err := BackupApp(name, app, destRepoDir)
		if err != nil {
			total.Warnings = append(total.Warnings, fmt.Sprintf("error backing up %s: %v", name, err))
		}
		total.Results[name] = res
		total.Total += len(res.Copied) + len(res.SecretsMasked)
	}

	return total, nil
}

func copyDirectory(srcDir, destDir string, excludes, secrets []string, res *Result) error {
	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}

		if isExcluded(rel, excludes) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		targetPath := filepath.Join(destDir, rel)

		if info.IsDir() {
			return os.MkdirAll(targetPath, 0755)
		}

		if isSecret(rel, secrets) || isSecret(path, secrets) {
			templateDest := targetPath + ".template"
			if err := sanitizeSecretFile(path, templateDest); err != nil {
				res.Warnings = append(res.Warnings, fmt.Sprintf("failed sanitizing secret %s: %v", path, err))
			} else {
				res.SecretsMasked = append(res.SecretsMasked, templateDest)
			}
			return nil
		}

		if err := copyFile(path, targetPath); err != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("failed copying %s: %v", path, err))
			return nil
		}

		res.Copied = append(res.Copied, targetPath)
		return nil
	})
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

func isExcluded(relPath string, patterns []string) bool {
	norm := filepath.ToSlash(relPath)
	for _, p := range patterns {
		pNorm := filepath.ToSlash(p)

		// Exact match or folder match
		if norm == pNorm {
			return true
		}

		// Prefix wildcard match e.g. ".git/**"
		if strings.HasSuffix(pNorm, "/**") {
			prefix := strings.TrimSuffix(pNorm, "/**")
			if norm == prefix || strings.HasPrefix(norm, prefix+"/") {
				return true
			}
		}

		// Simple filename match
		if filepath.Base(norm) == pNorm {
			return true
		}

		if matched, _ := filepath.Match(pNorm, filepath.Base(norm)); matched {
			return true
		}
	}
	return false
}

func isSecret(path string, secretList []string) bool {
	base := filepath.Base(path)
	if strings.HasPrefix(base, ".env") || base == ".env" {
		return true
	}
	if strings.HasSuffix(base, ".key") || strings.HasSuffix(base, ".pem") {
		return true
	}
	if base == "id_rsa" || base == "id_ed25519" {
		return true
	}
	for _, s := range secretList {
		if strings.Contains(path, s) {
			return true
		}
	}
	return false
}

func sanitizeSecretFile(src, destTemplate string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(destTemplate), 0755); err != nil {
		return err
	}

	out, err := os.Create(destTemplate)
	if err != nil {
		return err
	}
	defer out.Close()

	scanner := bufio.NewScanner(in)
	writer := bufio.NewWriter(out)

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") || trimmed == "" {
			writer.WriteString(line + "\n")
			continue
		}

		if strings.Contains(line, "=") {
			parts := strings.SplitN(line, "=", 2)
			key := parts[0]
			// Write key with placeholder value
			writer.WriteString(fmt.Sprintf("%s=your_%s_here\n", key, strings.ToLower(strings.TrimSpace(key))))
		} else {
			writer.WriteString("# redacted: " + trimmed + "\n")
		}
	}

	if err := scanner.Err(); err != nil {
		return err
	}

	return writer.Flush()
}

func dumpBrewfile(destDir string) error {
	dest := filepath.Join(destDir, "Brewfile")
	cmd := exec.Command("brew", "bundle", "dump", "--file="+dest, "--force")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w", string(output), err)
	}
	return nil
}
