package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectPath(t *testing.T) {
	if ProjectPath("/repo") != filepath.Join("/repo", ".kesh.yaml") {
		t.Fatalf("project path = %q", ProjectPath("/repo"))
	}
}

func TestProjectTemplate(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, ".kesh.yaml")
	write(t, configPath, ProjectTemplate())

	loaded, err := LoadFile(configPath, filepath.Join(root, "home"))
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Worktree.Dir != "" {
		t.Fatalf("config = %#v", loaded)
	}
	if len(loaded.Workspaces) != 1 || loaded.Workspaces[0].Name != "window_name" || loaded.Workspaces[0].Repo != "." {
		t.Fatalf("workspaces = %#v", loaded.Workspaces)
	}
	template := ProjectTemplate()
	for _, want := range []string{"# worktree:", "#   dir: ~/workspace/worktrees", "#   defaults:", "#     files:", "#   files:", "#   hooks:", "#     post_create:", "#   randomize_ports:", "#         - PORT", "#   set_env:", "#         API_URL:", "# panes:"} {
		if !strings.Contains(template, want) {
			t.Fatalf("template missing %q:\n%s", want, template)
		}
	}
}

func TestWriteProjectTemplateRefusesExistingConfig(t *testing.T) {
	root := t.TempDir()
	configPath, err := WriteProjectTemplate(root)
	if err != nil {
		t.Fatal(err)
	}
	if configPath != ProjectPath(root) {
		t.Fatalf("config path = %q", configPath)
	}
	if _, err := WriteProjectTemplate(root); err == nil || !strings.Contains(err.Error(), "config already exists") {
		t.Fatalf("err = %v", err)
	}
}

func TestFindProjectPathUsesNearestConfigWithinRepo(t *testing.T) {
	root := t.TempDir()
	projectRoot := filepath.Join(root, "projects", "project_a")
	startDir := filepath.Join(projectRoot, "src")
	must(t, os.MkdirAll(startDir, 0o755))
	rootConfig := filepath.Join(root, ".kesh.yaml")
	projectConfig := filepath.Join(projectRoot, ".kesh.yaml")
	write(t, rootConfig, "workspaces:\n  - name: root\n")
	write(t, projectConfig, "workspaces:\n  - name: project_a\n")

	got, found, err := FindProjectPath(startDir, root)
	if err != nil {
		t.Fatal(err)
	}
	if !found || got != projectConfig {
		t.Fatalf("project config = %q found=%v", got, found)
	}

	must(t, os.Remove(projectConfig))
	got, found, err = FindProjectPath(startDir, root)
	if err != nil {
		t.Fatal(err)
	}
	if !found || got != rootConfig {
		t.Fatalf("root config = %q found=%v", got, found)
	}

	must(t, os.Remove(rootConfig))
	got, found, err = FindProjectPath(startDir, root)
	if err != nil {
		t.Fatal(err)
	}
	if found || got != rootConfig {
		t.Fatalf("missing config = %q found=%v", got, found)
	}
}

