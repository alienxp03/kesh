package app

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/alienxp03/kesh/internal/workspace"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func prCheckoutPreview(value, branch, selectedRepoPath, repoPath string, newClone bool, cloneRoot, worktreeRoot string, fieldWidth int) string {
	owner, repo, _, useSelected, err := parsePullRequestInput(value)
	if err != nil {
		return ""
	}
	var lines []string
	if useSelected || owner == "" {
		if selectedRepoPath == "" {
			lines = append(lines, "Root repo path: select a project")
		} else {
			lines = append(lines, "Root repo path: "+displayPath(selectedRepoPath, os.Getenv("HOME")))
		}
		return renderPreviewLines(lines, fieldWidth)
	}
	if repoPath == "" {
		lines = append(lines, "Root repo path: resolving…")
		return renderPreviewLines(lines, fieldWidth)
	}
	rootNote := ""
	if newClone {
		rootNote = " (new clone)"
	}
	lines = append(lines, "Root repo path: "+displayPath(repoPath, os.Getenv("HOME"))+rootNote)
	// Worktrees land under <worktreeRoot>/<owner>/<repo>/<branch>; fall back to
	// the clone root when the worktree root is unconfigured so the path is still
	// informative.
	root := worktreeRoot
	if root == "" {
		root = cloneRoot
	}
	if branch == "" {
		lines = append(lines, "Worktree path: resolving PRbranch…")
		return renderPreviewLines(lines, fieldWidth)
	}
	worktreePath := displayPath(filepath.Join(root, owner, repo, worktreeDirectoryName(branch)), os.Getenv("HOME"))
	lines = append(lines, "Worktree path: "+worktreePath+rootNote)
	return renderPreviewLines(lines, fieldWidth)
}

func sessionPreview(recipe *workspace.Config, repoPath, branch string) string {
	template := recipe.Terminal.SessionName
	if template == "" {
		template = "${repo}/${branch}"
	}
	repo := filepath.Base(repoPath)
	branch = strings.NewReplacer("/", "-", " ", "-").Replace(branch)
	rendered := strings.ReplaceAll(strings.ReplaceAll(template, "${repo}", repo), "${branch}", branch)
	var keep []string
	for _, segment := range strings.Split(rendered, "/") {
		if strings.TrimSpace(segment) != "" {
			keep = append(keep, segment)
		}
	}
	return strings.Join(keep, "/")
}

func paneLabel(pane workspace.PaneCommand) string {
	label := pane.Command
	if label == "" && len(pane.Commands) > 0 {
		label = strings.Join(pane.Commands, " && ")
	}
	if label == "" {
		label = "shell"
	}
	if pane.Focus {
		label += " *"
	}
	return truncate(label, 26)
}

type paneNode struct {
	label         string
	vertical      bool
	percentage    int
	first, second *paneNode
}

// paneDiagram simulates the workspace layout's successive Kitty splits in
// a small terminal-cell canvas. It is intentionally a preview: Kitty makes
// final pixel sizing decisions, but the pane relationships and configured bias
// are exact.
func paneDiagram(panes []workspace.PaneCommand, width int) []string {
	if len(panes) == 0 {
		panes = append(panes, workspace.PaneCommand{Command: "shell"})
	}
	root := &paneNode{label: paneLabel(panes[0])}
	active := root
	for _, pane := range panes[1:] {
		old := *active
		next := &paneNode{label: paneLabel(pane)}
		*active = paneNode{vertical: pane.Split == "vertical", percentage: pane.Percentage, first: &old, second: next}
		active = next
	}
	canvasWidth := min(54, max(20, width-4))
	canvasHeight := 7
	canvas := make([][]rune, canvasHeight)
	for y := range canvas {
		canvas[y] = []rune(strings.Repeat(" ", canvasWidth))
	}
	put := func(x, y int, value string) {
		if y >= 0 && y < canvasHeight {
			for i, r := range []rune(value) {
				if x+i >= 0 && x+i < canvasWidth {
					canvas[y][x+i] = r
				}
			}
		}
	}
	var draw func(*paneNode, int, int, int, int)
	draw = func(node *paneNode, x, y, w, h int) {
		if w < 4 || h < 3 {
			return
		}
		put(x, y, "┌"+strings.Repeat("─", w-2)+"┐")
		put(x, y+h-1, "└"+strings.Repeat("─", w-2)+"┘")
		for row := y + 1; row < y+h-1; row++ {
			put(x, row, "│")
			put(x+w-1, row, "│")
		}
		if node.first == nil {
			put(x+1, y+1, truncate(node.label, w-3))
			return
		}
		percent := node.percentage
		if percent <= 0 {
			percent = 50
		}
		if percent < 25 {
			percent = 25
		}
		if percent > 75 {
			percent = 75
		}
		if node.vertical {
			split := max(3, h*percent/100)
			draw(node.first, x, y, w, split)
			draw(node.second, x, y+split-1, w, h-split+1)
		} else {
			split := max(4, w*percent/100)
			draw(node.first, x, y, split, h)
			draw(node.second, x+split-1, y, w-split+1, h)
		}
	}
	draw(root, 0, 0, canvasWidth, canvasHeight)
	lines := make([]string, canvasHeight)
	for i := range canvas {
		lines[i] = strings.TrimRight(string(canvas[i]), " ")
	}
	return lines
}

