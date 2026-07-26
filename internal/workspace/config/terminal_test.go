package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestTerminalConfigSupportsKittySessionNames(t *testing.T) {
	root := t.TempDir()
	defaultPath := filepath.Join(root, "default.yaml")
	write(t, defaultPath, strings.Join([]string{
		"terminal:",
		"  session_name: ${repo}/${branch}",
		"workspaces:",
		"  - name: app",
		"",
	}, "\n"))
	loaded, err := LoadFile(defaultPath, root)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Terminal.SessionName != "${repo}/${branch}" {
		t.Fatalf("terminal = %#v", loaded.Terminal)
	}
}

func TestTerminalConfigValidation(t *testing.T) {
	root := t.TempDir()
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{"provider", "terminal:\n  provider: tmux\n", "unsupported terminal key"},
		{"workspace-mode", "workspace_mode: all\n", "unsupported key"},
		{"default-workspaces", "default_workspaces: [app]\n", "unsupported key"},
		{"integrations", "integrations:\n  zoxide: true\n", "unsupported key"},
		{"tmux-mode", "tmux_mode: window\n", "unsupported key"},
		{"tmux-session-name", "tmux_session_name: app\n", "unsupported key"},
		{"kitty-zoom", "workspaces:\n  - name: app\n    panes:\n      - commands:\n          - nvim\n        zoom: true\n", "zoom is not supported"},
		{"kitty-size", "workspaces:\n  - name: app\n    panes:\n      - commands:\n          - nvim\n        size: 80\n", "size is not supported"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(root, test.name+".yaml")
			write(t, path, test.content)
			_, err := LoadFile(path, root)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want containing %q", err, test.want)
			}
		})
	}
}