func TestFindProjectPathAcceptsEquivalentFilesystemPaths(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repo")
	startDir := filepath.Join(root, "src")
	must(t, os.MkdirAll(startDir, 0o755))
	write(t, filepath.Join(root, ".kesh.yaml"), "workspaces:\n  - name: root\n")

	alias := filepath.Join(t.TempDir(), "repo-alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	got, found, err := FindProjectPath(filepath.Join(alias, "src"), root)
	if err != nil {
		t.Fatal(err)
	}
	if !found || got != filepath.Join(alias, ".kesh.yaml") {
		t.Fatalf("project config = %q found=%v", got, found)
	}
}

func TestFindProjectPathRejectsDirectoryOutsideRepo(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "repo")
	outside := filepath.Join(base, "outside")
	must(t, os.MkdirAll(root, 0o755))
	must(t, os.MkdirAll(outside, 0o755))

	if _, _, err := FindProjectPath(outside, root); err == nil || !strings.Contains(err.Error(), "start directory must be inside git repo root") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadProjectConfig(t *testing.T) {
	root := t.TempDir()
	sourceRoot := filepath.Join(root, "source")
	must(t, os.MkdirAll(sourceRoot, 0o755))
	write(t, filepath.Join(sourceRoot, ".kesh.yaml"), strings.Join([]string{
		"worktree:",
		"  dir: ~/worktree",
		"  defaults:",
		"    files:",
		"      copy:",
		"        - .env",
		"workspaces:",
		"  - name: backend",
		"    panes:",
		"      - commands:",
		"          - nvim",
		"        focus: true",
		"      - commands:",
		"          - pnpm install",
		"          - pnpm run dev",
		"        split: horizontal",
		"  - name: frontend",
		"    repo: ../frontend",
		"    worktree:",
		"      files:",
		"        symlink:",
		"          - AGENTS.override.md",
		"      hooks:",
		"        post_create:",
		"          - pnpm install",
		"      randomize_ports:",
		"        - file: .env.local",
		"          vars:",
		"            - PORT",
		"            - APP_PORT",
		"      set_env:",
		"        - file: .env.local",
		"          vars:",
		"            API_URL: http://localhost:${backend:PORT}/api",
		"",
	}, "\n"))

	config, err := LoadProject(sourceRoot, filepath.Join(root, "home"))
	if err != nil {
		t.Fatal(err)
	}
	if config.Worktree.Dir != "~/worktree" {
		t.Fatalf("config = %#v", config)
	}
	if len(config.Workspaces) != 2 || config.Workspaces[0].Name != "backend" || config.Workspaces[1].Repo != "../frontend" {
		t.Fatalf("workspaces = %#v", config.Workspaces)
	}
	panes := WorkspacePanes(config.Workspaces[0])
	if len(panes) != 2 || len(panes[0].Commands) != 1 || panes[0].Commands[0] != "nvim" || panes[1].Split != "horizontal" {
		t.Fatalf("panes = %#v", panes)
	}
	if !config.HasSetup() {
		t.Fatal("expected setup")
	}
	files := WorkspaceFiles(config, config.Workspaces[1])
	assertSlice(t, files.Copy, []string{".env"})
	assertSlice(t, files.Symlink, []string{"AGENTS.override.md"})
	hooks := WorkspaceHooks(config, config.Workspaces[1])
	assertSlice(t, hooks.PostCreate, []string{"pnpm install"})
	randomizePorts := config.Workspaces[1].Worktree.RandomizePorts
	if len(randomizePorts) != 1 || randomizePorts[0].File != ".env.local" {
		t.Fatalf("randomize ports = %#v", randomizePorts)
	}
	assertSlice(t, randomizePorts[0].Vars, []string{"PORT", "APP_PORT"})
	setEnv := config.Workspaces[1].Worktree.SetEnv
	if len(setEnv) != 1 || setEnv[0].File != ".env.local" || setEnv[0].Vars["API_URL"] != "http://localhost:${backend:PORT}/api" {
		t.Fatalf("set env = %#v", setEnv)
	}
}

func TestLoadProjectConfigCommandsAlias(t *testing.T) {
	root := t.TempDir()
	sourceRoot := filepath.Join(root, "source")
	must(t, os.MkdirAll(sourceRoot, 0o755))
	write(t, filepath.Join(sourceRoot, ".kesh.yaml"), strings.Join([]string{
		"workspaces:",
		"  - name: backend",
		"    commands:",
		"      - commands:",
		"          - nvim",
		"",
	}, "\n"))

	config, err := LoadProject(sourceRoot, filepath.Join(root, "home"))
	if err != nil {
		t.Fatal(err)
	}
	panes := WorkspacePanes(config.Workspaces[0])
	if len(panes) != 1 || len(panes[0].Commands) != 1 || panes[0].Commands[0] != "nvim" {
		t.Fatalf("panes = %#v", panes)
	}
}

func TestLoadFileMissingAndEmpty(t *testing.T) {
	root := t.TempDir()
	config, err := LoadFile(filepath.Join(root, "missing.yaml"), filepath.Join(root, "home"))
	if err != nil {
		t.Fatal(err)
	}
	if config.HasSetup() {
		t.Fatal("missing config should be empty")
	}

	empty := filepath.Join(root, "empty.yaml")
	write(t, empty, "\n")
	config, err = LoadFile(empty, filepath.Join(root, "home"))
	if err != nil {
		t.Fatal(err)
	}
	if config.HasSetup() {
		t.Fatal("empty config should be empty")
	}
}

func TestLoadFileAllowsShellOnlyPane(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "shell-only.yaml")
	write(t, path, "workspaces:\n  - name: app\n    panes:\n      - split: horizontal\n      - commands:\n          - nvim\n")

	config, err := LoadFile(path, filepath.Join(root, "home"))
	if err != nil {
		t.Fatal(err)
	}
	panes := WorkspacePanes(config.Workspaces[0])
	if len(panes) != 2 || panes[0].Commands != nil || panes[0].Split != "horizontal" {
		t.Fatalf("shell-only pane = %#v", panes[0])
	}
}

