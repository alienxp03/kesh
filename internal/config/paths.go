// Package config resolves Kesh's XDG configuration and state locations.
package config

import (
	"os"
	"path/filepath"
)

// Paths contains the stable on-disk locations used by Kesh. It deliberately
// does not create directories: callers choose permissions and write policy.
type Paths struct {
	StateDirectory  string
	ConfigDirectory string
	CacheDirectory  string
}

// FromEnvironment resolves XDG paths with the same HOME fallbacks used by
// existing Kesh installations.
func FromEnvironment() Paths {
	home := os.Getenv("HOME")
	stateHome := os.Getenv("XDG_STATE_HOME")
	if stateHome == "" {
		stateHome = filepath.Join(home, ".local", "state")
	}
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		configHome = filepath.Join(home, ".config")
	}
	cacheHome := os.Getenv("XDG_CACHE_HOME")
	if cacheHome == "" {
		cacheHome = filepath.Join(home, ".cache")
	}
	return Paths{
		StateDirectory:  filepath.Join(stateHome, "kesh"),
		ConfigDirectory: filepath.Join(configHome, "kesh"),
		CacheDirectory:  filepath.Join(cacheHome, "kesh"),
	}
}

func (p Paths) Pins() string          { return filepath.Join(p.StateDirectory, "pins.json") }
func (p Paths) SavedSessions() string { return filepath.Join(p.StateDirectory, "saved-sessions.json") }
func (p Paths) Sessions() string      { return filepath.Join(p.StateDirectory, "sessions") }
func (p Paths) PinShortcuts() string  { return filepath.Join(p.StateDirectory, "kitty-pins.conf") }
func (p Paths) KittyRun() string      { return filepath.Join(p.StateDirectory, "kitty-run") }
func (p Paths) File() string          { return filepath.Join(p.ConfigDirectory, "config.toml") }
func (p Paths) Names() string         { return filepath.Join(p.ConfigDirectory, "names.json") }
func (p Paths) PRCache() string       { return filepath.Join(p.CacheDirectory, "pr-status.json") }
