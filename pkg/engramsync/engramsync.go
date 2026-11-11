// Package engramsync carries Engram memories with a backup: full backup
// exports them into the stage and restore with configs imports them.
package engramsync

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/kevnnard/dots/pkg/clean"
)

// StagedFile is where the export lives, relative to a stage or a received
// backup. It sits under configs/ so `dots backup --send` carries it; restore
// deploys only manifest paths, so the file is never copied into home.
const StagedFile = "configs/engram/memories.json"

// Options say how to find and run engram. A nil LookPath or Exec means
// engram is not installed, so the zero value never runs anything.
type Options struct {
	Exec     clean.Executor
	LookPath func(string) (string, error)
}

// ForThisMachine runs the engram found on PATH.
func ForThisMachine() Options {
	return Options{Exec: clean.OSExecutor{}, LookPath: exec.LookPath}
}

func (o Options) available() bool {
	if o.Exec == nil || o.LookPath == nil {
		return false
	}
	_, err := o.LookPath("engram")
	return err == nil
}

// Export writes every project's memories to StagedFile in stage. Without
// engram it returns one line saying so and no error; an error means engram
// ran and failed.
func Export(ctx context.Context, opts Options, stage string) (string, error) {
	if !opts.available() {
		return "ℹ️  Engram not installed: memories not exported", nil
	}
	file := filepath.Join(stage, StagedFile)
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return "", err
	}
	if err := run(ctx, opts, "engram", "export", file, "--all"); err != nil {
		return "", err
	}
	// Memories are personal: keep the export private like the stage.
	if err := os.Chmod(file, 0o600); err != nil {
		return "", err
	}
	return fmt.Sprintf("🧠 Engram memories exported: %s", file), nil
}

// Import loads StagedFile from dir with `engram import`. Engram skips
// observations it already holds, so importing the same export twice does not
// duplicate them. A missing export or a missing engram is reported in the
// line, not as an error; dryRun only says what would be imported.
func Import(ctx context.Context, opts Options, dir string, dryRun bool) (string, error) {
	file := filepath.Join(dir, StagedFile)
	if _, err := os.Stat(file); err != nil {
		return fmt.Sprintf("ℹ️  Engram: no Engram export in %s, no memories imported", dir), nil
	}
	if !opts.available() {
		return fmt.Sprintf("ℹ️  Engram not installed: memories in %s not imported", file), nil
	}
	if dryRun {
		return fmt.Sprintf("🧠 Engram: would import memories from %s", file), nil
	}
	if err := run(ctx, opts, "engram", "import", file); err != nil {
		return "", err
	}
	return fmt.Sprintf("🧠 Engram memories imported from %s (memories already present are skipped)", file), nil
}

func run(ctx context.Context, opts Options, argv ...string) error {
	res, err := opts.Exec.Run(ctx, argv)
	if err != nil {
		return fmt.Errorf("%s: %w", strings.Join(argv[:2], " "), err)
	}
	if res.ExitCode != 0 {
		msg := strings.TrimSpace(string(res.Stderr))
		if msg == "" {
			msg = strings.TrimSpace(string(res.Stdout))
		}
		return fmt.Errorf("%s exited %d: %s", strings.Join(argv[:2], " "), res.ExitCode, msg)
	}
	return nil
}
