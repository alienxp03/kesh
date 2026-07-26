package setup

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alienxp03/kesh/internal/workspace/config"
	"github.com/alienxp03/kesh/internal/workspace/run"
)

func TestCopyFiles(t *testing.T) {
	root := t.TempDir()
	repoRoot := filepath.Join(root, "source")
	worktreePath := filepath.Join(root, "worktree")
	must(t, os.MkdirAll(repoRoot, 0o755))
	must(t, os.MkdirAll(worktreePath, 0o755))
	write(t, filepath.Join(repoRoot, ".env"), "SECRET=1\n")
	logger, stdout, stderr := captureLogger()

	status := CopyFiles(Plan{
		RepoRoot:     repoRoot,
		WorktreePath: worktreePath,
		Copy:         []string{".env", ".missing"},
	}, logger)

	if status != 0 {
		t.Fatalf("status = %d", status)
	}
	if got := read(t, filepath.Join(worktreePath, ".env")); got != "SECRET=1\n" {
		t.Fatalf("copied content = %q", got)
	}
	if !strings.Contains(stdout.String(), "copied .env") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "copy source not found") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestLoggerPrefixesMessages(t *testing.T) {
	logger, stdout, stderr := captureLogger()
	logger.Prefix = "project_a"

	logger.Info("copied %s", ".env")
	logger.Warn("copy destination already exists, skipping: %s", ".env")

	if got := stdout.String(); got != "project_a: copied .env\n" {
		t.Fatalf("stdout = %q", got)
	}
	if got := stderr.String(); got != "warning: project_a: copy destination already exists, skipping: .env\n" {
		t.Fatalf("stderr = %q", got)
	}
}

