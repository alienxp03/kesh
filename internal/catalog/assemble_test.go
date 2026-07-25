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
