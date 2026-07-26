package zoxide

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/alienxp03/kesh/internal/workspace/run"
)

func TestAvailableAndAdd(t *testing.T) {
	var calls [][]string
	runner := run.RunnerFunc(func(_ context.Context, command string, args []string, _ run.Options) run.Result {
		if command != "zoxide" {
			t.Fatalf("command = %q", command)
		}
		calls = append(calls, append([]string(nil), args...))
		return run.Result{}
	})
	if err := Available(context.Background(), runner); err != nil {
		t.Fatal(err)
	}
	paths := []string{"/tmp/backend", "/tmp/front end"}
	if err := Add(context.Background(), paths, runner); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"--version"}, {"add", "--", "/tmp/backend", "/tmp/front end"}}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
}

func TestAddFailure(t *testing.T) {
	runner := run.RunnerFunc(func(context.Context, string, []string, run.Options) run.Result {
		return run.Result{ExitCode: 1, Err: errors.New("exit status 1"), Stderr: "database unavailable"}
	})
	if err := Add(context.Background(), []string{"/tmp/app"}, runner); err == nil || !strings.Contains(err.Error(), "database unavailable") {
		t.Fatalf("error = %v", err)
	}
}
