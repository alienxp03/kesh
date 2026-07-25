package domain

import (
	"sort"
	"strconv"
	"strings"
)

type Worktree struct {
	Path      string
	Branch    string
	Head      string
	Current   bool
	IsDefault bool
	PRStatus  string
	PRURL     string
	PRNumber  int
	PRExact   bool
	PRRepoKey string
	Dirty     bool
	Behind    int
	Ahead     int
	Changes   []string
}

func ParseWorktreePorcelain(output string) []Worktree {
	var items []Worktree
	var current *Worktree
	flush := func() {
		if current != nil {
			items = append(items, *current)
			current = nil
		}
	}
	for _, raw := range strings.Split(output, "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case strings.HasPrefix(line, "worktree "):
			flush()
			current = &Worktree{Path: strings.TrimSpace(strings.TrimPrefix(line, "worktree "))}
		case current == nil:
			continue
		case strings.HasPrefix(line, "HEAD "):
			current.Head = strings.TrimPrefix(line, "HEAD ")
		case strings.HasPrefix(line, "branch "):
			current.Branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		case line == "detached":
			current.Branch = "(detached)"
		case line == "":
			flush()
		}
	}
	flush()
	return items
}

func ParseWorktreeStatus(output string) (dirty bool, ahead, behind int, changes []string) {
	lines := strings.Split(output, "\n")
	if len(lines) == 0 {
		return false, 0, 0, nil
	}
	if header := strings.TrimSpace(lines[0]); strings.HasPrefix(header, "## ") {
		if start := strings.Index(header, "["); start >= 0 {
			ahead, behind = ParseAheadBehind(header[start:])
		}
	}
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		dirty = true
		changes = append(changes, strings.TrimRight(line, "\r"))
	}
	return dirty, ahead, behind, changes
}

func ParseAheadBehind(segment string) (ahead, behind int) {
	segment = strings.Trim(segment, "[]")
	for _, part := range strings.Split(segment, ",") {
		fields := strings.Fields(part)
		if len(fields) != 2 {
			continue
		}
		n, _ := strconv.Atoi(fields[1])
		switch fields[0] {
		case "ahead":
			ahead = n
		case "behind":
			behind = n
		}
	}
	return ahead, behind
}

func WorktreePriority(worktree Worktree) int {
	if worktree.IsDefault {
		return 0
	}
	switch worktree.PRStatus {
	case "open":
		return 1
	case "merged":
		return 2
	case "closed":
		return 3
	default:
		return 4
	}
}

func SortWorktrees(worktrees []Worktree) {
	sort.SliceStable(worktrees, func(i, j int) bool {
		left, right := WorktreePriority(worktrees[i]), WorktreePriority(worktrees[j])
		if left != right {
			return left < right
		}
		return strings.ToLower(worktrees[i].Branch) < strings.ToLower(worktrees[j].Branch)
	})
}

func MergedWorktrees(worktrees []Worktree, mergedOutput, currentBranch string, pullRequestHeads map[string]map[string]bool) []Worktree {
	merged := map[string]bool{}
	for _, branch := range strings.Fields(mergedOutput) {
		merged[branch] = true
	}
	var result []Worktree
	for index, worktree := range worktrees {
		if index == 0 || worktree.Branch == "" || worktree.Branch == "(detached)" || worktree.Branch == currentBranch {
			continue
		}
		mergedPullRequest := pullRequestHeads[worktree.Branch][worktree.Head]
		if merged[worktree.Branch] || mergedPullRequest {
			result = append(result, worktree)
		}
	}
	return result
}
