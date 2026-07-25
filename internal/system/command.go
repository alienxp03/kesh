// Package system owns the small boundary between Kesh and operating-system
// processes. Application code describes a command; this package executes it.
package system

import (
	"os/exec"
	"sync"
)

// Spec is a complete, serializable description of a process invocation.
type Spec struct {
	Name string
	Args []string
	Dir  string
}

// Runner executes commands. Consumers should define narrower interfaces when
// they need one; this is intentionally limited to process execution.
type Runner interface {
	Output(Spec) ([]byte, error)
	CombinedOutput(Spec) ([]byte, error)
}

type osRunner struct{}

func (osRunner) Output(spec Spec) ([]byte, error) {
	cmd := exec.Command(spec.Name, spec.Args...)
	cmd.Dir = spec.Dir
	return cmd.Output()
}

func (osRunner) CombinedOutput(spec Spec) ([]byte, error) {
	cmd := exec.Command(spec.Name, spec.Args...)
	cmd.Dir = spec.Dir
	return cmd.CombinedOutput()
}

var (
	runnerMu sync.RWMutex
	runner   Runner = osRunner{}
)

// SetRunner replaces the process runner and returns a restoration function.
// It is intended for deterministic tests and must not be called concurrently
// with command execution.
func SetRunner(next Runner) func() {
	runnerMu.Lock()
	previous := runner
	runner = next
	runnerMu.Unlock()
	return func() {
		runnerMu.Lock()
		runner = previous
		runnerMu.Unlock()
	}
}

func currentRunner() Runner {
	runnerMu.RLock()
	defer runnerMu.RUnlock()
	return runner
}

// Command mirrors the small portion of os/exec.Cmd used by Kesh while keeping
// os/exec out of the application package.
type Process struct {
	name string
	args []string
	Dir  string
}

// NewCommand constructs a command description.
func Command(name string, args ...string) *Process {
	return &Process{name: name, args: append([]string(nil), args...)}
}

func (c *Process) spec() Spec {
	return Spec{Name: c.name, Args: append([]string(nil), c.args...), Dir: c.Dir}
}

func (c *Process) Output() ([]byte, error) { return currentRunner().Output(c.spec()) }
func (c *Process) CombinedOutput() ([]byte, error) {
	return currentRunner().CombinedOutput(c.spec())
}

// LookPath preserves Kesh's command-discovery behavior behind this boundary.
func LookPath(name string) (string, error) { return exec.LookPath(name) }
