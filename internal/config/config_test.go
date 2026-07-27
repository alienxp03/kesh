package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRootsPreserveDefaultsAndOverrides(t *testing.T) {
	home := filepath.Join(string(filepath.Separator), "home", "stan")
	missing := filepath.Join(t.TempDir(), "missing.yaml")
	if got, err := CloneRoot(missing, home); err != nil || got != filepath.Join(home, "workspace") {
		t.Fatalf("default clone root = %q, %v", got, err)
	}
	if got, err := WorktreeRoot(missing, home); err != nil || got != filepath.Join(home, "worktree") {
		t.Fatalf("default worktree root = %q, %v", got, err)
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := "clone:\n  root: ~/src\nworktree:\n  root: /tmp/trees\ncheckout:\n  root: ~/checkouts\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := CloneRoot(path, home); err != nil || got != filepath.Join(home, "src") {
		t.Fatalf("configured clone root = %q, %v", got, err)
	}
	if got, err := WorktreeRoot(path, home); err != nil || got != "/tmp/trees" {
		t.Fatalf("configured worktree root = %q, %v", got, err)
	}
	if got, err := CheckoutRoot(path, home); err != nil || got != filepath.Join(home, "checkouts") {
		t.Fatalf("configured checkout root = %q, %v", got, err)
	}
}

func TestStartupSessionsResolvePathsAndPins(t *testing.T) {
	home := filepath.Join(string(filepath.Separator), "home", "stan")
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := "startup:\n  sessions:\n    - path: ~/workspace/aurora\n      pin: 0\n    - name: tools\n      path: /tmp/tools\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	sessions, err := StartupSessions(path, home)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 || sessions[0].Path != filepath.Join(home, "workspace/aurora") || sessions[0].Pin == nil || *sessions[0].Pin != 0 || sessions[1].Name != "tools" {
		t.Fatalf("startup sessions = %#v", sessions)
	}
}

func TestStartupSessionsRejectInvalidValues(t *testing.T) {
	home := filepath.Join(string(filepath.Separator), "home", "stan")
	for name, content := range map[string]string{
		"missing path":  "startup:\n  sessions:\n    - pin: 0\n",
		"relative path": "startup:\n  sessions:\n    - path: workspace/aurora\n",
		"invalid pin":   "startup:\n  sessions:\n    - path: ~/workspace/aurora\n      pin: 10\n",
		"unknown key":   "startup:\n  sessions:\n    - path: ~/workspace/aurora\n      pinn: 0\n",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := StartupSessions(path, home); err == nil {
				t.Fatal("expected invalid startup config")
			}
		})
	}
}

func TestCheckoutDefaultsToConfiguredCloneRoot(t *testing.T) {
	home := filepath.Join(string(filepath.Separator), "home", "stan")
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("clone:\n  root: ~/src\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := CheckoutRoot(path, home); err != nil || got != filepath.Join(home, "src") {
		t.Fatalf("checkout root = %q, %v", got, err)
	}
}

func TestExpandHomePathRejectsUnsafeOrUnsupportedValues(t *testing.T) {
	home := filepath.Join(string(filepath.Separator), "home", "stan")
	for _, value := range []string{"", "~other/repo", "repo", "~/one\n/two"} {
		got, err := ExpandHomePath(value, home)
		if value == "repo" {
			if err != nil || got != "repo" {
				t.Fatalf("relative path = %q, %v", got, err)
			}
			continue
		}
		if err == nil {
			t.Fatalf("ExpandHomePath(%q) unexpectedly succeeded as %q", value, got)
		}
	}
}
