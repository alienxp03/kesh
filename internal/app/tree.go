package app

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/alienxp03/kesh/internal/kitty"
	"github.com/alienxp03/kesh/internal/workspace"
	workspacegit "github.com/alienxp03/kesh/internal/workspace/git"
	"github.com/alienxp03/kesh/internal/workspace/run"
	"github.com/alienxp03/kesh/internal/workspace/setup"
)

type treeCommandOptions struct {
	Cwd    string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	Runner run.Runner
	Kitty  string
}

type treeCommand struct {
	Operation string
	Branch    string
	From      string
	Force     bool
	Yes       bool
	Session   bool
	Window    bool
	JSON      bool
}

type treeResponse struct {
	OK            bool             `json:"ok"`
	Operation     string           `json:"operation"`
	Branch        string           `json:"branch,omitempty"`
	Target        string           `json:"target,omitempty"`
	Workspace     string           `json:"workspace,omitempty"`
	RepoRoot      string           `json:"repo_root,omitempty"`
	ConfigPath    string           `json:"config_path,omitempty"`
	Worktree      string           `json:"worktree,omitempty"`
	WorkspacePath string           `json:"workspace_path,omitempty"`
	Destroyed     bool             `json:"destroyed,omitempty"`
	Opened        bool             `json:"opened,omitempty"`
	Merged        bool             `json:"merged,omitempty"`
	Cancelled     bool             `json:"cancelled,omitempty"`
	Error         *treeResponseErr `json:"error,omitempty"`
}

type treeResponseErr struct {
	Message string `json:"message"`
}

const treeUsage = `usage: kesh tree <new|destroy|merge> ...

Commands:
  kesh tree new <branch> [--from <ref>] [--session|-s|--window|-w] [--json]
  kesh tree destroy <branch> [--force] [-y|--yes] [--json]
  kesh tree merge <branch> [--force] [-y|--yes] [--json]

All commands require .kesh.yaml and operate on its first workspace.
Tree commands are headless by default. tree new --session opens a session
layout; tree new --window opens a new Kitty window in the current tab.
`

func runTreeCommand(args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("find current directory: %w", err)
	}
	kittyPath, _ := commands()
	return runTreeCommandWithOptions(args, treeCommandOptions{
		Cwd:    cwd,
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Kitty:  kittyPath,
	})
}

func runTreeCommandWithOptions(args []string, options treeCommandOptions) error {
	options = normalizeTreeCommandOptions(options)
	if len(args) == 0 || containsTreeHelp(args) {
		_, err := fmt.Fprint(options.Stdout, treeUsage)
		return err
	}

	command, err := parseTreeCommand(args)
	if err != nil {
		if command.JSON {
			emitTreeError(options.Stdout, command.Operation, command.Branch, err)
		}
		return err
	}
	if command.JSON && command.Operation != "new" && !command.Yes {
		err := fmt.Errorf("--json requires -y/--yes for %s", command.Operation)
		emitTreeError(options.Stdout, command.Operation, command.Branch, err)
		return err
	}

	var response treeResponse
	switch command.Operation {
	case "new":
		response, err = runTreeNew(context.Background(), command, options)
	case "destroy":
		response, err = runTreeDestroy(context.Background(), command, options)
	case "merge":
		response, err = runTreeMerge(context.Background(), command, options)
	}
	if err != nil {
		if command.JSON {
			emitTreeError(options.Stdout, command.Operation, command.Branch, err)
		}
		return err
	}
	if command.JSON {
		return emitTreeResponse(options.Stdout, response)
	}
	return nil
}

func normalizeTreeCommandOptions(options treeCommandOptions) treeCommandOptions {
	if options.Cwd == "" {
		options.Cwd, _ = os.Getwd()
	}
	if options.Stdin == nil {
		options.Stdin = os.Stdin
	}
	if options.Stdout == nil {
		options.Stdout = os.Stdout
	}
	if options.Stderr == nil {
		options.Stderr = os.Stderr
	}
	if options.Runner == nil {
		options.Runner = run.DefaultRunner{}
	}
	return options
}

