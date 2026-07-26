package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestTerminalConfigDefaultsToKittyAndSupportsTmuxOptIn(t *testing.T) {
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
	if loaded.Terminal.Provider != TerminalProviderKitty || loaded.Terminal.Mode != DefaultTerminalMode || loaded.Terminal.SessionName != "${repo}/${branch}" {
		t.Fatalf("terminal = %#v", loaded.Terminal)
	}
	if !loaded.Integrations.Zoxide {
		t.Fatalf("integrations = %#v", loaded.Integrations)
	}

	tmuxPath := filepath.Join(root, "tmux.yaml")
	write(t, tmuxPath, "terminal:\n  provider: tmux\n  mode: session\n  session_name: ${repo}/${branch}\n")
	tmuxConfig, err := LoadFile(tmuxPath, root)
	if err != nil {
		t.Fatal(err)
	}
	if tmuxConfig.Terminal.Provider != TerminalProviderTmux || tmuxConfig.Terminal.Mode != "session" {
		t.Fatalf("tmux terminal = %#v", tmuxConfig.Terminal)
	}
}

func TestTerminalConfigValidation(t *testing.T) {
	root := t.TempDir()
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{"provider", "terminal:\n  provider: wezterm\n", "terminal.provider"},
		{"legacy-tmux", "tmux:\n  mode: window\n", "use terminal.provider: tmux"},
		{"integration-key", "integrations:\n  unknown: true\n", "unsupported integrations key"},
		{"integration-type", "integrations:\n  zoxide: yes please\n", "cannot unmarshal"},
		{"kitty-zoom", "workspaces:\n  - name: app\n    panes:\n      - command: nvim\n        zoom: true\n", "zoom is not supported"},
		{"kitty-size", "workspaces:\n  - name: app\n    panes:\n      - command: nvim\n        size: 80\n", "size is not supported"},
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