func TestCopyFilesDoesNotOverwrite(t *testing.T) {
	root := t.TempDir()
	repoRoot := filepath.Join(root, "source")
	worktreePath := filepath.Join(root, "worktree")
	must(t, os.MkdirAll(repoRoot, 0o755))
	must(t, os.MkdirAll(worktreePath, 0o755))
	write(t, filepath.Join(repoRoot, ".env"), "new\n")
	write(t, filepath.Join(worktreePath, ".env"), "existing\n")
	logger, _, stderr := captureLogger()

	status := CopyFiles(Plan{RepoRoot: repoRoot, WorktreePath: worktreePath, Copy: []string{".env"}}, logger)

	if status != 0 {
		t.Fatalf("status = %d", status)
	}
	if got := read(t, filepath.Join(worktreePath, ".env")); got != "existing\n" {
		t.Fatalf("destination overwritten: %q", got)
	}
	if !strings.Contains(stderr.String(), "copy destination already exists") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestSymlinkFiles(t *testing.T) {
	root := t.TempDir()
	repoRoot := filepath.Join(root, "source")
	worktreePath := filepath.Join(root, "worktree")
	must(t, os.MkdirAll(repoRoot, 0o755))
	must(t, os.MkdirAll(worktreePath, 0o755))
	write(t, filepath.Join(repoRoot, ".mcp.json"), "{\"ok\":true}\n")
	logger, stdout, stderr := captureLogger()

	status := SymlinkFiles(Plan{
		RepoRoot:     repoRoot,
		WorktreePath: worktreePath,
		Symlink:      []string{".mcp.json", ".missing"},
	}, logger)

	if status != 0 {
		t.Fatalf("status = %d", status)
	}
	linkPath := filepath.Join(worktreePath, ".mcp.json")
	stat, err := os.Lstat(linkPath)
	if err != nil {
		t.Fatal(err)
	}
	if stat.Mode()&os.ModeSymlink == 0 {
		t.Fatal("expected symlink")
	}
	if got := read(t, linkPath); got != "{\"ok\":true}\n" {
		t.Fatalf("linked content = %q", got)
	}
	if !strings.Contains(stdout.String(), "symlinked .mcp.json") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "symlink source not found") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestSymlinkFilesDoesNotOverwrite(t *testing.T) {
	root := t.TempDir()
	repoRoot := filepath.Join(root, "source")
	worktreePath := filepath.Join(root, "worktree")
	must(t, os.MkdirAll(repoRoot, 0o755))
	must(t, os.MkdirAll(worktreePath, 0o755))
	write(t, filepath.Join(repoRoot, ".mcp.json"), "source\n")
	write(t, filepath.Join(worktreePath, ".mcp.json"), "existing\n")
	logger, _, stderr := captureLogger()

	status := SymlinkFiles(Plan{RepoRoot: repoRoot, WorktreePath: worktreePath, Symlink: []string{".mcp.json"}}, logger)

	if status != 0 {
		t.Fatalf("status = %d", status)
	}
	if got := read(t, filepath.Join(worktreePath, ".mcp.json")); got != "existing\n" {
		t.Fatalf("destination overwritten: %q", got)
	}
	if !strings.Contains(stderr.String(), "symlink destination already exists") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestWriteContextEnv(t *testing.T) {
	root := t.TempDir()
	worktreePath := filepath.Join(root, "worktree")
	must(t, os.MkdirAll(worktreePath, 0o755))
	plan := Plan{
		WorktreePath: worktreePath,
		Context: Context{
			WorkspacePaths: map[string]string{
				"backend":       filepath.Join(root, "backend"),
				"front-end app": filepath.Join(root, "frontend"),
			},
		},
	}

	if err := WriteContextEnv(plan); err != nil {
		t.Fatal(err)
	}
	got := read(t, ContextEnvPath(worktreePath))
	if ContextEnvPath(worktreePath) != filepath.Join(worktreePath, ".kesh.env") {
		t.Fatalf("context env path = %q", ContextEnvPath(worktreePath))
	}
	for _, want := range []string{"KESH_BACKEND_DIR=", "KESH_FRONT_END_APP_DIR="} {
		if !strings.Contains(got, want) {
			t.Fatalf("env missing %q:\n%s", want, got)
		}
	}
	for _, notWant := range []string{"KESH_BRANCH=", "KESH_WORKSPACE=", "KESH_WORKSPACE_DIR="} {
		if strings.Contains(got, notWant) {
			t.Fatalf("env contains noisy var %q:\n%s", notWant, got)
		}
	}
	if !strings.Contains(got, filepath.Join(root, "backend")) || !strings.Contains(got, filepath.Join(root, "frontend")) {
		t.Fatalf("env should use absolute paths:\n%s", got)
	}
}

func TestContextEnvIncludesPullRequestMetadata(t *testing.T) {
	root := t.TempDir()
	worktreePath := filepath.Join(root, "worktree")
	must(t, os.MkdirAll(worktreePath, 0o755))
	plan := Plan{
		WorktreePath: worktreePath,
		Context: Context{
			WorkspacePaths: map[string]string{"app": worktreePath},
			PullRequest: &PullRequestContext{
				Number:  123,
				URL:     "https://github.com/alienxp03/demo/pull/123",
				HeadRef: "contributor/feature",
				HeadSHA: "abc123",
			},
		},
	}

	if err := WriteContextEnv(plan); err != nil {
		t.Fatal(err)
	}
	got := read(t, ContextEnvPath(worktreePath))
	for _, want := range []string{
		"export KESH_PR_NUMBER='123'",
		"export KESH_PR_URL='https://github.com/alienxp03/demo/pull/123'",
		"export KESH_PR_HEAD_REF='contributor/feature'",
		"export KESH_PR_HEAD_SHA='abc123'",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("context env missing %q:\n%s", want, got)
		}
	}
	context, ok, err := ReadPullRequestContext(worktreePath)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || context.Number != 123 || context.URL != "https://github.com/alienxp03/demo/pull/123" || context.HeadRef != "contributor/feature" || context.HeadSHA != "abc123" {
		t.Fatalf("ReadPullRequestContext = %+v, %v", context, ok)
	}
}

func TestContextEnvWorkspaceDirCount(t *testing.T) {
	root := t.TempDir()
	worktreePath := filepath.Join(root, "worktree")
	must(t, os.MkdirAll(worktreePath, 0o755))
	write(t, ContextEnvPath(worktreePath), strings.Join([]string{
		"export KESH_BACKEND_DIR='/tmp/backend'",
		"export KESH_FRONTEND_DIR='/tmp/front'\\''end'",
		"export KESH_WORKSPACE_DIR='/tmp/backend'",
		"export KESH_BAD_DIR=/tmp/unquoted",
		"export KESH_lower_DIR='/tmp/lower'",
		"",
	}, "\n"))

	count, err := ContextEnvWorkspaceDirCount(worktreePath)
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("count = %d", count)
	}

	count, err = ContextEnvWorkspaceDirCount(filepath.Join(root, "missing"))
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("missing count = %d", count)
	}
}

