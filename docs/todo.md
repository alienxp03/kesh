# Redesign todo

- [x] 1. `internal/app/workspaces.go`: workspace aggregate — repo groups, worktree
      states, agent rollups (pure functions + tests)
- [x] 2. Worktree-root repo scan (async) + scan messages wiring
- [x] 3. Main view rows (`filterWork` default): repo/worktree/other sections in
      `rebuildRows` + `renderRow`; header without chips
- [x] 4. Agent pane: flat agent list, tab toggle, block in `View`
- [x] 5. Keys: enter connect, a hunt, n new (repo-aware), f/o/x/D/space routing
      for `ws` rows
- [x] 6. Help text update; full test pass; rebuild

## Follow-ups (not started)

- [ ] `n` resolver input accepting `#PR` / issue numbers (currently branch names
      via the existing create form; PR checkout still via `C`)
- [ ] Preview (`p`) integration for agent panes on the main surface
- [ ] README screenshots + section rewrite
