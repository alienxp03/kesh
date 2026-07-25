// Package system owns the small boundary between Kesh and operating-system
// processes. Application code describes a command; this package executes it.
package system

import (
	"bytes"
	"os"
	"os/exec"
	"sync"
	"syscall"
)

// Spec is a complete, serializable description of a process invocation.
type Spec struct {
	Name  string
	Args  []string
	Dir   string
	Env   []string
	Stdin []byte
}

// Runner executes commands. Consumers should define narrower interfaces when
// they need one; this is intentionally limited to process execution.
type Runner interface {
	Output(Spec) ([]byte, error)
	CombinedOutput(Spec) ([]byte, error)
}

type osRunner struct{}

func commandFor(spec Spec) *exec.Cmd {
	cmd := exec.Command(spec.Name, spec.Args...)
	cmd.Dir = spec.Dir
	if len(spec.Env) > 0 {
		cmd.Env = append(os.Environ(), spec.Env...)
	}
	if len(spec.Stdin) > 0 {
		cmd.Stdin = bytes.NewReader(spec.Stdin)
	}
	return cmd
}

func (osRunner) Output(spec Spec) ([]byte, error) { return commandFor(spec).Output() }

func (osRunner) CombinedOutput(spec Spec) ([]byte, error) {
	return commandFor(spec).CombinedOutput()
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
	name  string
	args  []string
	Dir   string
	Env   []string
	Stdin []byte
}

// NewCommand constructs a command description.
func Command(name string, args ...string) *Process {
	return &Process{name: name, args: append([]string(nil), args...)}
}

func (c *Process) spec() Spec {
	return Spec{
		Name:  c.name,
		Args:  append([]string(nil), c.args...),
		Dir:   c.Dir,
		Env:   append([]string(nil), c.Env...),
		Stdin: append([]byte(nil), c.Stdin...),
	}
}

func (c *Process) Output() ([]byte, error) { return currentRunner().Output(c.spec()) }
func (c *Process) CombinedOutput() ([]byte, error) {
	return currentRunner().CombinedOutput(c.spec())
}

// LookPath preserves Kesh's command-discovery behavior behind this boundary.
func LookPath(name string) (string, error) { return exec.LookPath(name) }

// ProcessRunning reports whether a process exists without sending it a signal.
func ProcessRunning(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}