func TestRandomizeEnvPortsRewritesConfiguredVariables(t *testing.T) {
	root := t.TempDir()
	worktreePath := filepath.Join(root, "worktree")
	must(t, os.MkdirAll(worktreePath, 0o755))
	write(t, filepath.Join(worktreePath, ".env.local"), strings.Join([]string{
		"# app env",
		"PORT=3000",
		"export APP_PORT='3001'",
		"OTHER_PORT=9999",
		"",
	}, "\n"))
	logger, stdout, _ := captureLogger()
	ports := portSequence(4100, 4101, 4102)

	status := RandomizeEnvPorts(Plan{
		WorktreePath: worktreePath,
		RandomizePorts: []config.RandomizePort{
			{File: ".env.local", Vars: []string{"PORT", "APP_PORT", "MISSING_PORT"}},
		},
	}, logger, ports)

	if status != 0 {
		t.Fatalf("status = %d", status)
	}
	got := read(t, filepath.Join(worktreePath, ".env.local"))
	for _, want := range []string{"# app env", "PORT=4100", "export APP_PORT=4101", "OTHER_PORT=9999", "MISSING_PORT=4102"} {
		if !strings.Contains(got, want) {
			t.Fatalf("env missing %q:\n%s", want, got)
		}
	}
	if !strings.Contains(stdout.String(), "randomized ports in .env.local") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestRandomizeEnvPortsPreservesExistingPortsWhenReused(t *testing.T) {
	root := t.TempDir()
	worktreePath := filepath.Join(root, "worktree")
	must(t, os.MkdirAll(worktreePath, 0o755))
	write(t, filepath.Join(worktreePath, ".env"), "PORT=4200\nAPP_PORT=not-a-port\n")

	status := RandomizeEnvPorts(Plan{
		WorktreePath:        worktreePath,
		PreserveRandomPorts: true,
		RandomizePorts: []config.RandomizePort{
			{File: ".env", Vars: []string{"PORT", "APP_PORT"}},
		},
	}, Logger{}, portSequence(4300))

	if status != 0 {
		t.Fatalf("status = %d", status)
	}
	got := read(t, filepath.Join(worktreePath, ".env"))
	for _, want := range []string{"PORT=4200", "APP_PORT=4300"} {
		if !strings.Contains(got, want) {
			t.Fatalf("env missing %q:\n%s", want, got)
		}
	}
}

func TestRunRandomizesNewlyCopiedEnvWhenReused(t *testing.T) {
	root := t.TempDir()
	repoRoot := filepath.Join(root, "repo")
	worktreePath := filepath.Join(root, "worktree")
	must(t, os.MkdirAll(repoRoot, 0o755))
	must(t, os.MkdirAll(worktreePath, 0o755))
	write(t, filepath.Join(repoRoot, ".env"), "PORT=1\n")

	status := Run(context.Background(), Plan{
		RepoRoot:            repoRoot,
		WorktreePath:        worktreePath,
		Copy:                []string{".env"},
		PreserveRandomPorts: true,
		RandomizePorts: []config.RandomizePort{
			{File: ".env", Vars: []string{"PORT"}},
		},
		Context: Context{WorkspacePaths: map[string]string{"app": worktreePath}},
	}, Logger{}, ShellRunnerFunc(func(context.Context, string, string, bool) run.Result {
		return run.Result{}
	}))

	if status != 0 {
		t.Fatalf("status = %d", status)
	}
	if got := strings.TrimSpace(read(t, filepath.Join(worktreePath, ".env"))); got == "PORT=1" || !strings.HasPrefix(got, "PORT=") {
		t.Fatalf("newly copied env should be randomized:\n%s", got)
	}
}

func TestSetEnvFilesUsesWorkspaceTemplates(t *testing.T) {
	root := t.TempDir()
	repoAPath := filepath.Join(root, "repo_a")
	repoBPath := filepath.Join(root, "repo_b")
	must(t, os.MkdirAll(repoAPath, 0o755))
	must(t, os.MkdirAll(repoBPath, 0o755))
	write(t, filepath.Join(repoAPath, ".env"), "PORT=58372\n")
	write(t, filepath.Join(repoAPath, ".env.local"), "API_PORT='58373'\n")
	write(t, filepath.Join(repoBPath, ".env"), strings.Join([]string{
		"URL=http://localhost:5001/url",
		"export API_URL='http://localhost:5002/api'",
		"OTHER=${HOST}",
		"",
	}, "\n"))
	logger, stdout, _ := captureLogger()

	status := SetEnvFiles(Plan{
		WorktreePath: repoBPath,
		SetEnv: []config.SetEnv{
			{
				File: ".env",
				Vars: map[string]string{
					"URL":       "http://localhost:${repo_a:PORT}/url",
					"API_URL":   "http://localhost:${repo_a:.env.local:API_PORT}/api",
					"OTHER":     "${HOST}",
					"ADDED_URL": "http://localhost:${repo_a:PORT}/added",
				},
			},
		},
		Context: Context{WorkspacePaths: map[string]string{
			"repo_a": repoAPath,
			"repo_b": repoBPath,
		}},
	}, logger)

	if status != 0 {
		t.Fatalf("status = %d", status)
	}
	got := read(t, filepath.Join(repoBPath, ".env"))
	for _, want := range []string{
		"URL=http://localhost:58372/url",
		"export API_URL='http://localhost:58373/api'",
		"OTHER=${HOST}",
		"ADDED_URL=http://localhost:58372/added",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("env missing %q:\n%s", want, got)
		}
	}
	if !strings.Contains(stdout.String(), "set env in .env") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestSetEnvFilesFailsForMissingTemplateSource(t *testing.T) {
	root := t.TempDir()
	repoBPath := filepath.Join(root, "repo_b")
	must(t, os.MkdirAll(repoBPath, 0o755))
	write(t, filepath.Join(repoBPath, ".env"), "URL=http://localhost:5001/url\n")
	logger, _, stderr := captureLogger()

	status := SetEnvFiles(Plan{
		WorktreePath: repoBPath,
		SetEnv: []config.SetEnv{
			{File: ".env", Vars: map[string]string{"URL": "http://localhost:${repo_a:PORT}/url"}},
		},
		Context: Context{WorkspacePaths: map[string]string{"repo_b": repoBPath}},
	}, logger)

	if status != 1 {
		t.Fatalf("status = %d", status)
	}
	if !strings.Contains(stderr.String(), `unknown workspace "repo_a"`) {
		t.Fatalf("stderr = %q", stderr.String())
	}
	if got := read(t, filepath.Join(repoBPath, ".env")); got != "URL=http://localhost:5001/url\n" {
		t.Fatalf("env should not change on failure: %q", got)
	}
}

func TestRandomizeEnvPortsMissingFileWarnsWithoutFailure(t *testing.T) {
	root := t.TempDir()
	worktreePath := filepath.Join(root, "worktree")
	must(t, os.MkdirAll(worktreePath, 0o755))
	logger, _, stderr := captureLogger()

	status := RandomizeEnvPorts(Plan{
		WorktreePath: worktreePath,
		RandomizePorts: []config.RandomizePort{
			{File: ".env.missing", Vars: []string{"PORT"}},
		},
	}, logger, portSequence(4100))

	if status != 0 {
		t.Fatalf("status = %d", status)
	}
	if !strings.Contains(stderr.String(), "randomize_ports file not found, skipping: .env.missing") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunPostCreateStopsOnFailure(t *testing.T) {
	root := t.TempDir()
	worktreePath := filepath.Join(root, "worktree")
	must(t, os.MkdirAll(worktreePath, 0o755))
	write(t, filepath.Join(worktreePath, ".kesh.env"), "export KESH_APP_DIR='/tmp/app'\n")
	logger, _, stderr := captureLogger()
	calls := []string{}

	status := Run(context.Background(), Plan{
		RepoRoot:     root,
		WorktreePath: worktreePath,
		PostCreate:   []string{"echo ok", "false", "echo later"},
	}, logger, ShellRunnerFunc(func(_ context.Context, command string, cwd string, _ bool) run.Result {
		calls = append(calls, command+"@"+cwd)
		if strings.Contains(command, "false") {
			return run.Result{ExitCode: 1}
		}
		return run.Result{ExitCode: 0}
	}))

	if status != 1 {
		t.Fatalf("status = %d", status)
	}
	if len(calls) != 2 || !strings.Contains(calls[0], "echo ok@"+worktreePath) || !strings.Contains(calls[1], "false@"+worktreePath) {
		t.Fatalf("calls = %#v", calls)
	}
	if !strings.Contains(stderr.String(), "post create command failed") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func portSequence(values ...int) PortAllocator {
	index := 0
	return func() (int, error) {
		if index >= len(values) {
			return 0, nil
		}
		value := values[index]
		index++
		return value, nil
	}
}

func captureLogger() (Logger, *bytes.Buffer, *bytes.Buffer) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	return Logger{Stdout: stdout, Stderr: stderr}, stdout, stderr
}

func write(t *testing.T, filePath string, content string) {
	t.Helper()
	must(t, os.WriteFile(filePath, []byte(content), 0o644))
}

func read(t *testing.T, filePath string) string {
	t.Helper()
	data, err := os.ReadFile(filePath)
	must(t, err)
	return string(data)
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
