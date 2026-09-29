// Package github owns GitHub CLI command construction and output parsing.
package github

import (
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/alienxp03/kesh/internal/domain"
	gitx "github.com/alienxp03/kesh/internal/git"
	"github.com/alienxp03/kesh/internal/system"
)

type Client struct {
	Executable string
}

// ghJSONOutput keeps diagnostics on stderr out of the JSON on stdout.
func ghJSONOutput(command *system.Process, action string) ([]byte, error) {
	output, err := command.Output()
	if err == nil {
		return output, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
		return nil, gitx.CommandError(action, exitErr.Stderr, err)
	}
	return nil, gitx.CommandError(action, output, err)
}

func (c Client) PullRequestHead(owner, repository string, number int, directory string) (string, error) {
	command := system.Command(c.Executable, "pr", "view", strconv.Itoa(number), "--repo", owner+"/"+repository, "--json", "headRefName")
	command.Dir = directory
	output, err := ghJSONOutput(command, "gh pr view")
	if err != nil {
		return "", err
	}
	var pullRequest struct {
		HeadRefName string `json:"headRefName"`
	}
	if err := json.Unmarshal(output, &pullRequest); err != nil {
		return "", fmt.Errorf("parse pull request: %w", err)
	}
	branch := strings.TrimSpace(pullRequest.HeadRefName)
	if branch == "" {
		return "", fmt.Errorf("pull request #%d has no head branch", number)
	}
	return branch, nil
}

func (c Client) PullRequests(directory string) (map[string]domain.PullRequest, error) {
	command := system.Command(c.Executable, "pr", "list", "--state", "all", "--limit", "1000", "--json", "headRefName,headRefOid,state,mergedAt,number,url")
	command.Dir = directory
	output, err := ghJSONOutput(command, "gh pr list")
	if err != nil {
		return nil, err
	}
	var records []struct {
		HeadRefName string  `json:"headRefName"`
		HeadRefOID  string  `json:"headRefOid"`
		State       string  `json:"state"`
		MergedAt    *string `json:"mergedAt"`
		Number      int     `json:"number"`
		URL         string  `json:"url"`
	}
	if err := json.Unmarshal(output, &records); err != nil {
		return nil, fmt.Errorf("parse gh pr list: %w", err)
	}
	result := make(map[string]domain.PullRequest, len(records))
	priority := map[string]int{"closed": 1, "open": 2, "merged": 3}
	for _, record := range records {
		if record.HeadRefName == "" || record.HeadRefOID == "" {
			continue
		}
		status := strings.ToLower(record.State)
		if record.MergedAt != nil {
			status = "merged"
		}
		key := domain.PullRequestKey(record.HeadRefName, record.HeadRefOID)
		if priority[status] > priority[result[key].Status] {
			result[key] = domain.PullRequest{
				Status: status,
				URL:    record.URL,
				Number: record.Number,
			}
		}
	}
	return result, nil
}
