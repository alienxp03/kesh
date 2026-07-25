package domain

import (
	"reflect"
	"sort"
	"testing"
)

func TestMatchPullRequest(t *testing.T) {
	pullRequests := map[string]PullRequest{
		PullRequestKey("feature", "old"): {Number: 2},
		PullRequestKey("feature", "new"): {Number: 4},
	}
	if got, exact := MatchPullRequest(pullRequests, "feature", "old"); !exact || got.Number != 2 {
		t.Fatalf("exact match = %#v, %t", got, exact)
	}
	if got, exact := MatchPullRequest(pullRequests, "feature", "other"); exact || got.Number != 4 {
		t.Fatalf("branch fallback = %#v, %t", got, exact)
	}
}

func TestParseWorktreePorcelain(t *testing.T) {
	output := "worktree /repo\nHEAD abc\nbranch refs/heads/main\n\nworktree /repo-feature\nHEAD def\ndetached\nlocked reason\n"
	got := ParseWorktreePorcelain(output)
	want := []Worktree{
		{Path: "/repo", Head: "abc", Branch: "main"},
		{Path: "/repo-feature", Head: "def", Branch: "(detached)"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parsed worktrees = %#v, want %#v", got, want)
	}
}

func TestParseWorktreeStatus(t *testing.T) {
	dirty, ahead, behind, changes := ParseWorktreeStatus("## main...origin/main [ahead 2, behind 1]\n M main.go\n?? new.txt\n")
	if !dirty || ahead != 2 || behind != 1 || !reflect.DeepEqual(changes, []string{" M main.go", "?? new.txt"}) {
		t.Fatalf("status = %t, %d, %d, %#v", dirty, ahead, behind, changes)
	}
}

func TestEntryLess(t *testing.T) {
	entries := []EntryOrder{
		{Kind: "ssh", Order: 0},
		{Kind: "project", Order: 1},
		{Saved: true, Order: 2},
		{Open: true, LastFocused: 3, Order: 3},
		{Open: true, LastFocused: 9, Order: 4},
	}
	sort.SliceStable(entries, func(i, j int) bool { return EntryLess(entries[i], entries[j]) })
	got := []int{entries[0].Order, entries[1].Order, entries[2].Order, entries[3].Order, entries[4].Order}
	want := []int{4, 3, 2, 1, 0}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("entry order = %v, want %v", got, want)
	}
}

func TestPlanDestroyOnlyRemovesLinkedProjectWorktrees(t *testing.T) {
	linked := PlanDestroy(DestroyTarget{
		Name: "feature", Saved: true, Open: true, TabCount: 2,
		Kind: "project", Path: "/worktrees/feature", IsLinkedWorktree: true, LinkedBranch: "feature",
	})
	if linked.WorktreePath != "/worktrees/feature" || linked.Branch != "feature" || !linked.CloseSession || !linked.Saved {
		t.Fatalf("linked plan = %#v", linked)
	}
	plain := PlanDestroy(DestroyTarget{
		Name: "main", Kind: "project", Path: "/workspace/main", Open: true, TabCount: 1,
	})
	if plain.WorktreePath != "" || plain.Branch != "" || !plain.CloseSession {
		t.Fatalf("plain plan = %#v", plain)
	}
}

func TestComposedSessionName(t *testing.T) {
	if name, ok := ComposedSessionName("kesh-release"); !ok || name != "release" {
		t.Fatalf("name = %q, ok = %t", name, ok)
	}
	if _, ok := ComposedSessionName("release"); ok {
		t.Fatal("non-Kesh session was accepted")
	}
}

func TestSortAndSelectMergedWorktrees(t *testing.T) {
	worktrees := []Worktree{
		{Path: "/repo", Branch: "main", IsDefault: true},
		{Path: "/closed", Branch: "closed", PRStatus: "closed"},
		{Path: "/open", Branch: "open", PRStatus: "open"},
		{Path: "/merged", Branch: "merged", PRStatus: "merged", Head: "abc"},
		{Path: "/plain", Branch: "plain"},
	}
	SortWorktrees(worktrees)
	gotOrder := []string{worktrees[0].Branch, worktrees[1].Branch, worktrees[2].Branch, worktrees[3].Branch, worktrees[4].Branch}
	if want := []string{"main", "open", "merged", "closed", "plain"}; !reflect.DeepEqual(gotOrder, want) {
		t.Fatalf("order = %#v, want %#v", gotOrder, want)
	}
	merged := MergedWorktrees(worktrees, "closed\n", "main", map[string]map[string]bool{"merged": {"abc": true}})
	if len(merged) != 2 || merged[0].Branch != "merged" || merged[1].Branch != "closed" {
		t.Fatalf("merged worktrees = %#v", merged)
	}
}
