package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestHelpKeyOpensAndClosesHelp(t *testing.T) {
	m := model{}
	updated, command := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	if command != nil {
		t.Fatal("help key scheduled a command")
	}
	m = updated.(model)
	if m.mode != modeHelp {
		t.Fatalf("mode after help key = %d, want help", m.mode)
	}
	popup := m.helpPopupView(100, 100)
	for _, want := range []string{"Keyboard shortcuts", "gg/G", "open folder without layout", "pull (fetch and rebase)", "cycle filters", "Worktrees", "Agents", "Press ? or Esc to close"} {
		if !strings.Contains(popup, want) {
			t.Fatalf("help popup missing %q:\n%s", want, popup)
		}
	}
	if lines := strings.Count(popup, "\n") + 1; lines > 100 {
		t.Fatalf("help popup is too tall (%d lines):\n%s", lines, popup)
	}

	updated, command = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if command != nil || updated.(model).helpScroll != 1 {
		t.Fatalf("j did not scroll help: model=%#v command=%v", updated.(model), command)
	}
	smallPopup := updated.(model).helpPopupView(80, 20)
	if !strings.Contains(smallPopup, "j/k scroll") {
		t.Fatalf("small help popup missing scroll hint:\n%s", smallPopup)
	}
	if lines := strings.Count(smallPopup, "\n") + 1; lines > 20 {
		t.Fatalf("small help popup is too tall (%d lines):\n%s", lines, smallPopup)
	}

	updated, command = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if command != nil {
		t.Fatal("closing help scheduled a command")
	}
	m = updated.(model)
	if m.mode != modeNormal {
		t.Fatalf("mode after closing help = %d, want normal", m.mode)
	}
}
