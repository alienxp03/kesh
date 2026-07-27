package config

import (
	"path/filepath"
	"testing"
)

func TestFromEnvironmentUsesXDGLocations(t *testing.T) {
	t.Setenv("HOME", "/home/kesh")
	t.Setenv("XDG_STATE_HOME", "/state")
	t.Setenv("XDG_CONFIG_HOME", "/config")
	paths := FromEnvironment()
	if got, want := paths.Pins(), filepath.Join("/state", "kesh", "pins.json"); got != want {
		t.Fatalf("Pins() = %q, want %q", got, want)
	}
	if got, want := paths.Aliases(), filepath.Join("/config", "kesh", "aliases.json"); got != want {
		t.Fatalf("Aliases() = %q, want %q", got, want)
	}
}

func TestFromEnvironmentFallsBackToHome(t *testing.T) {
	t.Setenv("HOME", "/home/kesh")
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	paths := FromEnvironment()
	if got, want := paths.SavedSessions(), "/home/kesh/.local/state/kesh/saved-sessions.json"; got != want {
		t.Fatalf("SavedSessions() = %q, want %q", got, want)
	}
	if got, want := paths.File(), "/home/kesh/.config/kesh/config.yaml"; got != want {
		t.Fatalf("File() = %q, want %q", got, want)
	}
}
