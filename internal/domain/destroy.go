package domain

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
}

// DestroyPlan is the pure result consumed by the application confirmation and
// the integration command that performs the removals.
type DestroyPlan struct {
	EntryName    string
	CloseSession bool
	TabCount     int
	WorktreePath string
	Branch       string
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
	if target.Kind == "project" && target.Path != "" && target.IsLinkedWorktree {
		plan.WorktreePath = target.Path
		plan.Branch = target.LinkedBranch
	}
	return plan
}
