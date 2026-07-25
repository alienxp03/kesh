package kitty

import (
	"reflect"
	"testing"

	"github.com/alienxp03/kesh/internal/system"
)

type recorder struct {
	specs []system.Spec
	out   []byte
}

func (r *recorder) Output(spec system.Spec) ([]byte, error) {
	r.specs = append(r.specs, spec)
	return r.out, nil
}

func (r *recorder) CombinedOutput(spec system.Spec) ([]byte, error) {
	r.specs = append(r.specs, spec)
	return r.out, nil
}

func TestClientCommandContracts(t *testing.T) {
	runner := &recorder{}
	restore := system.SetRunner(runner)
	t.Cleanup(restore)
	client := Client{Executable: "/bin/kitty"}
	if err := client.FocusWindow(42); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CaptureScreen(42); err != nil {
		t.Fatal(err)
	}
	want := []system.Spec{
		{Name: "/bin/kitty", Args: []string{"@", "focus-window", "--match", "id:42"}},
		{Name: "/bin/kitty", Args: []string{"@", "get-text", "--match", "id:42", "--extent", "screen", "--ansi"}},
	}
	if !reflect.DeepEqual(runner.specs, want) {
		t.Fatalf("specs = %#v, want %#v", runner.specs, want)
	}
}

func TestSaveSessionCommand(t *testing.T) {
	runner := &recorder{}
	restore := system.SetRunner(runner)
	t.Cleanup(restore)
	client := Client{Executable: "/bin/kitty"}
	if err := client.SaveSession("dotfiles", "/state/dotfiles.kitty-session", true); err != nil {
		t.Fatal(err)
	}
	want := system.Spec{
		Name: "/bin/kitty",
		Args: []string{
			"@", "action", "save_as_session", "--save-only", "--use-foreground-process",
			"--match=session:^dotfiles$", "/state/dotfiles.kitty-session",
		},
	}
	if !reflect.DeepEqual(runner.specs, []system.Spec{want}) {
		t.Fatalf("specs = %#v, want %#v", runner.specs, []system.Spec{want})
	}
}
