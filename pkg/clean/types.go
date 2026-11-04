// Package clean plans the removal of declared apps. Planning is read-only:
// it never queries package managers itself, never runs commands and never
// touches files. Ownership evidence comes from an injected OwnershipProbe.
package clean

import (
	"context"

	"github.com/kevnnard/dots/pkg/manifest"
)

// Owner is native package-manager evidence that one package record owns an
// app (for example `brew list --cask` or `pacman -Qq`). Command presence and
// install recipes are never ownership.
type Owner struct {
	Manager string `json:"manager"` // brew, pacman, npm, pnpm, ...
	Package string `json:"package,omitempty"`
	Cask    string `json:"cask,omitempty"`
	// Prefix is the absolute npm global prefix holding the package (the
	// configured default or ~/.local); NativeProbe always sets it for npm.
	Prefix string `json:"prefix,omitempty"`
}

// Evidence is what a probe observed for one app. Present reports any presence
// signal (command on PATH, app bundle) and never proves ownership.
type Evidence struct {
	Present bool
	Owners  []Owner
}

// OwnershipProbe gathers evidence for one app. An error means the evidence is
// unknown, which blocks every decision that depends on it.
type OwnershipProbe func(name string, app manifest.AppConfig) (Evidence, error)

// Request selects apps exactly, per scope. Packages and configurations are
// chosen independently; dependencies are never added automatically.
type Request struct {
	Packages []string
	Configs  []string
}

// PackageStatus describes what the cleaner may do with one app's package.
type PackageStatus string

const (
	PackageRemove       PackageStatus = "remove"        // Argv uninstalls Owner
	PackageNotInstalled PackageStatus = "not-installed" // no owner and no presence
	PackageConfigOnly   PackageStatus = "config-only"   // the app declares no package
	PackageDuplicate    PackageStatus = "duplicate"     // an earlier item removes the same package
	PackageUnsupported  PackageStatus = "unsupported"   // no tested uninstall mapping
	PackageBlocked      PackageStatus = "blocked"       // unknown, ambiguous or still required
)

// PackageItem is one entry of the package plan, dependents before their
// dependencies.
type PackageItem struct {
	AppName  string        `json:"app_name"`
	Status   PackageStatus `json:"status"`
	Owner    *Owner        `json:"owner,omitempty"`
	Argv     []string      `json:"argv,omitempty"` // executed without a shell
	Reason   string        `json:"reason,omitempty"`
	Warnings []string      `json:"warnings,omitempty"`
}

// ConfigStatus describes what the cleaner may do with one app's configuration.
type ConfigStatus string

const (
	// ConfigQuarantine lists home paths to move into a recoverable
	// quarantine. The runner must still validate each path before moving it.
	ConfigQuarantine ConfigStatus = "quarantine"
	ConfigNone       ConfigStatus = "no-config" // the app declares no paths
	ConfigBlocked    ConfigStatus = "blocked"   // a declared path is unsafe to move
)

// ConfigItem is one entry of the configuration plan.
type ConfigItem struct {
	AppName string       `json:"app_name"`
	Status  ConfigStatus `json:"status"`
	Paths   []string     `json:"paths,omitempty"` // "~/..." sources from the manifest
	// Preserve lists secrets and excluded patterns (data, caches, history)
	// that must stay in place.
	Preserve []string `json:"preserve,omitempty"`
	Reason   string   `json:"reason,omitempty"`
}

// Plan is the read-only cleanup preview. Its scopes are independent.
type Plan struct {
	Packages []PackageItem `json:"packages"`
	Configs  []ConfigItem  `json:"configs"`
}

// Result is what an Executor observed. A non-zero ExitCode is reported here,
// not as an error; an error means the command could not run at all.
type Result struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

// Executor runs one argv directly, never through a shell. Tests inject fakes
// so no package manager is ever queried or changed.
type Executor interface {
	Run(ctx context.Context, argv []string) (Result, error)
}