func containsTreeHelp(args []string) bool {
	for _, arg := range args {
		if arg == "--help" || arg == "-h" {
			return true
		}
	}
	return false
}

func parseTreeCommand(args []string) (treeCommand, error) {
	command := treeCommand{}
	if len(args) == 0 {
		return command, fmt.Errorf("%s", treeUsage)
	}
	command.Operation = args[0]
	if command.Operation != "new" && command.Operation != "destroy" && command.Operation != "merge" {
		return command, fmt.Errorf("unknown tree command: %s\n%s", command.Operation, treeUsage)
	}

	var positionals []string
	for index := 1; index < len(args); index++ {
		arg := args[index]
		switch {
		case arg == "--json":
			command.JSON = true
		case arg == "--force":
			if command.Operation == "new" {
				return command, fmt.Errorf("unknown option: %s", arg)
			}
			command.Force = true
		case arg == "--session" || arg == "-s":
			if command.Operation != "new" {
				return command, fmt.Errorf("unknown option: %s", arg)
			}
			if command.Window {
				return command, fmt.Errorf("--session and --window are mutually exclusive")
			}
			command.Session = true
		case arg == "--window" || arg == "-w":
			if command.Operation != "new" {
				return command, fmt.Errorf("unknown option: %s", arg)
			}
			if command.Session {
				return command, fmt.Errorf("--session and --window are mutually exclusive")
			}
			command.Window = true
		case arg == "-y" || arg == "--yes":
			if command.Operation == "new" {
				return command, fmt.Errorf("unknown option: %s", arg)
			}
			command.Yes = true
		case arg == "--from":
			if command.Operation != "new" {
				return command, fmt.Errorf("unknown option: %s", arg)
			}
			index++
			if index >= len(args) || args[index] == "" || strings.HasPrefix(args[index], "-") {
				return command, fmt.Errorf("--from requires a ref")
			}
			command.From = args[index]
		case strings.HasPrefix(arg, "--from="):
			if command.Operation != "new" {
				return command, fmt.Errorf("unknown option: --from")
			}
			command.From = strings.TrimPrefix(arg, "--from=")
			if command.From == "" || strings.HasPrefix(command.From, "-") {
				return command, fmt.Errorf("--from requires a ref")
			}
		case strings.HasPrefix(arg, "-"):
			return command, fmt.Errorf("unknown option: %s", arg)
		default:
			positionals = append(positionals, arg)
		}
	}
	if len(positionals) != 1 || strings.TrimSpace(positionals[0]) == "" {
		return command, fmt.Errorf("usage: kesh tree %s <branch>", command.Operation)
	}
	if strings.HasPrefix(positionals[0], "-") {
		return command, fmt.Errorf("branch name must not start with '-'")
	}
	command.Branch = positionals[0]
	return command, nil
}

func runTreeNew(ctx context.Context, command treeCommand, options treeCommandOptions) (treeResponse, error) {
	shellRunner := setup.StreamShellRunner{Stdout: options.Stderr, Stderr: options.Stderr, Stdin: options.Stdin}
	createOptions := workspace.CreateOptions{
		Cwd:         options.Cwd,
		Branch:      command.Branch,
		From:        command.From,
		Mode:        workspace.ModeSingle,
		Runner:      options.Runner,
		Stdout:      options.Stderr,
		Stderr:      options.Stderr,
		ShellRunner: shellRunner,
	}
	var result workspace.CreateResult
	var err error
	switch {
	case command.Session:
		result, err = workspace.CreateAndOpen(ctx, createOptions)
	case command.Window:
		result, err = workspace.CreateAndOpenWindow(ctx, createOptions)
	default:
		result, err = workspace.CreateHeadless(ctx, createOptions)
	}
	if err != nil {
		return treeResponse{}, err
	}
	if !command.JSON {
		verb := "created"
		if command.Session || command.Window {
			verb = "created and opened"
		}
		fmt.Fprintf(options.Stdout, "%s worktree: %s\n", verb, result.WorkspacePath)
	}
	return treeResponse{
		OK:            true,
		Operation:     "new",
		Branch:        result.Branch,
		Workspace:     result.WorkspaceName,
		RepoRoot:      result.RepoRoot,
		ConfigPath:    result.ConfigPath,
		Worktree:      result.WorktreePath,
		WorkspacePath: result.WorkspacePath,
		Opened:        command.Session || command.Window,
	}, nil
}