func TestLoadFileRejectsInvalidConfig(t *testing.T) {
	root := t.TempDir()
	invalidYAML := filepath.Join(root, "invalid.yaml")
	legacyCopy := filepath.Join(root, "legacy-copy.yaml")
	legacyMode := filepath.Join(root, "legacy-mode.yaml")
	unsupported := filepath.Join(root, "unsupported.yaml")
	duplicateWorkspace := filepath.Join(root, "duplicate-workspace.yaml")
	duplicateWorkspaceEnv := filepath.Join(root, "duplicate-workspace-env.yaml")
	emptyWorkspaceEnv := filepath.Join(root, "empty-workspace-env.yaml")
	singularCommand := filepath.Join(root, "singular-command.yaml")
	bothPaneKeys := filepath.Join(root, "both-pane-keys.yaml")
	defaultHooks := filepath.Join(root, "default-hooks.yaml")
	badSplit := filepath.Join(root, "bad-split.yaml")
	unsafeCopy := filepath.Join(root, "unsafe-copy.yaml")
	unsafeRandomizePortFile := filepath.Join(root, "unsafe-randomize-port-file.yaml")
	emptyRandomizePortVars := filepath.Join(root, "empty-randomize-port-vars.yaml")
	badRandomizePortVar := filepath.Join(root, "bad-randomize-port-var.yaml")
	duplicateRandomizePortVar := filepath.Join(root, "duplicate-randomize-port-var.yaml")
	unsafeSetEnvFile := filepath.Join(root, "unsafe-set-env-file.yaml")
	emptySetEnvVars := filepath.Join(root, "empty-set-env-vars.yaml")
	badSetEnvVar := filepath.Join(root, "bad-set-env-var.yaml")
	emptySetEnvTemplate := filepath.Join(root, "empty-set-env-template.yaml")

	write(t, invalidYAML, "workspaces: [\n")
	write(t, legacyCopy, "copy:\n  - .env\n")
	write(t, legacyMode, "mode: session\n")
	write(t, unsupported, "commands:\n  - pnpm install\n")
	write(t, duplicateWorkspace, "workspaces:\n  - name: app\n  - name: app\n")
	write(t, duplicateWorkspaceEnv, "workspaces:\n  - name: front-end\n  - name: front end\n")
	write(t, emptyWorkspaceEnv, "workspaces:\n  - name: '---'\n")
	write(t, singularCommand, "workspaces:\n  - name: app\n    panes:\n      - command: nvim\n")
	write(t, bothPaneKeys, "workspaces:\n  - name: app\n    panes:\n      - commands:\n          - nvim\n    commands:\n      - commands:\n          - codex\n")
	write(t, defaultHooks, "worktree:\n  defaults:\n    hooks:\n      post_create:\n        - pnpm install\n")
	write(t, badSplit, "workspaces:\n  - name: app\n    panes:\n      - commands:\n          - nvim\n        split: diagonal\n")
	write(t, unsafeCopy, "worktree:\n  defaults:\n    files:\n      copy:\n        - ../.env\n")
	write(t, unsafeRandomizePortFile, "workspaces:\n  - name: app\n    worktree:\n      randomize_ports:\n        - file: ../.env\n          vars:\n            - PORT\n")
	write(t, emptyRandomizePortVars, "workspaces:\n  - name: app\n    worktree:\n      randomize_ports:\n        - file: .env\n")
	write(t, badRandomizePortVar, "workspaces:\n  - name: app\n    worktree:\n      randomize_ports:\n        - file: .env\n          vars:\n            - APP-PORT\n")
	write(t, duplicateRandomizePortVar, "workspaces:\n  - name: app\n    worktree:\n      randomize_ports:\n        - file: .env\n          vars:\n            - PORT\n            - PORT\n")
	write(t, unsafeSetEnvFile, "workspaces:\n  - name: app\n    worktree:\n      set_env:\n        - file: ../.env\n          vars:\n            URL: http://localhost:3000\n")
	write(t, emptySetEnvVars, "workspaces:\n  - name: app\n    worktree:\n      set_env:\n        - file: .env\n")
	write(t, badSetEnvVar, "workspaces:\n  - name: app\n    worktree:\n      set_env:\n        - file: .env\n          vars:\n            APP-URL: http://localhost:3000\n")
	write(t, emptySetEnvTemplate, "workspaces:\n  - name: app\n    worktree:\n      set_env:\n        - file: .env\n          vars:\n            URL: ''\n")

	loadErrorContains(t, invalidYAML, "invalid YAML")
	loadErrorContains(t, legacyCopy, "legacy key")
	loadErrorContains(t, legacyMode, "legacy key")
	loadErrorContains(t, unsupported, "unsupported key")
	loadErrorContains(t, duplicateWorkspace, "duplicate workspace name")
	loadErrorContains(t, duplicateWorkspaceEnv, "workspace env var")
	loadErrorContains(t, emptyWorkspaceEnv, "at least one letter or digit")
	loadErrorContains(t, singularCommand, "field command not found")
	loadErrorContains(t, bothPaneKeys, "panes or commands")
	loadErrorContains(t, defaultHooks, "worktree.defaults.hooks is not supported")
	loadErrorContains(t, badSplit, "split")
	loadErrorContains(t, unsafeCopy, `cannot contain ".."`)
	loadErrorContains(t, unsafeRandomizePortFile, `cannot contain ".."`)
	loadErrorContains(t, emptyRandomizePortVars, "must define at least one variable")
	loadErrorContains(t, badRandomizePortVar, "valid env var name")
	loadErrorContains(t, duplicateRandomizePortVar, "duplicate randomize port var")
	loadErrorContains(t, unsafeSetEnvFile, `cannot contain ".."`)
	loadErrorContains(t, emptySetEnvVars, "must define at least one variable")
	loadErrorContains(t, badSetEnvVar, "valid env var name")
	if _, err := LoadFile(emptySetEnvTemplate, filepath.Join(root, "home")); err != nil {
		t.Fatalf("empty set_env value should be valid: %v", err)
	}
}

