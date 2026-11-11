package manifest

import (
	"fmt"
	"sort"
)

// ValidOS lists the systems an os list or src_os key may name (GOOS values).
var ValidOS = []string{"darwin", "linux", "windows"}

func validOS(goos string) bool {
	for _, v := range ValidOS {
		if v == goos {
			return true
		}
	}
	return false
}

func listed(list []string, goos string) bool {
	if len(list) == 0 {
		return true
	}
	for _, v := range list {
		if v == goos {
			return true
		}
	}
	return false
}

// AppliesTo reports whether the app belongs on the system goos. An app
// without an os list applies everywhere.
func (a AppConfig) AppliesTo(goos string) bool { return listed(a.OS, goos) }

// AppliesTo reports whether the mapping belongs on the system goos. A
// mapping without an os list applies everywhere.
func (p PathMapping) AppliesTo(goos string) bool { return listed(p.OS, goos) }

// SrcFor returns the local source of the mapping on the system goos: its
// src_os entry when present, otherwise Src.
func (p PathMapping) SrcFor(goos string) string {
	if src, ok := p.SrcOS[goos]; ok && src != "" {
		return src
	}
	return p.Src
}

// ForOS returns a copy of the app holding only the mappings that apply to
// goos, each with Src set to its effective source there. Dest is unchanged.
func (a AppConfig) ForOS(goos string) AppConfig {
	paths := make([]PathMapping, 0, len(a.Paths))
	for _, p := range a.Paths {
		if p.AppliesTo(goos) {
			p.Src = p.SrcFor(goos)
			paths = append(paths, p)
		}
	}
	a.Paths = paths
	return a
}

// Validate rejects os lists and src_os keys naming an unknown system.
func (m *Manifest) Validate() error {
	names := make([]string, 0, len(m.Apps))
	for name := range m.Apps {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		app := m.Apps[name]
		if err := checkOS(app.OS); err != nil {
			return fmt.Errorf("app %q: os: %w", name, err)
		}
		for i, p := range app.Paths {
			if err := checkOS(p.OS); err != nil {
				return fmt.Errorf("app %q: path #%d (%s): os: %w", name, i+1, p.Dest, err)
			}
			keys := make([]string, 0, len(p.SrcOS))
			for k := range p.SrcOS {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			if err := checkOS(keys); err != nil {
				return fmt.Errorf("app %q: path #%d (%s): src_os: %w", name, i+1, p.Dest, err)
			}
		}
	}
	return nil
}

func checkOS(list []string) error {
	for _, v := range list {
		if !validOS(v) {
			return fmt.Errorf("unknown system %q (valid: darwin, linux, windows)", v)
		}
	}
	return nil
}
