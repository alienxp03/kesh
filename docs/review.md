# kesh review — improvement backlog

Snapshot from a full review pass on **2026-07-25** (`main.go` ~6591 lines,
`main_test.go`). Build/vet/gofmt clean; all tests pass at time of writing.
Two parallel passes fed this list: a correctness/quality audit and a
UX/feature-gap survey. Findings are anchored to `main.go` unless noted;
line numbers drift over time — grep for the cited symbols.

Status legend: `[ ]` open · `[x]` done · `[~]` won't-do / deferred.

---

## 🔴 Critical bugs — wrong or destructive actions

These were verified against the code at review time. Several can destroy
or mis-target work; fix before anything else.

- [x] **`x` in the Worktree tab removes the wrong worktree, force, no confirm**
  `case "x"` (~2142) calls `runRemoveWorktree(true)` without setting
  `m.closeRow`; `runRemoveWorktree` (~6328) reads `m.closeRow`, not the
  cursor. First use removes `entries[0].worktrees[0]` (possibly another
  repo's worktree) with `--force` (closes kitty windows) and skips the
  confirm popup. **Data loss.** Fix: set `closeRow` from the cursor row
  before calling, and route through the confirm popup like main-mode `x`.
  Done — the tab `x` now addresses the worktree by path (the tab list can
  be a filtered/reordered subset, so a wt index would mis-target) and
  routes through the `y`/`f` confirm popup; `runRemoveWorktree` and
  `closePrompt` resolve via the new `worktreeForRow` helper.

- [x] **`enter` opens the project, not the worktree** — `enter` in the tab
  (~2049) → `runAction` (~5448). The row carries `worktreePath` (written
  ~2054) but it is **never read** anywhere; `runAction` only opens a
  worktree for `section == "wt-item"`, so it falls through to
  `openProjectSession` and opens the main checkout. Fix: honor
  `worktreePath` in `runAction` (or branch in the `enter` handler).
  Done — `runAction` opens `selected.worktreePath` before any other branch.

- [x] **`D` destroys the whole project, not the selected worktree** — the
  worktree-specific `destroyPlan` only builds for `section == "wt-item"`
  (~2160); in the tab the section is `"wt-filter"`, so `D` plans full
  entry destruction. Fix: include `"wt-filter"` and resolve the worktree
  via `m.worktreeFilterRows[m.cursor]`.
  Done — the tab `D` builds a worktree-scoped plan from
  `worktreeFilterRows[cursor]` (no `closeSession`); the post-destroy reload
  re-resolves the tab's project by path and refetches.

- [x] **`X` (remove merged) errors for open projects in the tab** —
  `findMergedWorktrees` → `worktreeDirectory` → `closedEntryAt` returns
  nil for open entries. Fix: fall back to
  `m.entries[m.worktreeFilterEntryIndex].path` in worktree-tab mode.
  Done — `worktreeDirectory` resolves the repo from the tab's project path
  when in `filterWorktrees` mode, so both `findMergedWorktrees` (the scan)
  and `runRemoveMergedWorktrees` (the confirmed removal) target the right
  repository for open projects.

- [x] **PR opener is platform-wrong** — tab `g` shells out to `xdg-open`
  (~2127); main `openWorktreePR` uses `run("open", …)` (~5725). On Linux
  main-mode PR open is broken (`open` missing); on macOS tab `g` breaks.
  Fix: route both through one platform-aware opener (mirror `commands()`).
  Done — both paths route through `openURL`/`openerCommand` (macOS `open`,
  Linux `xdg-open`, via `LookPath`); the tab `g` also stopped quitting
  kesh on success (it now returns `openPRMsg`, not `actionMsg`).

## 🟠 Notable bugs (annoying, recoverable)

- [x] **`shift+tab` cycles wrong** (~2224): `(filter+5)%7` jumps back two
  tabs, not one. Should be `(filter+6)%7`. Forward `tab` is correct.
- [x] **Cursor jumps to top while typing in tab search** —
  `rebuildWorktreeRows` sets `m.cursor = 0` on every rebuild (~2827).
  Main-mode `rebuildRows` only clamps when out of range; do the same here.
- [x] **Cursor jumps to top on any background PR refresh** in the tab —
  `focusedWorktreePath`/`restoreFocusedWorktree` (~6420/~6436) only handle
  `"wt-item"`, so the save/restore is a no-op for `"wt-filter"`.
- [x] **Create-from-tab quits kesh** — `worktreeMsg` → `tea.Quit`
  unconditionally (~1406). When `filter == filterWorktrees`, reload the
  tab instead of quitting (matches the documented "esc returns to tab").
- [x] **`space` in the tab leaks selections into main mode** — not cleared
  on `esc` (~2001); pollutes the "Selected (N)" header and
  `n`/`worktreeEntries` resolution.
- [x] **Stale `e worktrees` hint** — detail panel action still reads
  `"Enter open · e worktrees"` (~3395) but `e`/`toggleWorktrees` was
  removed; pressing `e` is a silent no-op.
- [x] **`git fetch` errors swallowed** while the UI claims the refresh is
  authoritative (`fetchOriginThenReload` ~5517). Surface failures as
  `m.err` while still reloading local state.
- [x] **Footer error-guard omits the new busy flags** (~2972): excludes
  `worktreePullBusy`/`mergedWorktreeBusy`, so an error during those flows
  renders in both popup and footer.
- [x] **Dead `"wt-foot"`** in the `row.section` doc comment (~125).
- [x] **`worktreeWindowIDs` error silently swallowed** in the tab `enter`
  flow (~2042); user gets no signal that focus-existing failed.

## 🟢 UX / feature opportunities

- [x] **Paging / jump keys** — `ctrl+d/u`, `pgup/dn`, `g`/`G`, `home/end`.
  Lists are line-scroll only today; biggest single navigation win.
  Touches `View()` window calc (~2912) + main `Update()` switch (~1995).
  Done — added `listPageSize()` (mirrors the `View()` window calc) and
  `ctrl+d`/`pgdown` (half-page down), `ctrl+u`/`pgup` (half-page up),
  `home`, `end`/`G`, and `g` (top) to the main key switch. `g` keeps its
  PR-open meaning in the Worktree tab (and the worktree tab gets `G`/`end`
  for jump-to-bottom since `g` is taken there).
- [x] **Show the project/repo name in the Worktree tab** —
  `worktreeFilterRow.entryName`/`entryPath` are populated (~2789) but
  never rendered; the tab is context-blind once entered. Put it in the
  header (~2900) or detail-panel title (~3282).
  Done — the header now renders the tab project's name (and `owner/repo`
  when the origin differs) plus a `Selected (N)` count when worktrees are
  bulk-selected.
