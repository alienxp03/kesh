package git

import (
	"errors"
	"reflect"
	"testing"

	"github.com/alienxp03/kesh/internal/system"
)

type recordingRunner struct {
	specs []system.Spec
	out   []byte
	err   error
}

func (r *recordingRunner) Output(spec system.Spec) ([]byte, error) {
	r.specs = append(r.specs, spec)
	return r.out, r.err
}

func (r *recordingRunner) CombinedOutput(spec system.Spec) ([]byte, error) {
	r.specs = append(r.specs, spec)
	return r.out, r.err
}

func TestRepositoryCommandContracts(t *testing.T) {
	recorder := &recordingRunner{out: []byte("origin/main\n")}
	restore := system.SetRunner(recorder)
	t.Cleanup(restore)

	repository := Repository{Path: "/repo"}
	if got := repository.DefaultBranch(); got != "main" {
		t.Fatalf("default branch = %q", got)
	}
	if err := repository.RemoveWorktree("/trees/feature", true); err != nil {
		t.Fatal(err)
	}
	if err := repository.CreateBranchWorktree("/trees/feature", "topic"); err != nil {
		t.Fatal(err)
	}
	want := []system.Spec{
		{Name: "git", Args: []string{"-C", "/repo", "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"}},
		{Name: "git", Args: []string{"-C", "/repo", "worktree", "remove", "--force", "/trees/feature"}},
		{Name: "git", Args: []string{"-C", "/repo", "worktree", "add", "-b", "topic", "/trees/feature"}},
	}
	if !reflect.DeepEqual(recorder.specs, want) {
		t.Fatalf("specs = %#v, want %#v", recorder.specs, want)
	}
}

func TestCommandErrorPreservesOutput(t *testing.T) {
	recorder := &recordingRunner{out: []byte("dirty worktree\n"), err: errors.New("exit 128")}
	restore := system.SetRunner(recorder)
	t.Cleanup(restore)
	err := (Repository{Path: "/repo"}).RemoveWorktree("/tree", false)
	if err == nil || err.Error() != "git worktree remove: exit 128: dirty worktree" {
		t.Fatalf("error = %v", err)
	}
}
