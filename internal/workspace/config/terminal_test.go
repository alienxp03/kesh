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
		"integrations:",
		"  zoxide: true",
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
	if !loaded.Integrations.Zoxide {
		t.Fatalf("integrations = %#v", loaded.Integrations)
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
		{"tmux-mode", "tmux_mode: window\n", "unsupported key"},
		{"tmux-session-name", "tmux_session_name: app\n", "unsupported key"},
		{"integration-key", "integrations:\n  unknown: true\n", "unsupported integrations key"},
		{"integration-type", "integrations:\n  zoxide: yes please\n", "cannot unmarshal"},
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