func runTreeDestroy(ctx context.Context, command treeCommand, options treeCommandOptions) (treeResponse, error) {
	first, target, kittyIDs, err := resolveTreeDestroy(ctx, command, options)
	if err != nil {
		return treeResponse{}, err
	}
	if !command.JSON {
		printTreeDestroyPlan(options.Stdout, command.Branch, target.WorktreePath)
	}
	confirmed, err := confirmTreeAction(options, command.Yes, "Continue? [y/N] ")
	if err != nil {
		return treeResponse{}, err
	}
	if !confirmed {
		if !command.JSON {
			fmt.Fprintln(options.Stdout, "destroy cancelled")
		}
		return treeResponse{OK: false, Operation: "destroy", Branch: command.Branch, Cancelled: true}, nil
	}
	if err := closeTreeKittyWindows(options, kittyIDs); err != nil {
		return treeResponse{}, err
	}
	if err := setup.RemoveContextEnv(first.WorktreeFolder(target.WorktreePath)); err != nil {
		return treeResponse{}, fmt.Errorf("remove generated workspace env: %w", err)
	}
	if err := workspacegit.RemoveWorktree(ctx, target, command.Force, options.Runner); err != nil {
		return treeResponse{}, err
	}
	if !command.JSON {
		fmt.Fprintf(options.Stdout, "destroyed worktree and branch: %s\n", command.Branch)
	}
	return treeResponse{
		OK:        true,
		Operation: "destroy",
		Branch:    command.Branch,
		RepoRoot:  first.RepoRoot,
		Worktree:  target.WorktreePath,
		Destroyed: true,
	}, nil
}

func runTreeMerge(ctx context.Context, command treeCommand, options treeCommandOptions) (treeResponse, error) {
	first, target, mainPath, defaultBranch, kittyIDs, err := resolveTreeMerge(ctx, command, options)
	if err != nil {
		return treeResponse{}, err
	}
	if !command.JSON {
		printTreeMergePlan(options.Stdout, command.Branch, defaultBranch, mainPath, target.WorktreePath)
	}
	confirmed, err := confirmTreeAction(options, command.Yes, "Continue? [y/N] ")
	if err != nil {
		return treeResponse{}, err
	}
	if !confirmed {
		if !command.JSON {
			fmt.Fprintln(options.Stdout, "merge cancelled")
		}
		return treeResponse{OK: false, Operation: "merge", Branch: command.Branch, Target: defaultBranch, Cancelled: true}, nil
	}
	if err := closeTreeKittyWindows(options, kittyIDs); err != nil {
		return treeResponse{}, err
	}
	if err := workspacegit.EnsureCleanWorktree(ctx, workspacegit.RemoveTarget{WorktreePath: mainPath}, options.Runner); err != nil {
		return treeResponse{}, fmt.Errorf("target repository changed after preflight: %w", err)
	}
	if err := workspacegit.EnsureCleanWorktree(ctx, target, options.Runner); err != nil {
		return treeResponse{}, fmt.Errorf("worktree changed after preflight: %w", err)
	}
	if err := workspacegit.MergeBranch(ctx, mainPath, command.Branch, options.Runner); err != nil {
		return treeResponse{}, fmt.Errorf("merge %s into %s: %w", command.Branch, defaultBranch, err)
	}
	if err := setup.RemoveContextEnv(first.WorktreeFolder(target.WorktreePath)); err != nil {
		return treeResponse{}, fmt.Errorf("merge succeeded but remove generated workspace env failed: %w", err)
	}
	if err := workspacegit.RemoveWorktree(ctx, target, command.Force, options.Runner); err != nil {
		return treeResponse{}, fmt.Errorf("merge succeeded but destroy failed: %w", err)
	}
	if !command.JSON {
		fmt.Fprintf(options.Stdout, "merged %s into %s\n", command.Branch, defaultBranch)
		fmt.Fprintf(options.Stdout, "destroyed worktree and branch: %s\n", command.Branch)
	}
	return treeResponse{
		OK:        true,
		Operation: "merge",
		Branch:    command.Branch,
		Target:    defaultBranch,
		RepoRoot:  first.RepoRoot,
		Worktree:  target.WorktreePath,
		Merged:    true,
		Destroyed: true,
	}, nil
}

