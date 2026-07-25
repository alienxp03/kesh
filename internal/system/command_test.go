package system

import (
	"reflect"
	"testing"
)

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
	command.Env = []string{"KESH_TEST=1"}
	command.Stdin = []byte("input\n")
	output, err := command.Output()
	if err != nil || string(output) != "output" {
		t.Fatalf("Output() = %q, %v", output, err)
	}
	want := Spec{
		Name:  "git",
		Args:  []string{"-C", "/repo", "status"},
		Dir:   "/work",
		Env:   []string{"KESH_TEST=1"},
		Stdin: []byte("input\n"),
	}
	if !reflect.DeepEqual(runner.output, want) {
		t.Fatalf("runner received %#v, want %#v", runner.output, want)
	}

	if output, err := command.CombinedOutput(); err != nil || string(output) != "combined" {
		t.Fatalf("CombinedOutput() = %q, %v", output, err)
	}
	if !reflect.DeepEqual(runner.combined, want) {
		t.Fatalf("combined runner received %#v, want %#v", runner.combined, want)
	}
}
