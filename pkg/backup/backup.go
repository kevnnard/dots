package backup

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

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

type GitSnapshotResult struct {
	Committed  bool   `json:"committed"`
	CommitHash string `json:"commit_hash,omitempty"`
	Message    string `json:"message,omitempty"`
	Pushed     bool   `json:"pushed"`
	Error      string `json:"error,omitempty"`
}

// CreateGitSnapshot stages all changes in repoDir, creates a snapshot commit, and optionally pushes to remote.
func CreateGitSnapshot(repoDir string, pushRemote bool) (*GitSnapshotResult, error) {
	gitDir := filepath.Join(repoDir, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		return &GitSnapshotResult{Committed: false}, nil
	}

	// Check if there are changes
	statusCmd := exec.Command("git", "status", "--porcelain")
	statusCmd.Dir = repoDir
	statusOut, err := statusCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("checking git status: %w", err)
	}

	if len(strings.TrimSpace(string(statusOut))) == 0 {
		return &GitSnapshotResult{Committed: false}, nil
	}

	// Stage all changes
	addCmd := exec.Command("git", "add", "-A")
	addCmd.Dir = repoDir
	if err := addCmd.Run(); err != nil {
		return nil, fmt.Errorf("git add: %w", err)
	}

	// Prepare commit message
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "machine"
	}
	ts := time.Now().Format("2006-01-02 15:04:05")
	msg := fmt.Sprintf("snapshot: %s (%s)", ts, hostname)

	commitCmd := exec.Command("git", "commit", "-m", msg)
	commitCmd.Dir = repoDir
	if out, err := commitCmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("git commit failed: %s: %w", string(out), err)
	}

	// Get commit hash
	revCmd := exec.Command("git", "rev-parse", "--short", "HEAD")
	revCmd.Dir = repoDir
	revOut, _ := revCmd.Output()
	commitHash := strings.TrimSpace(string(revOut))

	res := &GitSnapshotResult{
		Committed:  true,
		CommitHash: commitHash,
		Message:    msg,
	}

	if pushRemote {
		// Check if remote 'origin' exists
		remoteCmd := exec.Command("git", "remote", "get-url", "origin")
		remoteCmd.Dir = repoDir
		if err := remoteCmd.Run(); err == nil {
			pushCmd := exec.Command("git", "push", "origin", "HEAD")
			pushCmd.Dir = repoDir
			if err := pushCmd.Run(); err == nil {
				res.Pushed = true
			} else {
				res.Error = fmt.Sprintf("git push failed: %v", err)
			}
		}
	}

	return res, nil
}
