package git

import (
	"reflect"
	"testing"

	"github.com/alienxp03/dotfiles/apps/kesh/internal/system"
)

func TestWktreeNewContract(t *testing.T) {
	recorder := &recordingRunner{}
	restore := system.SetRunner(recorder)
	t.Cleanup(restore)
	err := (Wktree{Executable: "/bin/wktree"}).New("/repo", "selected", "feature", []string{"api", "web"})
	if err != nil {
		t.Fatal(err)
	}
	want := system.Spec{
		Name: "/bin/wktree",
		Args: []string{"new", "--workspace", "api", "--workspace", "web", "feature"},
		Dir:  "/repo",
	}
	if !reflect.DeepEqual(recorder.specs, []system.Spec{want}) {
		t.Fatalf("specs = %#v", recorder.specs)
	}
}