func resolveTreeDestroy(ctx context.Context, command treeCommand, options treeCommandOptions) (workspace.FirstWorkspace, workspacegit.RemoveTarget, []int, error) {
	first, err := workspace.LoadFirstWorkspace(ctx, options.Cwd, nil, options.Runner)
	if err != nil {
		return workspace.FirstWorkspace{}, workspacegit.RemoveTarget{}, nil, err
	}
	mainPath, err := workspace.MainWorktreePath(ctx, first.RepoRoot, options.Runner)
	if err != nil {
		return first, workspacegit.RemoveTarget{}, nil, err
	}
	target, err := workspacegit.ResolveRemoveTarget(ctx, workspacegit.RemoveOptions{
		Branch: command.Branch,
		Cwd:    mainPath,
		Force:  command.Force,
		Runner: options.Runner,
	})
	if err != nil {
		return first, workspacegit.RemoveTarget{}, nil, err
	}
	if target.WorktreePath == "" {
		return first, workspacegit.RemoveTarget{}, nil, fmt.Errorf("branch has no linked worktree: %s", command.Branch)
	}
	if !command.Force {
		if err := workspacegit.EnsureCleanWorktree(ctx, target, options.Runner); err != nil {
			return first, workspacegit.RemoveTarget{}, nil, err
		}
	}
	kittyIDs := treeKittyWindowIDs(options.Kitty, target.WorktreePath)
	if len(kittyIDs) > 0 && !command.Force {
		return first, workspacegit.RemoveTarget{}, nil, fmt.Errorf("%d Kitty window(s) are open in this worktree; rerun with --force", len(kittyIDs))
	}
	return first, target, kittyIDs, nil
}

func resolveTreeMerge(ctx context.Context, command treeCommand, options treeCommandOptions) (workspace.FirstWorkspace, workspacegit.RemoveTarget, string, string, []int, error) {
	first, err := workspace.LoadFirstWorkspace(ctx, options.Cwd, nil, options.Runner)
	if err != nil {
		return workspace.FirstWorkspace{}, workspacegit.RemoveTarget{}, "", "", nil, err
	}
	mainPath, err := workspace.MainWorktreePath(ctx, first.RepoRoot, options.Runner)
	if err != nil {
		return first, workspacegit.RemoveTarget{}, "", "", nil, err
	}
	target, err := workspacegit.ResolveCloseTarget(ctx, workspacegit.RemoveOptions{
		Branch: command.Branch,
		Cwd:    mainPath,
		Runner: options.Runner,
	})
	if err != nil {
		return first, workspacegit.RemoveTarget{}, "", "", nil, err
	}
	if target.WorktreePath == "" || sameTreePath(target.WorktreePath, mainPath) {
		return first, workspacegit.RemoveTarget{}, "", "", nil, fmt.Errorf("cannot merge the primary worktree: %s", command.Branch)
	}
	defaultBranch, err := workspacegit.DefaultBranch(ctx, mainPath, options.Runner)
	if err != nil {
		return first, workspacegit.RemoveTarget{}, "", "", nil, err
	}
	currentBranch, err := workspacegit.CurrentBranch(ctx, mainPath, options.Runner)
	if err != nil {
		return first, workspacegit.RemoveTarget{}, "", "", nil, err
	}
	if currentBranch != defaultBranch {
		return first, workspacegit.RemoveTarget{}, "", "", nil, fmt.Errorf("primary repository is on %q; checkout default branch %q before merging", currentBranch, defaultBranch)
	}
	if err := workspacegit.EnsureCleanWorktree(ctx, workspacegit.RemoveTarget{WorktreePath: mainPath}, options.Runner); err != nil {
		return first, workspacegit.RemoveTarget{}, "", "", nil, fmt.Errorf("target repository is not clean: %w", err)
	}
	if err := workspacegit.EnsureCleanWorktree(ctx, target, options.Runner); err != nil {
		return first, workspacegit.RemoveTarget{}, "", "", nil, fmt.Errorf("worktree is not clean: %w", err)
	}
	kittyIDs := treeKittyWindowIDs(options.Kitty, target.WorktreePath)
	if len(kittyIDs) > 0 && !command.Force {
		return first, workspacegit.RemoveTarget{}, "", "", nil, fmt.Errorf("%d Kitty window(s) are open in this worktree; rerun with --force", len(kittyIDs))
	}
	return first, target, mainPath, defaultBranch, kittyIDs, nil
}

