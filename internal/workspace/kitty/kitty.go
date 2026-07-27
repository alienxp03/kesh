package kitty

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/alienxp03/kesh/internal/workspace/layout"
	"github.com/alienxp03/kesh/internal/workspace/run"
)

const sessionEnv = "KESH_KITTY_SESSION"

var unsafeSessionFileChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func Available(ctx context.Context, runner run.Runner) error {
	if runner == nil {
		runner = run.DefaultRunner{}
	}
	args := []string{"--version"}
	result := runner.Run(ctx, "kitty", args, run.Options{})
	if result.Err != nil || result.ExitCode != 0 {
		return errors.New(run.FailureMessage("kitty", args, result))
	}
	return nil
}

func Check(ctx context.Context, env map[string]string, runner run.Runner) error {
	if runner == nil {
		runner = run.DefaultRunner{}
	}
	if err := Available(ctx, runner); err != nil {
		return err
	}
	_, reachable, err := listWindows(ctx, runner, env)
	if err != nil {
		return err
	}
	if !reachable {
		return fmt.Errorf("kitty remote control is unavailable; run inside Kitty or set KITTY_LISTEN_ON")
	}
	return nil
}

func OpenLayout(ctx context.Context, options layout.OpenOptions) (int, error) {
	runner := options.Runner
	if runner == nil {
		runner = run.DefaultRunner{}
	}
	if err := Available(ctx, runner); err != nil {
		return 1, err
	}
	if err := validateLayout(options); err != nil {
		return 1, err
	}

	windows, reachable, err := listWindows(ctx, runner, options.Env)
	if err != nil {
		return 1, err
	}
	for _, window := range windows {
		if window.Env[sessionEnv] == options.SessionName {
			args := remoteArgs(options.Env, "focus-window", "--match", "id:"+strconv.FormatInt(window.ID, 10))
			result := runner.Run(ctx, "kitty", args, remoteRunOptions(options.Env))
			if result.Err != nil || result.ExitCode != 0 {
				return 1, errors.New(run.FailureMessage("kitty", args, result))
			}
			return 0, nil
		}
	}
	if !reachable && options.Env["KITTY_WINDOW_ID"] == "" && options.Env["KITTY_LISTEN_ON"] == "" {
		return 1, fmt.Errorf("kitty provider requires running inside Kitty or KITTY_LISTEN_ON to open a session")
	}

	content, err := RenderSession(options.SessionName, options.Windows)
	if err != nil {
		return 1, err
	}
	path, err := WriteSessionFile(options.CacheDir, options.SessionName, content)
	if err != nil {
		return 1, err
	}
	args := remoteArgs(options.Env, "action", "goto_session", path)
	result := runner.Run(ctx, "kitty", args, remoteRunOptions(options.Env))
	if result.Err != nil || result.ExitCode != 0 {
		return 1, errors.New(run.FailureMessage("kitty", args, result))
	}
	if err := setLayoutTabTitles(ctx, runner, options); err != nil {
		return 1, err
	}
	return 0, nil
}

