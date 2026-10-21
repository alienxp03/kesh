# kesh redesign — worktree-first, agent-aware main surface

Kitty-only (no daemon), bubbletea TUI. The main surface becomes the unit of
work: git worktrees grouped by repo, with coding-agent state inline. Sesh's
worktree-as-session + herdr's agent pane state, realized inside kesh.

## Main view (default surface)

- Repo-grouped tree: `▾ aurora  ⊘1 ⠋1  4 trees` → nested worktree rows.
- Worktree row: state glyph · branch · PR badge · sync badge · `★` current.
  - States (exclusive, priority): `⊘` blocked (agent finished/errored) →
    `⠋` working → `●` open (kitty session live) → `○` idle.
  - Inside a repo: blocked first, then working, open, idle.
- No filter chips (All/Agents/SSH/Saved removed). SSH hosts, saved sessions and
  the home folder collapse into an `other` group at the bottom.
- Agent pane (toggle, bottom): flat list, one line per agent —
  `status · worktree · repo · agent · age`, header `AGENTS · N blocked`.
  Worktree/repo is the primary column; the coding agent is secondary.

## Keys

| key | action |
| --- | --- |
| enter | connect: focus worktree's live session, else open it (recipe or plain) |
| n | new worktree for the repo under the cursor (branch \| #PR \| issue) |
| a | hunt: flatten to blocked-only worktrees; esc clears |
| tab | toggle the agent pane |
| f / o / x / D / space | pull / PR / remove / destroy / bulk — unchanged semantics |

## Data

Merge sources, no daemon: kitty live state (open windows, cwds, agent titles),
`git worktree list` per known repo (project entries + repos discovered under
the worktree root), agentstatus records, PR cache. Grouping, rollups and state
resolution are pure functions over these inputs.

## Non-goals

No background server, no pane ownership, no tmux. Existing Worktree drill-in
(`w`), PR checkout (`C`), and recipes (`.kesh.yaml`) keep working unchanged.
