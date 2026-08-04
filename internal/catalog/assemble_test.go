package catalog

import (
	"testing"

	"github.com/alienxp03/kesh/internal/domain"
	"github.com/alienxp03/kesh/internal/kitty"
	"github.com/alienxp03/kesh/internal/state"
)

func TestAssembleMergesKittySavedAndSSHEntries(t *testing.T) {
	kittyState := kitty.State{{Tabs: []kitty.Tab{
		{
			ID: 1, Title: "dotfiles",
			Windows: []kitty.Window{{
				ID: 11, CWD: "/workspace/dotfiles", SessionName: "dotfiles", LastFocusedAt: 8,
				Env: map[string]string{"PWD": "/workspace/dotfiles"},
			}},
		},
		{
			ID: 2, Title: "kesh",
			Windows: []kitty.Window{{ID: 12, Cmdline: []string{"kesh"}}},
		},
	}}}
	saved := state.SavedSessions{
		Version: state.CurrentSavedSessionVersion,
		Sessions: map[string]state.SavedSessionRecord{
			"/state/release.kitty-session": {
				Name: "Release", SessionName: "kesh-release", SessionFile: "/state/release.kitty-session",
				Projects: []string{"/workspace/api", "/workspace/web"},
			},
		},
	}
	entries, context := Assemble(
		kittyState, saved, []SSHHost{{Name: "production", Target: "stan@production:22"}}, 999, "/Users/stan",
	)
	if len(entries) != 3 {
		t.Fatalf("entries = %#v", entries)
	}
	if entries[0].Key != "/workspace/dotfiles" || entries[0].Kind != "project" || !entries[0].Open {
		t.Fatalf("live project = %#v", entries[0])
	}
	if entries[1].Key != "workspace:kesh-release" || !entries[1].Saved {
		t.Fatalf("saved workspace = %#v", entries[1])
	}
	if entries[2].Key != "ssh://production" || entries[2].Detail != "stan@production:22" {
		t.Fatalf("SSH entry = %#v", entries[2])
	}
	if !context.LivePaths["/workspace/dotfiles"] || !context.MergedPaths["/workspace/dotfiles"] {
		t.Fatalf("context = %#v", context)
	}
}

func TestAssembleAttachesOpenUnscopedSSHTabs(t *testing.T) {
	kittyState := kitty.State{{Tabs: []kitty.Tab{{
		ID:    4,
		Title: "hermes",
		Windows: []kitty.Window{{
			ID: 41, CWD: "/Users/stan", LastFocusedAt: 9,
			ForegroundProcesses: []kitty.ForegroundProcess{{Cmdline: []string{"ssh", "hermes"}}},
		}},
	}}}}
	entries, _ := Assemble(kittyState, state.SavedSessions{}, []SSHHost{{Name: "hermes", Target: "hermes"}}, 999, "/Users/stan")
	if len(entries) != 1 || !entries[0].Open || entries[0].Session != "ssh-hermes" {
		t.Fatalf("open SSH entry = %#v", entries)
	}
	if len(entries[0].Tabs) != 1 || entries[0].Tabs[0].ID != 4 {
		t.Fatalf("open SSH tabs = %#v", entries[0].Tabs)
	}
}

func TestAssembleUsesKeshLayoutSessionName(t *testing.T) {
	entries, _ := Assemble(kitty.State{{Tabs: []kitty.Tab{{
		ID: 1, Title: "dotfiles",
		Windows: []kitty.Window{{
			ID: 11, CWD: "/Users/stan/.dotfiles", SessionName: "kesh-63c760d5", LastFocusedAt: 8,
			Env: map[string]string{"PWD": "/Users/stan/.dotfiles", "KESH_KITTY_SESSION": "dot-2"},
		}},
	}}}}, state.SavedSessions{}, nil, 999, "/Users/stan")
	if len(entries) != 1 || entries[0].Name != "dot-2" || entries[0].OriginalName != "dot-2" {
		t.Fatalf("Kesh layout session = %#v, want display name dot-2", entries)
	}
}

func TestMergeZoxideSkipsRepresentedPathsAndAddsLivePaths(t *testing.T) {
	context := domain.CatalogContext{
		LivePaths:    map[string]bool{"/workspace/live": true},
		MergedPaths:  map[string]bool{"/workspace/open": true},
		SessionNames: map[string]bool{"repo": true},
		Home:         "/Users/stan",
	}
	entries := MergeZoxide([]byte("/workspace/open\n/workspace/repo\n"), context)
	if len(entries) != 2 || entries[0].Path != "/workspace/repo" || entries[1].Path != "/workspace/live" {
		t.Fatalf("entries = %#v", entries)
	}
	if !entries[0].NameTaken {
		t.Fatalf("session-name collision was not preserved: %#v", entries[0])
	}
}

func TestMergeZoxideAttachesOpenStateToKnownProject(t *testing.T) {
	context := domain.CatalogContext{
		Home: "/Users/stan",
		OpenTabs: map[string]domain.OpenTabState{
			"/workspace/repo": {Tabs: []domain.Tab{{ID: 7, Title: "repo-code"}}, LastFocused: 42},
		},
	}
	entries := MergeZoxide([]byte("/workspace/repo\n"), context)
	if len(entries) != 1 {
		t.Fatalf("entries = %#v", entries)
	}
	got := entries[0]
	if !got.Open || got.LastFocused != 42 || len(got.Tabs) != 1 || got.Tabs[0].Title != "repo-code" {
		t.Fatalf("open state not attached: %#v", got)
	}
}

func TestAssembleHidesUnscopedWindowsThatAreNotSessions(t *testing.T) {
	// A live window at $HOME with no session_name is not a session and must not
	// become a catalog entry. It only surfaces if zoxide knows the path.
	kittyState := kitty.State{{Tabs: []kitty.Tab{{
		ID:    1,
		Title: "home",
		Windows: []kitty.Window{{
			ID: 11, CWD: "/Users/stan", LastFocusedAt: 5,
			Env: map[string]string{"PWD": "/Users/stan"},
		}},
	}}}}
	entries, context := Assemble(kittyState, state.SavedSessions{}, nil, 999, "/Users/stan")
	if len(entries) != 0 {
		t.Fatalf("unscoped home window became an entry: %#v", entries)
	}
	if _, ok := context.LivePaths["/Users/stan"]; ok {
		t.Fatalf("unscoped window leaked into LivePaths: %#v", context.LivePaths)
	}
	if _, ok := context.OpenTabs["/Users/stan"]; !ok {
		t.Fatalf("unscoped window open state not carried via OpenTabs: %#v", context.OpenTabs)
	}
}