// setLayoutTabTitles makes workspace names sticky. Kitty's startup-session
// new_tab title can fall back to the worktree directory after goto_session;
// setting the live tab title explicitly avoids every tab becoming the branch
// name while preserving workspace-specific window titles.
func setLayoutTabTitles(ctx context.Context, runner run.Runner, options layout.OpenOptions) error {
	attempts := 1
	if _, realRunner := runner.(run.DefaultRunner); realRunner {
		attempts = 40
	}
	titledTabs := map[int64]bool{}
	readyPolls := map[int64]int{}
	for attempt := 0; attempt < attempts; attempt++ {
		windows, _, err := listWindows(ctx, runner, options.Env)
		if err != nil {
			return err
		}
		for _, expected := range options.Windows {
			expectedPanes := max(1, len(expected.Commands))
			for _, window := range windows {
				if window.Env[sessionEnv] != options.SessionName || titledTabs[window.TabID] || !samePath(window.CWD, expected.WorktreePath) {
					continue
				}
				paneCount := 0
				for _, candidate := range windows {
					if candidate.TabID == window.TabID && candidate.Env[sessionEnv] == options.SessionName {
						paneCount++
					}
				}
				if paneCount < expectedPanes {
					readyPolls[window.TabID] = 0
					break
				}
				readyPolls[window.TabID]++
				if attempts > 1 && readyPolls[window.TabID] < 5 {
					break
				}
				args := remoteArgs(options.Env, "set-tab-title", "--match", "id:"+strconv.FormatInt(window.TabID, 10), expected.Name)
				result := runner.Run(ctx, "kitty", args, remoteRunOptions(options.Env))
				if result.Err != nil || result.ExitCode != 0 {
					return errors.New(run.FailureMessage("kitty", args, result))
				}
				titledTabs[window.TabID] = true
				break
			}
		}
		if len(titledTabs) == len(options.Windows) {
			return nil
		}
		if attempt < attempts-1 {
			time.Sleep(25 * time.Millisecond)
		}
	}
	if attempts == 1 {
		return nil
	}
	return fmt.Errorf("kitty session %q opened, but its tabs were not ready for naming", options.SessionName)
}

func samePath(left, right string) bool {
	if filepath.Clean(left) == filepath.Clean(right) {
		return true
	}
	leftInfo, leftErr := os.Stat(left)
	rightInfo, rightErr := os.Stat(right)
	return leftErr == nil && rightErr == nil && os.SameFile(leftInfo, rightInfo)
}

func KillLayout(ctx context.Context, options layout.KillOptions) error {
	runner := options.Runner
	if runner == nil {
		runner = run.DefaultRunner{}
	}
	if err := Available(ctx, runner); err != nil {
		return RemoveSessionFile(options.CacheDir, options.SessionName)
	}
	windows, _, err := listWindows(ctx, runner, options.Env)
	if err != nil {
		return err
	}
	tabs := map[int64]bool{}
	for _, window := range windows {
		if window.Env[sessionEnv] == options.SessionName {
			tabs[window.TabID] = true
		}
	}
	ids := make([]int64, 0, len(tabs))
	for id := range tabs {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		args := remoteArgs(options.Env, "close-tab", "--match", "id:"+strconv.FormatInt(id, 10))
		result := runner.Run(ctx, "kitty", args, remoteRunOptions(options.Env))
		// A tab can disappear between listing and closing; Kitty reports that as no match.
		if result.ExitCode == 1 && strings.Contains(strings.ToLower(result.Stderr), "no match") {
			continue
		}
		if result.Err != nil || result.ExitCode != 0 {
			return errors.New(run.FailureMessage("kitty", args, result))
		}
	}
	return RemoveSessionFile(options.CacheDir, options.SessionName)
}

