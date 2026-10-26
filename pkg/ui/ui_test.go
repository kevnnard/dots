package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/kevnnard/dots/pkg/detector"
	"github.com/kevnnard/dots/pkg/manifest"
)

func TestUIModelNavigation(t *testing.T) {
	m := manifest.DefaultManifest()
	sys := detector.Detect()
	repoDir := "/tmp/mock-dotfiles"

	model := NewModel(m, sys, repoDir)

	// Initial view should be main
	if model.State != viewMain {
		t.Errorf("expected viewMain, got %v", model.State)
	}

	// Press down arrow
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyDown})
	mUpdated := updated.(Model)
	if mUpdated.Cursor != 1 {
		t.Errorf("expected cursor 1, got %d", mUpdated.Cursor)
	}

	// Press up arrow
	updated, _ = mUpdated.Update(tea.KeyMsg{Type: tea.KeyUp})
	mUpdated = updated.(Model)
	if mUpdated.Cursor != 0 {
		t.Errorf("expected cursor 0, got %d", mUpdated.Cursor)
	}

	// Press enter on cursor 0 (Restore)
	updated, _ = mUpdated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	mUpdated = updated.(Model)
	if mUpdated.State != viewRestorePreset {
		t.Errorf("expected viewRestorePreset, got %v", mUpdated.State)
	}

	// Render view
	viewOutput := mUpdated.View()
	if !strings.Contains(viewOutput, "WORKSTATION & DOTFILES MANAGER") {
		t.Errorf("expected header in view output, got: %s", viewOutput)
	}
}

func TestUICustomAppsToggle(t *testing.T) {
	m := manifest.DefaultManifest()
	sys := detector.Detect()
	model := NewModel(m, sys, "/tmp/mock-dotfiles")

	// Transition to custom apps view
	model.State = viewCustomApps
	model.Cursor = 0

	initialChecked := model.AppItems[0].Checked

	// Press space to toggle
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeySpace})
	mUpdated := updated.(Model)
	if mUpdated.AppItems[0].Checked == initialChecked {
		t.Error("expected checkbox to toggle on space")
	}

	// Press 'a' to toggle all
	updated, _ = mUpdated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	mUpdated = updated.(Model)
	for _, item := range mUpdated.AppItems {
		if !item.Checked {
			t.Error("expected all items to be checked after pressing 'a'")
		}
	}
}
