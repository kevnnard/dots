package ui

import (
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/kevnnard/dots/pkg/clean"
	"github.com/kevnnard/dots/pkg/detector"
	"github.com/kevnnard/dots/pkg/manifest"
)

// RunTUI launches the interactive terminal user interface.
func RunTUI(m *manifest.Manifest, sys *detector.SystemInfo, repoDir string) error {
	model := NewModel(m, sys, repoDir)
	if home, err := os.UserHomeDir(); err == nil {
		model.Receipts = clean.NativeProbe{Sys: sys, Exec: model.CleanExec, Home: home}.Installed
	}
	p := tea.NewProgram(model, tea.WithAltScreen())
	_, err := p.Run()
	return err
}
