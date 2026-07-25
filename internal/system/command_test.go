package system

import "testing"

type recordingRunner struct {
	output   Spec
	combined Spec
}

func (r *recordingRunner) Output(spec Spec) ([]byte, error) {
	r.output = spec
	return []byte("output"), nil
}

func (r *recordingRunner) CombinedOutput(spec Spec) ([]byte, error) {
	r.combined = spec
	return []byte("combined"), nil
}

func TestCommandPassesCompleteSpecToRunner(t *testing.T) {
	runner := &recordingRunner{}
	restore := SetRunner(runner)
	t.Cleanup(restore)

	command := Command("git", "-C", "/repo", "status")
	command.Dir = "/work"
	output, err := command.Output()
	if err != nil || string(output) != "output" {
		t.Fatalf("Output() = %q, %v", output, err)
	}
	want := Spec{Name: "git", Args: []string{"-C", "/repo", "status"}, Dir: "/work"}
	if runner.output.Name != want.Name || runner.output.Dir != want.Dir || len(runner.output.Args) != len(want.Args) {
		t.Fatalf("runner received %#v, want %#v", runner.output, want)
	}
	for i := range want.Args {
		if runner.output.Args[i] != want.Args[i] {
			t.Fatalf("runner args = %#v, want %#v", runner.output.Args, want.Args)
		}
	}

	if output, err := command.CombinedOutput(); err != nil || string(output) != "combined" {
		t.Fatalf("CombinedOutput() = %q, %v", output, err)
	}
	if runner.combined.Name != want.Name || runner.combined.Dir != want.Dir {
		t.Fatalf("combined runner received %#v, want %#v", runner.combined, want)
	}
}
