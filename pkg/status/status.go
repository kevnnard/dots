package status

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kevnnard/dots/pkg/detector"
	"github.com/kevnnard/dots/pkg/manifest"
)

type FileState string

const (
	StateSynced       FileState = "synced"
	StateModified     FileState = "modified"
	StateMissingLocal FileState = "missing_local"
	StateMissingRepo  FileState = "missing_repo"
	StateMissingBoth  FileState = "missing_both"
)

type PathStatus struct {
	Src   string    `json:"src"`
	Dest  string    `json:"dest"`
	State FileState `json:"state"`
}

type AppStatus struct {
	AppName     string       `json:"app_name"`
	Installed   bool         `json:"installed"`
	Paths       []PathStatus `json:"paths"`
	HasDrift    bool         `json:"has_drift"`
}

type OverallStatus struct {
	Apps map[string]*AppStatus `json:"apps"`
}

// CheckAppStatus checks the synchronization state of an app's paths.
func CheckAppStatus(appName string, app manifest.AppConfig, repoDir string) *AppStatus {
	st := &AppStatus{
		AppName:   appName,
		Installed: detector.IsInstalled(appName),
	}

	for _, p := range app.Paths {
		srcExpanded, err := manifest.ExpandPath(p.Src)
		if err != nil {
			st.Paths = append(st.Paths, PathStatus{
				Src:   p.Src,
				Dest:  p.Dest,
				State: StateMissingLocal,
			})
			st.HasDrift = true
			continue
		}

		destFull := filepath.Join(repoDir, p.Dest)
		srcExists := fileOrDirExists(srcExpanded)
		repoExists := fileOrDirExists(destFull)

		var state FileState
		if !srcExists && !repoExists {
			// Check if template exists
			if fileOrDirExists(destFull + ".template") {
				state = StateMissingLocal
				st.HasDrift = true
			} else {
				state = StateMissingBoth
			}
		} else if !srcExists && repoExists {
			state = StateMissingLocal
			st.HasDrift = true
		} else if srcExists && !repoExists {
			state = StateMissingRepo
			st.HasDrift = true
		} else {
			// Both exist: check content match
			if areContentsIdentical(srcExpanded, destFull) {
				state = StateSynced
			} else {
				state = StateModified
				st.HasDrift = true
			}
		}

		st.Paths = append(st.Paths, PathStatus{
			Src:   p.Src,
			Dest:  p.Dest,
			State: state,
		})
	}

	return st
}

// CheckAll gathers status across all configured apps in the manifest.
func CheckAll(m *manifest.Manifest, repoDir string) *OverallStatus {
	overall := &OverallStatus{
		Apps: make(map[string]*AppStatus),
	}
	for name, app := range m.Apps {
		overall.Apps[name] = CheckAppStatus(name, app, repoDir)
	}
	return overall
}

// AddAppPath registers a new path mapping into an existing or newly created app config.
func AddAppPath(m *manifest.Manifest, appName, srcPath, destPath string) error {
	if m.Apps == nil {
		m.Apps = make(map[string]manifest.AppConfig)
	}

	if destPath == "" {
		// Automatically infer a clean destination inside configs/<appName>/...
		base := filepath.Base(srcPath)
		destPath = fmt.Sprintf("configs/%s/%s", appName, base)
	}

	app, exists := m.Apps[appName]
	if !exists {
		app = manifest.AppConfig{
			Description: fmt.Sprintf("Configuration for %s", appName),
			Paths:       []manifest.PathMapping{},
			Install:     make(map[string]manifest.InstallRecipe),
		}
	}

	// Avoid duplicates
	for _, p := range app.Paths {
		if p.Src == srcPath {
			return nil // already tracked
		}
	}

	app.Paths = append(app.Paths, manifest.PathMapping{
		Src:  srcPath,
		Dest: destPath,
	})
	m.Apps[appName] = app

	return nil
}

func fileOrDirExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func areContentsIdentical(path1, path2 string) bool {
	info1, err1 := os.Stat(path1)
	info2, err2 := os.Stat(path2)
	if err1 != nil || err2 != nil {
		return false
	}

	if info1.IsDir() != info2.IsDir() {
		return false
	}

	if !info1.IsDir() {
		if info1.Size() != info2.Size() {
			return false
		}
		data1, err1 := os.ReadFile(path1)
		data2, err2 := os.ReadFile(path2)
		if err1 != nil || err2 != nil {
			return false
		}
		return bytes.Equal(data1, data2)
	}

	// For directories, do a shallow or deep check
	var identical = true
	_ = filepath.Walk(path1, func(p string, info os.FileInfo, err error) error {
		if err != nil || !identical {
			return nil
		}
		rel, _ := filepath.Rel(path1, p)
		if rel == "." || strings.HasPrefix(rel, ".git") {
			return nil
		}
		p2 := filepath.Join(path2, rel)
		i2, err := os.Stat(p2)
		if err != nil || i2.IsDir() != info.IsDir() {
			identical = false
			return nil
		}
		if !info.IsDir() {
			if info.Size() != i2.Size() {
				identical = false
				return nil
			}
			b1, _ := os.ReadFile(p)
			b2, _ := os.ReadFile(p2)
			if !bytes.Equal(b1, b2) {
				identical = false
			}
		}
		return nil
	})

	return identical
}
