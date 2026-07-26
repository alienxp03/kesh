// Package layout defines the provider-neutral terminal layout model used by wktree.
package layout

import "github.com/alienxp03/kesh/internal/workspace/run"

const (
	ModeWindow  = "window"
	ModeSession = "session"
)

type OpenOptions struct {
	Mode        string
	SessionName string
	Windows     []Window
	Env         map[string]string
	CacheDir    string
	Runner      run.Runner
}

type Window struct {
	Name         string
	WorktreePath string
	Commands     []PaneCommand
}

type PaneCommand struct {
	Commands   []string
	Split      string
	Focus      bool
	Zoom       bool
	Size       string
	Percentage int
}

type KillOptions struct {
	Mode        string
	SessionName string
	WindowNames []string
	KillSession bool
	Env         map[string]string
	CacheDir    string
	Runner      run.Runner
}
