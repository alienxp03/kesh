package paths

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBranchSlug(t *testing.T) {
	tests := map[string]string{
		"feature/example":               "feature-example",
		"origin/feature/example branch": "feature-example-branch",
		"feature/$bad:name":             "feature-bad-name",
		"feature/testing_1":             "feature-testing_1",
	}
	for input, want := range tests {
		got, err := BranchSlug(input)
		if err != nil {
			t.Fatalf("BranchSlug(%q) returned error: %v", input, err)
		}
		if got != want {
			t.Fatalf("BranchSlug(%q) = %q, want %q", input, got, want)
		}
	}
	if _, err := BranchSlug("///"); err == nil {
		t.Fatal("BranchSlug should reject empty slugs")
	}
}

func TestParseGitHubRemote(t *testing.T) {
	tests := []string{
		"https://github.com/alienxp03/dotfiles.git",
		"git@github.com:alienxp03/dotfiles.git",
		"ssh://git@github.com/alienxp03/dotfiles.git",
	}
	for _, input := range tests {
		got, ok, err := ParseGitHubRemote(input)
		if err != nil {
			t.Fatalf("ParseGitHubRemote(%q) returned error: %v", input, err)
		}
		if !ok || got != filepath.Join("alienxp03", "dotfiles") {
			t.Fatalf("ParseGitHubRemote(%q) = %q, %v; want alienxp03/dotfiles, true", input, got, ok)
		}
	}
	if _, ok, err := ParseGitHubRemote("https://example.com/alienxp03/dotfiles.git"); ok || err != nil {
		t.Fatalf("non-GitHub remote should not parse, ok=%v err=%v", ok, err)
	}
}

func TestParseGitHubPullURL(t *testing.T) {
	gotSlug, gotNumber, ok, err := ParseGitHubPullURL("https://github.com/alienxp03/dotfiles/pull/123")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || gotSlug != filepath.Join("alienxp03", "dotfiles") || gotNumber != 123 {
		t.Fatalf("ParseGitHubPullURL returned slug=%q number=%d ok=%v", gotSlug, gotNumber, ok)
	}

	gotSlug, gotNumber, ok, err = ParseGitHubPullURL("https://github.com/alienxp03/dotfiles/pull/123/files")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || gotSlug != filepath.Join("alienxp03", "dotfiles") || gotNumber != 123 {
		t.Fatalf("ParseGitHubPullURL with suffix returned slug=%q number=%d ok=%v", gotSlug, gotNumber, ok)
	}

	if _, _, ok, err := ParseGitHubPullURL("https://example.com/alienxp03/dotfiles/pull/123"); ok || err != nil {
		t.Fatalf("non-GitHub PR URL should not parse, ok=%v err=%v", ok, err)
	}
}

func TestRepoDirectorySlug(t *testing.T) {
	got, err := RepoDirectorySlug("/tmp/example repo", "Test User")
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join("Test-User", "example-repo") {
		t.Fatalf("got %q", got)
	}
}

func TestWorktreeHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	got, err := WorktreeHome("")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, "workspace", "worktrees")
	if got != want {
		t.Fatalf("WorktreeHome default = %q, want %q", got, want)
	}

	got, err = WorktreeHome("/tmp/custom")
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Clean("/tmp/custom") {
		t.Fatalf("WorktreeHome explicit = %q", got)
	}
}

func TestWorktreePath(t *testing.T) {
	got := WorktreePath("/tmp/worktrees", filepath.Join("alienxp03", "dotfiles"), "feature-example")
	want := filepath.Join("/tmp/worktrees", "alienxp03", "dotfiles", "feature-example")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
