package run

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

type Options struct {
	Cwd     string
	Env     []string
	Inherit bool
	Stdin   io.Reader
}

type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Err      error
}

type Runner interface {
	Run(ctx context.Context, command string, args []string, options Options) Result
}

type RunnerFunc func(ctx context.Context, command string, args []string, options Options) Result

func (fn RunnerFunc) Run(ctx context.Context, command string, args []string, options Options) Result {
	return fn(ctx, command, args, options)
}

type DefaultRunner struct{}

func (DefaultRunner) Run(ctx context.Context, command string, args []string, options Options) Result {
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Dir = options.Cwd
	cmd.Env = mergedEnv(options.Env)
	cmd.Stdin = options.Stdin

	if options.Inherit {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if cmd.Stdin == nil {
			cmd.Stdin = os.Stdin
		}
		err := cmd.Run()
		return resultFromError(err, "", "")
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	return resultFromError(err, stdout.String(), stderr.String())
}

func FailureMessage(command string, args []string, result Result) string {
	rendered := strings.Join(append([]string{command}, args...), " ")
	detail := strings.TrimSpace(result.Stderr)
	if result.Err != nil && detail == "" {
		detail = result.Err.Error()
	}
	if detail == "" {
		return fmt.Sprintf("%s failed", rendered)
	}
	return fmt.Sprintf("%s failed: %s", rendered, detail)
}

func resultFromError(err error, stdout string, stderr string) Result {
	if err == nil {
		return Result{Stdout: stdout, Stderr: stderr, ExitCode: 0}
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return Result{Stdout: stdout, Stderr: stderr, ExitCode: exitErr.ExitCode(), Err: err}
	}
	return Result{Stdout: stdout, Stderr: stderr, ExitCode: 1, Err: err}
}

func mergedEnv(extra []string) []string {
	if len(extra) == 0 {
		return os.Environ()
	}
	env := os.Environ()
	env = append(env, extra...)
	return env
}
