package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/alienxp03/kesh/internal/system"
	"github.com/alienxp03/kesh/internal/workspace/run"
)

// wtShellTestRunner keeps existing shell-based UI tests independent of installed wt.
// All non-wt commands still use their test scripts.
type wtShellTestRunner struct{}

func (r wtShellTestRunner) Output(spec system.Spec) ([]byte, error) { return r.CombinedOutput(spec) }
func (wtShellTestRunner) CombinedOutput(spec system.Spec) ([]byte, error) {
	if spec.Name == "wt" {
		switch spec.Args[0] {
		case "config":
			return []byte(`{"state":"approved"}`), nil
		case "switch":
			return json.Marshal(map[string]string{"path": spec.Args[len(spec.Args)-1]})
		case "remove":
			file, err := os.OpenFile(os.Getenv("REMOVE_CAPTURE"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
			if err != nil {
				return nil, err
			}
			defer file.Close()
			_, err = fmt.Fprintln(file, strings.Join(spec.Args, " "))
			return nil, err
		}
		return nil, fmt.Errorf("unexpected wt command %v", spec.Args)
	}
	result := run.DefaultRunner{}.Run(context.Background(), spec.Name, spec.Args, run.Options{Cwd: spec.Dir, Env: spec.Env, Stdin: strings.NewReader(string(spec.Stdin))})
	return []byte(result.Stdout + result.Stderr), result.Err
}
