package catalog

import (
	"reflect"
	"testing"

	"github.com/alienxp03/dotfiles/apps/kesh/internal/system"
)

type fakeRunner struct {
	specs []system.Spec
	out   []byte
}

func (f *fakeRunner) Output(spec system.Spec) ([]byte, error) {
	f.specs = append(f.specs, spec)
	return f.out, nil
}

func (f *fakeRunner) CombinedOutput(spec system.Spec) ([]byte, error) {
	f.specs = append(f.specs, spec)
	return f.out, nil
}

func TestZoxideContracts(t *testing.T) {
	runner := &fakeRunner{out: []byte("/one\n/two\n")}
	restore := system.SetRunner(runner)
	t.Cleanup(restore)
	zoxide := Zoxide{Executable: "/bin/zoxide"}
	if _, err := zoxide.Query(); err != nil {
		t.Fatal(err)
	}
	if err := zoxide.Add("/three"); err != nil {
		t.Fatal(err)
	}
	want := []system.Spec{
		{Name: "/bin/zoxide", Args: []string{"query", "-l"}},
		{Name: "/bin/zoxide", Args: []string{"add", "--", "/three"}},
	}
	if !reflect.DeepEqual(runner.specs, want) {
		t.Fatalf("specs = %#v, want %#v", runner.specs, want)
	}
}