func TestWorkspaceDirEnvKey(t *testing.T) {
	got, err := WorkspaceDirEnvKey("front-end app")
	if err != nil {
		t.Fatal(err)
	}
	if got != "KESH_FRONT_END_APP_DIR" {
		t.Fatalf("key = %q", got)
	}
	got, err = WorkspaceDirEnvKey("123")
	if err != nil {
		t.Fatal(err)
	}
	if got != "KESH_123_DIR" {
		t.Fatalf("numeric key = %q", got)
	}
	if _, err := WorkspaceDirEnvKey("---"); err == nil {
		t.Fatal("expected empty sanitized name error")
	}
}

func TestExpandConfiguredPath(t *testing.T) {
	root := t.TempDir()
	got, err := ExpandConfiguredPath("~/repo", filepath.Join(root, "home"), root)
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(root, "home", "repo") {
		t.Fatalf("expanded = %q", got)
	}

	got, err = ExpandConfiguredPath("../repo", filepath.Join(root, "home"), filepath.Join(root, "config"))
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(root, "repo") {
		t.Fatalf("relative expanded = %q", got)
	}
}

func assertSlice(t *testing.T, got []string, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %#v, want %#v", got, want)
		}
	}
}

func loadErrorContains(t *testing.T, filePath string, want string) {
	t.Helper()
	_, err := LoadFile(filePath, filepath.Join(filepath.Dir(filePath), "home"))
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want containing %q", err, want)
	}
}

func write(t *testing.T, filePath string, content string) {
	t.Helper()
	must(t, os.WriteFile(filePath, []byte(content), 0o644))
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
