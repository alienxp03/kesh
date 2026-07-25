# kesh

Bubble Tea picker for browsing zoxide projects, Kitty workspaces, tabs, windows, SSH hosts, and active Codex/pi agents.

A single-project Kitty session and its zoxide source are one logical folder row, shown with ``. Multi-project sessions created by Kesh remain separate `` session rows so their individual folder sources stay available for composing another session with `n`. SSH locations use ``. Green means an entry is currently open; the icon does not change for saved or closed state.

## Build and install

Kesh source lives in `apps/kesh`; Kitty always launches the installed artifact
at `~/.local/bin/kesh` rather than code in the Kitty configuration tree.

```sh
cd ~/.dotfiles
mise run kesh
```

The task builds to a temporary file and atomically replaces the installed
binary only after a successful build. For development-only builds:

```sh
cd ~/.dotfiles/apps/kesh
go build -o "${TMPDIR:-/tmp}/kesh-dev" ./cmd/kesh
```

## Architecture

- `cmd/kesh` owns process startup and exit codes.
- `internal/app` owns the Bubble Tea model, message and mode routing, command scheduling, and pure rendering. Its single explicit mode state makes search, forms, pinning, confirmations, and worktree creation mutually exclusive.
- `internal/domain` owns pure entry ranking, pull-request/worktree matching and sorting, session composition, and destruction planning.
- `internal/config` and `internal/state` own XDG path/config loading and the versioned JSON formats, including legacy pin migration and atomic writes.
- `internal/kitty`, `internal/git`, and `internal/github` own intent-oriented integrations and output decoding; `internal/catalog` assembles Kitty, saved-session, SSH, and asynchronous zoxide sources.
- `internal/system` owns process execution and platform URL opening behind testable boundaries.

Dependencies point from `cmd/kesh` to `internal/app`, then toward the domain,
storage, and integration packages. Platform packages never import the app, and
neither the app nor domain packages invoke subprocesses directly. Bubble Tea
views are pure; filesystem and external-command work runs through commands.

User configuration remains in `~/.config/kesh`; state, sessions, and caches
remain under the XDG paths described below. The Kitty watcher at
`config/kitty/scripts/kesh_clear_pins_on_quit.py` is intentionally a thin
adapter that invokes the installed binary.

## Validation

```sh
cd ~/.dotfiles/apps/kesh
test -z "$(gofmt -l $(find . -name '*.go' -type f))"
go test -race ./...
go vet ./...
go build -o "${TMPDIR:-/tmp}/kesh-dev" ./cmd/kesh
```

See `docs/manual-smoke.md` for the real-Kitty checks that automated tests
cannot perform.

Keys:

The picker starts in normal mode:

- `j` / `k` or `ctrl+j` / `ctrl+k`: select a row
- `space`: toggle a project or SSH host for a new multi-tab session
- `n`: name and create a session with one tab per selected item
- `c`: clone a Git repository into an editable destination and open it
- `C`: check out a GitHub pull request (paste a URL, `owner/repo#123`, or a bare number on a selected project) — clones if needed, then creates a worktree on the PR head
- `l`: expand or descend through session → tabs → windows
- `h`: return to the parent or collapse the current level
- `enter`: open a session, focus a tab, or focus a window
- `s`: safely save the selected open project or workspace's tabs, splits, and working directories
- `S`: additionally save foreground commands so restoring the project or workspace reruns them
- `p`, then `0`–`9`: pin the selected session to a shortcut slot; repeat an occupied slot to confirm its replacement
- `p`, then `x`: unpin the selected session
- `r`: rename the selected workspace, tab, or window; submitting an empty workspace name resets it
- `e`: expand or collapse the selected session or tab in the main hierarchy
- `w`: open the selected project's primary Worktrees management surface
- `o`: open the exact pull request associated with the selected worktree in the browser
- `X`, then `y`: remove every non-current worktree merged by Git ancestry or by a GitHub pull request at the same branch HEAD
- `D`, then `y`: destroy the focused entry — close its Kitty session, remove its worktree (only when it is a linked worktree), delete its local branch, and delete its saved record; the confirmation lists exactly which layers apply. In Worktrees, destroys the selected worktree and its branch
- `x`, then `y`: close the selected workspace, tab, or window; in Worktrees, remove the selected worktree
- `/`: enter search mode and fuzzy-filter sessions as you type
- `enter` / `esc`: return to command mode while retaining the filter
- `tab` / `shift+tab`: change filter
- `q`: close from command mode; `esc` is a no-op there

