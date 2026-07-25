package kitty

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/alienxp03/dotfiles/apps/kesh/internal/domain"
)

func TestComposedSessionContent(t *testing.T) {
	content := ComposedSessionContent("release", "/Users/stan", []domain.SessionEntry{
		{Name: "API", Directory: "/projects/api"},
		{Name: "production", SSHHost: "production"},
	})
	want := "os_window_title release\nlayout splits\n" +
		"new_tab API\ncd /projects/api\nlaunch --title \"API\"\n" +
		"new_tab production\ncd /Users/stan\nlaunch --title \"ssh: production\" ssh \"production\"\n" +
		"focus\nfocus_os_window\n"
	if content != want {
		t.Fatalf("content = %q, want %q", content, want)
	}
}

func TestPinShortcutsRoundTripAndPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "kitty-pins.conf")
	pins := PinSessionFiles{"1": "/sessions/project one.kitty-session"}
	content := string(PinShortcutsContent(pins))
	if !strings.Contains(content, `map cmd+1 goto_session "/sessions/project one.kitty-session"`) {
		t.Fatalf("content = %q", content)
	}
	changed, err := SavePinShortcuts(path, pins)
	if err != nil || !changed {
		t.Fatalf("first save = %t, %v", changed, err)
	}
	changed, err = SavePinShortcuts(path, pins)
	if err != nil || changed {
		t.Fatalf("second save = %t, %v", changed, err)
	}
}
