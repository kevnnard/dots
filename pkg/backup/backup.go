package backup

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/kevnnard/dots/pkg/homepath"
	"github.com/kevnnard/dots/pkg/manifest"
	"github.com/kevnnard/dots/pkg/packagelink"
)

type Result struct {
	AppName       string   `json:"app_name"`
	Copied        []string `json:"copied"`
	Skipped       []string `json:"skipped"`
	SecretsMasked []string `json:"secrets_masked"`
	Warnings      []string `json:"warnings"`
}

// RawSecretError reports a staging folder that already holds an unmasked
// entry where a secret's template-only backup belongs, typically left by a
// dots that copied the file as is. Restore prefers that entry to the
// template, so backup refuses the folder instead of removing anything in it.
type RawSecretError struct{ Path string }

func (e *RawSecretError) Error() string {
	return e.Path + " already holds an unmasked copy of a file that is now backed up as a template; nothing was removed or sent. Stage into a new empty folder: dots backup --out <new-folder>"
}

// checkNoRawSecret fails closed when anything, even a dangling symlink, sits
// at the unmasked destination of a secret.
func checkNoRawSecret(dest string) error {
	_, err := os.Lstat(dest)
	if err == nil {
		return &RawSecretError{Path: dest}
	}
	if os.IsNotExist(err) {
		return nil
	}
	return fmt.Errorf("cannot inspect %s: %w", dest, err)
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

		destFull := filepath.Join(destRepoDir, mapping.Dest)
		if stagePackageLink(srcExpanded, destFull, res) {
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

		if info.IsDir() {
			if err := copyDirectory(srcExpanded, destFull, app.Exclude, app.Secrets, res); err != nil {
				var raw *RawSecretError
				if errors.As(err, &raw) {
					return res, raw
				}
				res.Warnings = append(res.Warnings, fmt.Sprintf("failed copying dir %s: %v", mapping.Src, err))
			}
		} else {
			if isSecret(srcExpanded, app.Secrets) {
				if err := checkNoRawSecret(destFull); err != nil {
					return res, err
				}
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

// stagePackageLink stages a mapped source that is itself a link into an
// installed package as that link, never as the package's contents. It
// reports whether src was such a link; any other source is left to the copy.
func stagePackageLink(src, dest string, res *Result) bool {
	info, err := os.Lstat(src)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return false
	}
	target, err := os.Readlink(src)
	if err != nil {
		return false
	}
	if _, ok := packagelink.Parse(target); !ok {
		return false
	}
	if existing, err := os.Lstat(dest); err == nil && existing.IsDir() {
		res.Warnings = append(res.Warnings, fmt.Sprintf("%s is a directory in the staging folder; package link %s not staged", dest, src))
		return true
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		res.Warnings = append(res.Warnings, fmt.Sprintf("failed creating parent dir for symlink %s: %v", dest, err))
		return true
	}
	_ = os.Remove(dest)
	if err := os.Symlink(target, dest); err != nil {
		res.Warnings = append(res.Warnings, fmt.Sprintf("failed creating symlink %s: %v", dest, err))
		return true
	}
	res.Copied = append(res.Copied, dest)
	return true
}

// BackupAll iterates through all apps declared in the manifest and backs them up.
func BackupAll(m *manifest.Manifest, destRepoDir string) (*TotalResult, error) {
	total := &TotalResult{
		Results: make(map[string]*Result),
	}

	for name, app := range m.Apps {
		res, err := BackupApp(name, app, destRepoDir)
		if err != nil {
			// A partial stage must never be reported, staged or sent as a backup.
			return nil, err
		}
		total.Results[name] = res
		total.Total += len(res.Copied) + len(res.SecretsMasked)
	}

	return total, nil
}

// MissingDefaultPaths lists, as "app: src", the config paths the built-in
// inventory backs up for apps the manifest declares but does not map. A
// manifest written by an older dots is never rewritten, so backup reports
// them instead. Apps the manifest does not declare stay undeclared.
func MissingDefaultPaths(m *manifest.Manifest) []string {
	var missing []string
	for name, shipped := range manifest.DefaultManifest().Apps {
		app, ok := m.Apps[name]
		if !ok {
			continue
		}
		have := map[string]bool{}
		for _, p := range app.Paths {
			have[p.Src] = true
		}
		for _, p := range shipped.Paths {
			if !have[p.Src] {
				missing = append(missing, name+": "+p.Src)
			}
		}
	}
	sort.Strings(missing)
	return missing
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

		if info.Mode()&os.ModeSymlink != 0 {
			linkTarget, err := os.Readlink(path)
			if err != nil {
				res.Warnings = append(res.Warnings, fmt.Sprintf("cannot read symlink %s: %v", path, err))
				return nil
			}
			if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
				res.Warnings = append(res.Warnings, fmt.Sprintf("failed creating parent dir for symlink %s: %v", targetPath, err))
				return nil
			}
			_ = os.Remove(targetPath)
			if err := os.Symlink(linkTarget, targetPath); err != nil {
				res.Warnings = append(res.Warnings, fmt.Sprintf("failed creating symlink %s: %v", targetPath, err))
			} else {
				res.Copied = append(res.Copied, targetPath)
			}
			return nil
		}

		if info.IsDir() {
			return os.MkdirAll(targetPath, 0755)
		}

		if isSecret(rel, secrets) || isSecret(path, secrets) {
			if err := checkNoRawSecret(targetPath); err != nil {
				return err
			}
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

// copyFile copies a file into the dotfiles repository, replacing the user's
// home directory with a portable placeholder.
func copyFile(src, dest string) error {
	return homepath.CopyTokenized(src, dest)
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
	if err := os.MkdirAll(filepath.Dir(destTemplate), 0755); err != nil {
		return err
	}

	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}

	// If it's a JSON file, sanitize JSON structure preserving valid syntax
	if strings.HasSuffix(src, ".json") {
		var parsed any
		if err := json.Unmarshal(data, &parsed); err == nil {
			masked := maskJSONValues(parsed, "VALUE")
			marshaled, err := json.MarshalIndent(masked, "", "  ")
			if err == nil {
				return os.WriteFile(destTemplate, marshaled, 0644)
			}
		}
	}

	// Line-by-line fallback for .env / config files
	out, err := os.Create(destTemplate)
	if err != nil {
		return err
	}
	defer out.Close()

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	writer := bufio.NewWriter(out)

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") || trimmed == "" {
			writer.WriteString(line + "\n")
			continue
		}

		if strings.HasPrefix(trimmed, "HostName ") {
			target := strings.TrimSpace(strings.TrimPrefix(trimmed, "HostName "))
			if target == "github.com" || target == "gitlab.com" {
				writer.WriteString(line + "\n")
			} else {
				writer.WriteString("  HostName YOUR_SERVER_HOST_OR_IP\n")
			}
			continue
		}
		if strings.HasPrefix(trimmed, "User ") {
			u := strings.TrimSpace(strings.TrimPrefix(trimmed, "User "))
			if u == "git" {
				writer.WriteString(line + "\n")
			} else {
				writer.WriteString("  User YOUR_SSH_USER\n")
			}
			continue
		}
		if strings.HasPrefix(trimmed, "Host ") ||
			strings.HasPrefix(trimmed, "Port ") ||
			strings.HasPrefix(trimmed, "IdentityFile ") ||
			strings.HasPrefix(trimmed, "AddKeysToAgent ") ||
			strings.HasPrefix(trimmed, "UseKeychain ") ||
			strings.HasPrefix(trimmed, "ServerAliveInterval ") ||
			strings.HasPrefix(trimmed, "ServerAliveCountMax ") {
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

func maskJSONValues(val any, keyName string) any {
	switch v := val.(type) {
	case map[string]any:
		res := make(map[string]any)
		for k, child := range v {
			res[k] = maskJSONValues(child, k)
		}
		return res
	case []any:
		var res []any
		for _, child := range v {
			res = append(res, maskJSONValues(child, keyName))
		}
		return res
	case string:
		lowerKey := strings.ToLower(keyName)
		isSecret := strings.Contains(lowerKey, "key") ||
			strings.Contains(lowerKey, "token") ||
			strings.Contains(lowerKey, "secret") ||
			strings.Contains(lowerKey, "auth") ||
			strings.Contains(lowerKey, "pass") ||
			strings.Contains(lowerKey, "access") ||
			strings.Contains(lowerKey, "refresh") ||
			strings.Contains(lowerKey, "connection_string") ||
			strings.HasPrefix(v, "Bearer ") ||
			strings.HasPrefix(v, "lin_api_") ||
			strings.HasPrefix(v, "sk-") ||
			strings.HasPrefix(v, "AQ.") ||
			strings.HasPrefix(v, "postgresql://") ||
			strings.HasPrefix(v, "postgres://") ||
			strings.HasPrefix(v, "ghp_") ||
			strings.HasPrefix(v, "gho_")

		if isSecret {
			cleanKey := strings.ToUpper(strings.ReplaceAll(keyName, "-", "_"))
			return fmt.Sprintf("YOUR_%s_HERE", cleanKey)
		}
		return v
	default:
		return v
	}
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
