package domain

// LinkedWorktree identifies a linked checkout and its local branch.
type LinkedWorktree struct {
	Path   string
	Branch string
}

// DestroyTarget describes the independently removable layers associated with
// one catalog entry.
type DestroyTarget struct {
	Name             string
	Saved            bool
	Open             bool
	TabCount         int
	Kind             string
	Path             string
	IsLinkedWorktree bool
	LinkedBranch     string
	LinkedWorktrees  []LinkedWorktree
}

// DestroyPlan is the pure result consumed by the application confirmation and
// the integration command that performs the removals.
type DestroyPlan struct {
	EntryName    string
	CloseSession bool
	TabCount     int
	WorktreePath string
	Branch       string
	Worktrees    []LinkedWorktree
	Saved        bool
}

// PlanDestroy restricts folder and branch removal to linked worktree projects.
// Plain directories and composed workspaces are never deleted.
func PlanDestroy(target DestroyTarget) DestroyPlan {
	plan := DestroyPlan{
		EntryName:    target.Name,
		CloseSession: target.Open && target.TabCount > 0,
		TabCount:     target.TabCount,
		Saved:        target.Saved,
	}
	worktrees := append([]LinkedWorktree(nil), target.LinkedWorktrees...)
	if target.Kind == "project" && target.Path != "" && target.IsLinkedWorktree {
		worktrees = append(worktrees, LinkedWorktree{Path: target.Path, Branch: target.LinkedBranch})
	}
	seen := make(map[string]bool, len(worktrees))
	for _, worktree := range worktrees {
		if worktree.Path == "" || seen[worktree.Path] {
			continue
		}
		seen[worktree.Path] = true
		plan.Worktrees = append(plan.Worktrees, worktree)
	}
	if len(plan.Worktrees) > 0 {
		plan.WorktreePath = plan.Worktrees[0].Path
		plan.Branch = plan.Worktrees[0].Branch
	}
	return plan
}