func treeKittyWindowIDs(executable, target string) []int {
	if executable == "" {
		return nil
	}
	ids, err := worktreeWindowIDs(executable, target)
	if err != nil {
		// Kitty is optional for headless commands. An unreachable Kitty cannot
		// be a reason to reject a Git-only operation.
		return nil
	}
	return ids
}

func closeTreeKittyWindows(options treeCommandOptions, ids []int) error {
	if len(ids) == 0 || options.Kitty == "" {
		return nil
	}
	client := kitty.Client{Executable: options.Kitty}
	for _, id := range ids {
		if err := client.CloseWindow(id); err != nil {
			return fmt.Errorf("close Kitty window %d: %w", id, err)
		}
	}
	return nil
}

func confirmTreeAction(options treeCommandOptions, yes bool, prompt string) (bool, error) {
	if yes {
		return true, nil
	}
	if _, err := fmt.Fprint(options.Stdout, prompt); err != nil {
		return false, err
	}
	scanner := bufio.NewScanner(options.Stdin)
	if !scanner.Scan() {
		return false, scanner.Err()
	}
	answer := strings.ToLower(strings.TrimSpace(scanner.Text()))
	return answer == "y" || answer == "yes", nil
}

func printTreeDestroyPlan(writer io.Writer, branch, worktreePath string) {
	// Reuse the same layer-aware destroy prompt used by the interactive D flow.
	fmt.Fprintln(writer, destroyPrompt(destroyPlan{
		entryName:    branch,
		worktreePath: worktreePath,
		branch:       branch,
	}))
}

func printTreeMergePlan(writer io.Writer, branch, target, repoPath, worktreePath string) {
	fmt.Fprintf(writer, "Merge %q into %q?\n", branch, target)
	fmt.Fprintf(writer, "  Repository: %s\n", repoPath)
	fmt.Fprintf(writer, "  Worktree:   %s\n", worktreePath)
	fmt.Fprintf(writer, "  • git merge %s\n", branch)
	fmt.Fprintf(writer, "  • Remove worktree\n")
	fmt.Fprintf(writer, "  • Delete local branch %s\n", branch)
}

func sameTreePath(left, right string) bool {
	return filepath.Clean(left) == filepath.Clean(right)
}

func emitTreeResponse(writer io.Writer, response treeResponse) error {
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(response)
}

func emitTreeError(writer io.Writer, operation, branch string, err error) {
	_ = emitTreeResponse(writer, treeResponse{
		OK:        false,
		Operation: operation,
		Branch:    branch,
		Error:     &treeResponseErr{Message: err.Error()},
	})
}