func workspaceRepoPath(ws workspace.Workspace, recipePath string) string {
	repo := ws.Repo
	if repo == "" {
		repo = "."
	}
	if expanded, err := expandHomePath(repo); err == nil {
		repo = expanded
	}
	if !filepath.IsAbs(repo) {
		repo = filepath.Join(filepath.Dir(recipePath), repo)
	}
	return displayPath(filepath.Clean(repo), os.Getenv("HOME"))
}

func layoutPreview(recipe *workspace.Config, recipePath, mode string, width int, selected []bool) []string {
	all := recipe.Workspaces
	var indices []int
	switch mode {
	case "single":
		if len(all) > 0 {
			indices = []int{0}
		}
	case "selected":
		for i := range all {
			if i < len(selected) && selected[i] {
				indices = append(indices, i)
			}
		}
	default: // "all" and others show every workspace
		for i := range all {
			indices = append(indices, i)
		}
	}
	var lines []string
	for display, i := range indices {
		workspace := all[i]
		connector := "├─"
		if display == len(indices)-1 {
			connector = "└─"
		}
		lines = append(lines, connector+" "+workspace.Name+"  "+workspaceRepoPath(workspace, recipePath))
		for _, line := range paneDiagram(workspace.Panes, width) {
			lines = append(lines, "   "+line)
		}
	}
	return lines
}

// renderWorktreeChecklist renders the workspace selection shared by the
// read-only template preview and the editable Workspaces mode.
func (m *model) renderWorktreeChecklist(selected []bool, interactive bool) string {
	var rows []string
	for i, workspace := range m.worktreeRecipe.Workspaces {
		mark := "☐"
		if i < len(selected) && selected[i] {
			mark = "☑"
		}
		label := mark + " " + workspace.Name + "  " + workspaceRepoPath(workspace, m.worktreeRecipePath)
		if interactive && i == m.worktreeWorkspaceCursor {
			rows = append(rows, focusStyle.Render("› "+label))
		} else {
			rows = append(rows, dimStyle.Render("  "+label))
		}
	}
	return strings.Join(rows, "\n") + "\n"
}

func (m model) worktreePreviewSelection() []bool {
	if m.worktreeCustomWorkspaces || m.worktreeRecipeMode == "selected" {
		return m.worktreeSelected
	}
	selected := make([]bool, len(m.worktreeRecipe.Workspaces))
	if m.worktreeRecipeMode == "single" && len(selected) > 0 {
		selected[0] = true
	} else if m.worktreeRecipeMode == "all" {
		for i := range selected {
			selected[i] = true
		}
	}
	return selected
}

func renderPreviewLines(lines []string, fieldWidth int) string {
	wrapped := lipgloss.NewStyle().Width(fieldWidth).Render(strings.Join(lines, "\n"))
	return "\n\n" + dimStyle.Render(wrapped)
}

// worktreeModeMenuView makes the available creation layouts visible while the
// worktree form is open. Tab still cycles these choices so branch entry remains
// the primary keyboard focus.
func (m model) worktreeModeMenuView(width int) string {
	active := "workspaces"
	if m.worktreeRecipeMode == "none" {
		active = "native"
	}
	modes := []struct {
		value string
		label string
	}{
		{"native", "Plain"},
		{"workspaces", "Workspaces"},
	}

	lines := []string{dimStyle.Render("MODE")}
	for _, mode := range modes {
		line := "  " + mode.label
		if mode.value == active {
			line = focusStyle.Render("› " + mode.label)
		} else {
			line = dimStyle.Render(line)
		}
		lines = append(lines, ansi.Truncate(line, width, "…"))
	}
	return lipgloss.NewStyle().Width(width).Render(strings.Join(lines, "\n"))
}
