package app

import (
	"reflect"
	"strings"
	"testing"

	gitx "github.com/alienxp03/kesh/internal/git"
	"github.com/alienxp03/kesh/internal/system"
)

// appGitRunner records every command and returns a fixed combined-output value,
// letting addWorktreeForBranch's branch-on-origin decision be asserted without a
// real repository. A non-empty answer means BranchExistsOnOrigin reports true.
type appGitRunner struct {
	specs []system.Spec
	out   []byte
}

func (r *appGitRunner) Output(spec system.Spec) ([]byte, error) {
	r.specs = append(r.specs, spec)
	return r.out, nil
}

func (r *appGitRunner) CombinedOutput(spec system.Spec) ([]byte, error) {
	r.specs = append(r.specs, spec)
	return r.out, nil
}

// TestAddWorktreeForBranch_NewBranchCreatesBranchOffHead proves the popup path
// no longer demands the branch exist on origin: when ls-remote finds nothing,
// it runs `git worktree add -b <branch> <path>` to create it off HEAD.
func TestAddWorktreeForBranch_NewBranchCreatesBranchOffHead(t *testing.T) {
	recorder := &appGitRunner{out: nil} // ls-remote empty -> branch absent on origin
	restore := system.SetRunner(recorder)
	t.Cleanup(restore)

	if err := addWorktreeForBranch(gitx.Repository{Path: "/repo"}, "/trees/topic", "rename-session"); err != nil {
		t.Fatalf("addWorktreeForBranch: %v", err)
	}
	last := recorder.specs[len(recorder.specs)-1]
	want := system.Spec{Name: "git", Args: []string{"-C", "/repo", "worktree", "add", "-b", "rename-session", "/trees/topic"}}
	if !reflect.DeepEqual(last, want) {
		t.Fatalf("new-branch spec = %#v, want %#v", last, want)
	}
}

func TestFetchWorktreesDefersPerWorktreeStatus(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	recorder := &appGitRunner{out: []byte("worktree /repo\nHEAD abc123\nbranch refs/heads/main\n")}
	restore := system.SetRunner(recorder)
	t.Cleanup(restore)

	message := fetchWorktrees("/repo", 0, -1, -1)()
	if _, ok := message.(worktreeListMsg); !ok {
		t.Fatalf("fetchWorktrees result = %T", message)
	}
	for _, spec := range recorder.specs {
		if strings.Contains(strings.Join(spec.Args, " "), "status") {
			t.Fatalf("initial worktree fetch ran blocking status command: %#v", spec)
		}
	}
}

// TestAddWorktreeForBranch_ExistingBranchChecksOutOrigin confirms the prior
// behavior is preserved when the branch already lives on origin.
func TestAddWorktreeForBranch_ExistingBranchChecksOutOrigin(t *testing.T) {
	recorder := &appGitRunner{out: []byte("abc123\trefs/heads/rename-session\n")}
	restore := system.SetRunner(recorder)
	t.Cleanup(restore)

	if err := addWorktreeForBranch(gitx.Repository{Path: "/repo"}, "/trees/topic", "rename-session"); err != nil {
		t.Fatalf("addWorktreeForBranch: %v", err)
	}
	last := recorder.specs[len(recorder.specs)-1]
	want := system.Spec{Name: "git", Args: []string{"-C", "/repo", "worktree", "add", "/trees/topic", "origin/rename-session"}}
	if !reflect.DeepEqual(last, want) {
		t.Fatalf("existing-branch spec = %#v, want %#v", last, want)
	}
}