func RenderSession(sessionName string, windows []layout.Window) (string, error) {
	if strings.TrimSpace(sessionName) == "" {
		return "", fmt.Errorf("kitty session name must not be empty")
	}
	if containsLineBreak(sessionName) {
		return "", fmt.Errorf("kitty session name must not contain line breaks")
	}
	var output strings.Builder
	for tabIndex, window := range windows {
		if strings.TrimSpace(window.Name) == "" || strings.TrimSpace(window.WorktreePath) == "" {
			return "", fmt.Errorf("kitty tab name and worktree path must not be empty")
		}
		if containsLineBreak(window.Name) || containsLineBreak(window.WorktreePath) {
			return "", fmt.Errorf("kitty tab name and worktree path must not contain line breaks")
		}
		if tabIndex > 0 {
			// Unlike launch options, Kitty treats the remainder of a new_tab
			// directive as the literal title, so shell quotes would be displayed.
			fmt.Fprintf(&output, "new_tab %s\n", window.Name)
		}
		commands := window.Commands
		if len(commands) == 0 {
			commands = []layout.PaneCommand{{}}
		}
		stableTallLayout := usesTallLayout(commands)
		if stableTallLayout {
			// Tall recreates the common editor-left, stacked-panes-right shape
			// deterministically. Unlike a nested splits tree, it remains intact
			// when Kitty temporarily toggles this tab to another layout.
			fmt.Fprintln(&output, "layout tall")
		} else {
			fmt.Fprintln(&output, "layout splits")
		}
		fmt.Fprintf(&output, "cd %s\n", sessionQuote(window.WorktreePath))
		focusIndex := 0
		for commandIndex, command := range commands {
			if command.Focus {
				focusIndex = commandIndex
			}
		}
		for commandIndex, command := range commands {
			if command.Zoom {
				return "", fmt.Errorf("kitty provider does not support panes[].zoom")
			}
			if command.Size != "" {
				return "", fmt.Errorf("kitty provider does not support panes[].size; use percentage")
			}
			args := []string{"launch", "--title=" + window.Name}
			if commandIndex > 0 && !stableTallLayout {
				location := "vsplit"
				if command.Split == "vertical" {
					location = "hsplit"
				}
				args = append(args, "--location="+location)
				if command.Percentage > 0 {
					args = append(args, "--bias="+strconv.Itoa(command.Percentage))
				}
			}
			args = append(args, "--env", sessionEnv+"="+sessionName, "--cwd", window.WorktreePath)
			if commandIndex == focusIndex {
				args = append(args, "--var", "kesh_focus=yes")
			}
			if commandText := layout.PaneCommandText(command); commandText != "" {
				if containsLineBreak(commandText) {
					return "", fmt.Errorf("kitty provider does not support line breaks in pane commands")
				}
				args = append(args, "/bin/sh", "-c", layout.PaneShellCommand(window.WorktreePath, commandText))
			}
			fmt.Fprintf(&output, "launch %s\n", quoteSessionArgs(args[1:]))
		}
		// A focus directive between launches changes which window the splits
		// layout targets. Defer focus until the complete pane tree exists.
		fmt.Fprintln(&output, "focus_matching_window var:kesh_focus=yes")
		fmt.Fprintln(&output)
	}
	if len(windows) > 0 {
		fmt.Fprintln(&output, "focus_tab 0")
	}
	fmt.Fprintln(&output, "focus_os_window")
	return output.String(), nil
}

func WriteSessionFile(cacheDir string, sessionName string, content string) (string, error) {
	path, err := sessionFilePath(cacheDir, sessionName)
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create Kitty session cache: %w", err)
	}
	temporary, err := os.CreateTemp(dir, ".kitty-session-*")
	if err != nil {
		return "", fmt.Errorf("create Kitty session file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return "", err
	}
	if _, err := temporary.WriteString(content); err != nil {
		temporary.Close()
		return "", fmt.Errorf("write Kitty session file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return "", fmt.Errorf("install Kitty session file: %w", err)
	}
	return path, nil
}

func RemoveSessionFile(cacheDir string, sessionName string) error {
	path, err := sessionFilePath(cacheDir, sessionName)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove Kitty session file: %w", err)
	}
	return nil
}

func sessionFilePath(cacheDir string, sessionName string) (string, error) {
	if cacheDir == "" {
		var err error
		cacheDir, err = os.UserCacheDir()
		if err != nil {
			return "", fmt.Errorf("resolve Kitty session cache: %w", err)
		}
	}
	digest := sha256.Sum256([]byte(sessionName))
	name := strings.Trim(unsafeSessionFileChars.ReplaceAllString(sessionName, "-"), "-._")
	if name == "" {
		name = "session"
	}
	if len(name) > 80 {
		name = name[:80]
	}
	name += "-" + hex.EncodeToString(digest[:4])
	return filepath.Join(cacheDir, "kesh", "kitty", name+".kitty-session"), nil
}