Arrow keys remain available for moving through rows and the hierarchy.

Worktrees are managed in one surface, opened with `w` from a project and
closed with `esc`. Its rows are ordered by default branch, open PR, merged PR,
closed PR, then entries without a matching PR. Use `n` to create, `enter` to
open/focus, `r` to fetch and refresh, `p` to pull, `g` to open the exact PR,
`x` to remove, `D` to destroy the worktree and branch, or `X` to remove merged
worktrees. `space` enables bulk pull/removal.

The list keeps a concise second column for paths, commands, counts, and other scan-friendly context when space allows. A detail panel follows every selected row—project, workspace, tab, window, agent, or worktree—and adapts its fields to that row type. Wide layouts keep the list on the left and details immediately beside it on the right inside a centered, width-capped workspace; narrow layouts stack details below the list. Long detail values wrap across lines with a hanging indent under their field label. Session details show each unique window directory instead of treating the first tab's directory as representative, deduplicating paths and summarizing overflow. Rows backed by a Git checkout lazily load its branch and PR summary when focused, keeping Kesh startup fast and showing a warning when local HEAD differs from the PR head. Worktree details include the branch, shortened path, and PR summary. They show GitHub pull-request lifecycle status when the branch and PR head SHA match: green for open, purple for merged, and red for closed without merging. Kesh displays cached status immediately from `${XDG_CACHE_HOME:-~/.cache}/kesh/pr-status.json`, refreshes it in the background when Worktrees opens, and throttles refreshes to once per minute per repository. Capital `X` always bypasses the cache and revalidates merged status before offering removal.

The `Agents` filter is a flat, most-recently-focused list of Kitty windows running Codex or pi. Its preview refreshes the selected window's visible terminal screen once per second:

- `enter`: focus the selected agent window
- `p`: show or hide the terminal preview
- `/`: fuzzy-search agent, project, tab, command, and directory fields

Run `kesh agents` to start directly in this view. Kitty invokes it in a tab for `Cmd+Shift+P`; `Cmd+Shift+O` opens the complete hierarchy in an overlay.

Pinned sessions are stored in `${XDG_STATE_HOME:-~/.local/state}/kesh/pins.json`. Kesh also generates `kitty-pins.conf` beside that file and reloads Kitty whenever pins change. `Cmd+0` through `Cmd+9` therefore invoke Kitty's native `goto_session` action directly, without starting Kesh on every switch. Pins apply only to the current Kitty run: Kitty notifies Kesh on a confirmed normal quit, and Kesh clears its state and mappings. Kesh records the active Kitty process; if Kitty is force-terminated, its next start detects the dead process, clears the leftover pins, and reloads the mappings.

Saved states are catalogued in `${XDG_STATE_HOME:-~/.local/state}/kesh/saved-sessions.json`, with Kitty snapshots under the adjacent `sessions/` directory. Press `s` on an open named project or workspace and confirm with `y` to save it safely without capturing shell foreground commands. Use `S` for an explicit command-aware snapshot: Kesh lists the detected foreground commands before confirmation, and Kitty reruns them when restoring. Saved entries remain in Kesh after they are closed; pressing `enter` restores a closed entry or focuses it when already open. Use `x`, then `y` on a closed saved entry to delete its snapshot.

The clone destination defaults to `~/workspace`. Override it in `${XDG_CONFIG_HOME:-~/.config}/kesh/config.toml`:

```toml
[clone]
root = "~/workspace"
```

Press `c` to open a form with the Git URL and inferred destination together. Use `tab` to switch fields, edit either value, then press `enter` to clone. After a successful clone, Kesh adds the directory to zoxide and opens its Kitty workspace.

Press `C` to check out a GitHub pull request. Paste the PR's web URL (or `owner/repo#123`, or a bare number with the cursor on one of the project's rows). Kesh reuses an existing local clone — the project under the cursor, or a candidate under the checkout root — cloning first when none exists, then fetches the PR head and creates a worktree on its branch and opens the workspace. Re-checking out the same PR focuses the existing worktree instead of recreating it. The `[checkout] root` option in `config.toml` controls where existing clones are searched and defaults to the clone root:

```toml
[checkout]
root = "~/workspace"
```

Workspace names are Kesh aliases stored in `${XDG_CONFIG_HOME:-~/.config}/kesh/names.json`. Kitty's internal session identity remains unchanged, so aliases can be edited without recreating a live session. Search matches both the alias and the original project or SSH name.
