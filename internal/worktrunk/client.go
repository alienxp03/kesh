// Package worktrunk delegates worktree lifecycle and hook trust to Worktrunk.
package worktrunk

import (
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/alienxp03/kesh/internal/system"
)

// Execute keeps both process boundaries (UI and headless CLI) testable.
type Execute func(dir string, args []string) (stdout, stderr string, err error)

type Client struct {
	Path    string
	Execute Execute
}

type Item struct {
	Branch   string `json:"branch"`
	Worktree *struct {
		Path string `json:"path"`
		Main bool   `json:"main"`
	} `json:"worktree"`
	Remote string `json:"remote"`
}

func (c Client) command(args ...string) ([]byte, error) {
	var out, detail string
	var err error
	if c.Execute != nil {
		out, detail, err = c.Execute(c.Path, args)
	} else {
		cmd := system.Command("wt", args...)
		cmd.Dir = c.Path
		var data []byte
		data, err = cmd.Output()
		out = string(data)
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			detail = string(exit.Stderr)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("wt %s: %w %s (Worktrunk is required; for hook approval run: wt -C %q config approvals add)", strings.Join(args, " "), err, strings.TrimSpace(detail), c.Path)
	}
	return []byte(out), nil
}

// CheckApproval asks Worktrunk to validate its stored approvals on every action.
// Never run an action when trust is missing, even if a wt version would skip hooks.
func (c Client) CheckApproval() error {
	data, err := c.command("config", "approvals", "list", "--format=json")
	if err != nil {
		return err
	}
	var state struct {
		State string `json:"state"`
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return fmt.Errorf("read Worktrunk approvals: %w", err)
	}
	switch state.State {
	case "approved", "no_commands":
		return nil
	case "approval_required":
		return fmt.Errorf("review project hooks in a terminal: wt -C %q config approvals add; then retry (Worktrunk asks again when hooks change)", c.Path)
	default:
		return fmt.Errorf("unknown Worktrunk approval state %q; update wt", state.State)
	}
}

func (c Client) List(branches bool) ([]Item, error) {
	args := []string{"--config-set", "list.json-schema=2", "list", "--format=json"}
	if branches {
		args = append(args, "--branches", "--remotes")
	}
	data, err := c.command(args...)
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Schema int    `json:"schema"`
		Items  []Item `json:"items"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("read wt list JSON: %w", err)
	}
	if envelope.Schema != 2 {
		return nil, fmt.Errorf("wt list schema %d is not supported; schema 2 required (update wt)", envelope.Schema)
	}
	return envelope.Items, nil
}

func (c Client) Find(branch string) (Item, error) {
	items, err := c.List(false)
	if err != nil {
		return Item{}, err
	}
	for _, item := range items {
		if item.Branch == branch && item.Worktree != nil && filepath.IsAbs(item.Worktree.Path) {
			return item, nil
		}
	}
	return Item{}, fmt.Errorf("no Worktrunk worktree for %q", branch)
}

// Switch opens an existing local/remote branch or creates a new branch.
// Worktrunk chooses the base and path; from overrides the base for a new branch.
func (c Client) Switch(branch, from string) (string, error) {
	if branch == "" || strings.HasPrefix(branch, "-") {
		return "", fmt.Errorf("branch name is required and must not start with '-'")
	}
	items, err := c.List(true)
	if err != nil {
		return "", err
	}
	exists := false
	for _, item := range items {
		if item.Branch == branch {
			exists = true
		}
	}
	args := []string{"switch", "--no-cd", "--format=json"}
	if !exists {
		args = append(args, "--create")
		if from != "" {
			args = append(args, "--base", from)
		}
	}
	args = append(args, branch)
	return c.switchPath(args, branch)
}

// SwitchTarget lets Worktrunk resolve paths and PR URLs, including fork refs.
func (c Client) SwitchTarget(target string) (string, error) {
	if target == "" || strings.HasPrefix(target, "-") {
		return "", fmt.Errorf("invalid Worktrunk target %q", target)
	}
	return c.switchPath([]string{"switch", "--no-cd", "--format=json", target}, "")
}

func (c Client) switchPath(args []string, branch string) (string, error) {
	if err := c.CheckApproval(); err != nil {
		return "", err
	}
	data, err := c.command(args...)
	if err != nil {
		return "", err
	}
	var result struct {
		Path string `json:"path"`
	}
	if json.Unmarshal(data, &result) == nil && filepath.IsAbs(result.Path) {
		return result.Path, nil
	}
	// Use wt's actual list path, never a guessed path or template.
	if branch != "" {
		item, err := c.Find(branch)
		if err != nil {
			return "", err
		}
		return item.Worktree.Path, nil
	}
	return "", fmt.Errorf("wt switch returned no absolute path; worktree may exist; inspect wt list before retrying")
}

func (c Client) Remove(path string, force, deleteUnmerged bool) error {
	if err := c.CheckApproval(); err != nil {
		return err
	}
	args := []string{"remove", "--foreground", "--config-set", "switch.cd=false"}
	if force {
		args = append(args, "--force")
	}
	if deleteUnmerged {
		args = append(args, "--force-delete")
	}
	_, err := c.command(append(args, "--", path)...)
	return err
}

// Merge keeps the old clean, non-squashing CLI policy. wt owns merge and removal.
func (c Client) Merge(target string) error {
	if err := c.CheckApproval(); err != nil {
		return err
	}
	data, err := c.command("merge", "--no-commit", "--no-squash", "--no-rebase", "--format=json", "--config-set", "merge.verify=true", "--config-set", "merge.remove=true", "--config-set", "switch.cd=false", "--", target)
	if err != nil {
		return err
	}
	var result struct {
		Removed *bool `json:"removed"`
	}
	if err := json.Unmarshal(data, &result); err != nil || result.Removed == nil {
		return fmt.Errorf("merge returned no cleanup result; inspect wt list before retrying")
	}
	if !*result.Removed {
		return fmt.Errorf("merge succeeded but Worktrunk kept the worktree; inspect wt list")
	}
	return nil
}