- [ ] **Create worktree from a branch/PR picker** instead of typing a name
  (`n` → `beginWorktreeCreate` ~5746; `validateWorktreeBranch` ~5591).
  Browse `git branch -r` / `gh pr list`; eliminates typos. The PR data it
  needs is already fetched per-repo via `queryPRStatuses` (`gh pr list`) and
  cached, so a future picker can reuse it. Deferred pending UX decision.
- [x] **Bulk select in the Worktree tab** — `space` + bulk `x`/`p` for
  fast stale-worktree cleanup. `toggleSelected` (~2233) currently rejects
  non-project rows; mirror main's selection machinery.
  Done — `space` toggles a path-keyed `wtBulkSelected` map (rendered as a
  leading `✓`); `x` routes a bulk remove through the confirm popup via the
  new `wt-bulk` row section (`runRemoveWorktrees` mirrors the merged-
  worktree flow), and `p` runs a batched `pullWorktrees`. Selections
  resolve from the project's full worktree list (so a search filter does
  not silently drop them) and stale paths are pruned on rebuild. Cleared
  on `esc` and on `tab`/`shift+tab`.
- [ ] **Worktree recency / last-modified + sort** — surface stale
  worktrees (the point of `X`). Add an mtime field to `worktreeItem`
  (~73), populate in `fetchWorktrees` (~6468), sort in
  `rebuildWorktreeRows` (~2810), render in `renderRow` wt-filter (~4063).
- [ ] **`git worktree move` / rename** in the tab.
- [ ] **"cd" an existing kitty window into a worktree** instead of opening
  a new tab (`enter` handler ~2027 + `runAction`).
- [ ] **`g` fall-back to `gh pr list --web`** (or in-app list) when a
  worktree has no linked PR (~2126 / `openWorktreePR` ~5725).
- [ ] **Dead-key feedback + `?` help overlay** (per-tab keymap); the main
  switch has no `default` (~2226), so unknown keys silently no-op.
- [ ] **Agents tab depth** — group by project/agent, add open-PR (the
  `pathPR` data is already loaded for agent windows ~3333).
  `rebuildAgentRows` (~2732), agents footer (~2948).

## 🏗 Architecture

- [x] **Reconcile inline worktree rendering with the Worktree tab** —
  the inline `wt-head`/`wt-item` projection, cache, renderer, and action path
  have been removed. `w` now routes a selected project into the sole
  Worktrees management surface, and `esc` returns to the originating filter.
- [x] **Split the single file** — Kesh now has a composition-only
  `cmd/kesh`, a routed `internal/app` with explicit mutually exclusive modes
  and separately owned update/view files, pure `domain`, versioned
  `config/state`, and narrow `kitty/git/github/catalog/system` integration
  packages. Async preview, rename, save, and worktree results use stable
  identities or request IDs rather than mutable row positions.

## Regression coverage

The formerly missing Worktrees paths now have regression coverage for enter,
create, pull, PR open, single and merged removal, destroy, bulk selection,
search, cursor preservation, filter cycling, and return to the originating
filter. Late asynchronous mode results are also ignored after cancellation.