func validateLayout(options layout.OpenOptions) error {
	if options.Mode != "" && options.Mode != layout.ModeWindow && options.Mode != layout.ModeSession {
		return fmt.Errorf("unsupported terminal mode for kitty: %s", options.Mode)
	}
	for _, window := range options.Windows {
		for _, pane := range window.Commands {
			if pane.Zoom {
				return fmt.Errorf("kitty provider does not support panes[].zoom")
			}
			if pane.Size != "" {
				return fmt.Errorf("kitty provider does not support panes[].size; use percentage")
			}
		}
	}
	return nil
}

type listedOSWindow struct {
	Tabs []listedTab `json:"tabs"`
}

type listedTab struct {
	ID      int64          `json:"id"`
	Windows []listedWindow `json:"windows"`
}

type listedWindow struct {
	ID  int64             `json:"id"`
	CWD string            `json:"cwd"`
	Env map[string]string `json:"env"`
}

type sessionWindow struct {
	ID    int64
	TabID int64
	CWD   string
	Env   map[string]string
}

func listWindows(ctx context.Context, runner run.Runner, env map[string]string) ([]sessionWindow, bool, error) {
	args := remoteArgs(env, "ls")
	result := runner.Run(ctx, "kitty", args, remoteRunOptions(env))
	if result.Err != nil || result.ExitCode != 0 {
		if remoteUnavailable(result) {
			return nil, false, nil
		}
		return nil, false, errors.New(run.FailureMessage("kitty", args, result))
	}
	var osWindows []listedOSWindow
	if err := json.Unmarshal([]byte(result.Stdout), &osWindows); err != nil {
		return nil, true, fmt.Errorf("parse kitty @ ls output: %w", err)
	}
	var windows []sessionWindow
	for _, osWindow := range osWindows {
		for _, tab := range osWindow.Tabs {
			for _, window := range tab.Windows {
				windows = append(windows, sessionWindow{ID: window.ID, TabID: tab.ID, CWD: window.CWD, Env: window.Env})
			}
		}
	}
	return windows, true, nil
}

func remoteUnavailable(result run.Result) bool {
	if result.ExitCode != 1 {
		return false
	}
	message := strings.ToLower(result.Stderr)
	return strings.Contains(message, "failed to connect") ||
		strings.Contains(message, "not a tty") ||
		strings.Contains(message, "no controlling terminal")
}

func remoteArgs(env map[string]string, args ...string) []string {
	result := []string{"@"}
	if listenOn := strings.TrimSpace(env["KITTY_LISTEN_ON"]); listenOn != "" {
		result = append(result, "--to", listenOn)
	}
	return append(result, args...)
}

func remoteRunOptions(env map[string]string) run.Options {
	values := []string{}
	for _, key := range []string{"KITTY_LISTEN_ON", "KITTY_WINDOW_ID"} {
		if value := env[key]; value != "" {
			values = append(values, key+"="+value)
		}
	}
	return run.Options{Env: values}
}

func quoteSessionArgs(args []string) string {
	quoted := make([]string, len(args))
	for index, arg := range args {
		quoted[index] = sessionQuote(arg)
	}
	return strings.Join(quoted, " ")
}

func sessionQuote(value string) string {
	return layout.SingleQuote(value)
}

// usesTallLayout recognizes the common nested-split shape declared as a
// horizontal second pane followed by vertical panes. Kitty's tall layout has
// the same geometry (one main pane with a stack beside it), but can be toggled
// away from and back to without losing the shape.
func usesTallLayout(commands []layout.PaneCommand) bool {
	if len(commands) < 3 || commands[1].Split != "horizontal" || commands[1].Percentage > 0 {
		return false
	}
	for _, command := range commands[2:] {
		if command.Split != "vertical" || command.Percentage > 0 {
			return false
		}
	}
	return true
}

func containsLineBreak(value string) bool {
	return strings.ContainsAny(value, "\r\n")
}
