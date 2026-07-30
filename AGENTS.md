# AGENTS.md

Contract for agents and contributors editing kesh. Start here before changing
behavior — the user-facing overview lives in [README.md](README.md).

## Architecture

- `cmd/kesh` owns process startup and exit codes.
- `internal/app` owns the Bubble Tea model, message and mode routing, command scheduling, and pure rendering. Its single explicit mode state makes search, forms, pinning, confirmations, and worktree creation mutually exclusive.
- `internal/domain` owns pure entry ranking, pull-request/worktree matching and sorting, session composition, and destruction planning.
- `internal/config` and `internal/state` own XDG path/config loading and the versioned JSON formats, including legacy pin migration and atomic writes.
- `internal/kitty`, `internal/git`, and `internal/github` own intent-oriented integrations and output decoding; `internal/catalog` assembles Kitty, saved-session, SSH, and asynchronous zoxide sources.
- `internal/system` owns process execution and platform URL opening behind testable boundaries.

Dependency direction runs from `cmd/kesh` to `internal/app`, then toward the
domain, storage, and integration packages. Platform packages never import the
app, and neither the app nor domain packages invoke subprocesses directly.
Bubble Tea views are pure; filesystem and external-command work runs through
commands.

User configuration lives in `~/.config/kesh`; state, sessions, and caches live
under the XDG paths below. The Kitty watcher at
`config/kitty/scripts/kesh_clear_pins_on_quit.py` is intentionally a thin
adapter that invokes the installed binary.

## Validation

Tooling is pinned in `.mise.toml` (Go, golangci-lint); run `mise install` to
provision. The full local gate is:

```sh
make ci   # golangci-lint run ./... + go test -race ./... + go build ./cmd/kesh
make fmt  # apply gofmt + goimports (local imports grouped under third-party)
```

The pre-push hook (`.pre-commit-config.yaml`) runs formatting, lint, and tests
before every push. Activate once with `prek install --hook-type pre-push` (or
`pre-commit install --hook-type pre-push`).

See `docs/manual-smoke.md` for the real-Kitty checks that automated tests
cannot perform.

## Release

Releases are published by `.github/workflows/release.yml` when a `v*` tag is
pushed. The workflow publishes GitHub release archives and the generated
`Formula/kesh.rb` formula to the `alienxp03/homebrew-tap` repository.

The tap repository needs a `HOMEBREW_TAP_GITHUB_TOKEN` secret with Contents
write access to `alienxp03/homebrew-tap`; the default workflow token cannot
write to a different repository. The local helper requires Git push access to
`origin` and pushes the release commit and tag that starts the workflow:

```sh
make release-dry-run VERSION=v0.1.0
make release BUMP=patch                       # e.g. v0.1.0 -> v0.1.1
make release BUMP=minor RELEASE_FLAGS=-y       # e.g. v0.1.1 -> v0.2.0
make release VERSION=v1.0.0                   # explicit version, then prompt
```

For direct local publishing, export both `GITHUB_TOKEN` and
`HOMEBREW_TAP_GITHUB_TOKEN`, then run `goreleaser release --clean`.

## XDG paths

Resolved by `internal/config/paths.go` via `FromEnvironment()`:

| Kind | Location |
|---|---|
| Config | `${XDG_CONFIG_HOME:-~/.config}/kesh/config.yaml` |
| Aliases | `${XDG_STATE_HOME:-~/.local/state}/kesh/aliases.json` |
| Pins | `${XDG_STATE_HOME:-~/.local/state}/kesh/pins.json` |
| Pin shortcuts | `${XDG_STATE_HOME:-~/.local/state}/kesh/kitty-pins.conf` |
| Saved sessions | `${XDG_STATE_HOME:-~/.local/state}/kesh/saved-sessions.json` |
| Session snapshots | `${XDG_STATE_HOME:-~/.local/state}/kesh/sessions/` |
| Kitty run marker | `${XDG_STATE_HOME:-~/.local/state}/kesh/kitty-run` |
| Agent statuses | `${XDG_STATE_HOME:-~/.local/state}/kesh/agent-status/` |
| PR status cache | `${XDG_CACHE_HOME:-~/.cache}/kesh/pr-status.json` |

Note: kitty's `include ~/.local/state/kesh/kitty-pins.conf` is a hardcoded
literal in `kitty.conf` — kitty has no `XDG_STATE_HOME` concept, so a
non-default `XDG_STATE_HOME` will desync kesh's writes from kitty's read.

## Behavioral contracts

### Pin and alias lifecycle

Pinned sessions are stored in `pins.json`; kesh also generates `kitty-pins.conf`
beside it and reloads Kitty whenever pins change. The generated `kesh_pin_0`–
`kesh_pin_9` action aliases invoke Kitty's native `goto_session` directly;
users choose their own Kitty mappings without starting kesh on every switch.

Pins and session aliases apply only to the current Kitty run. On a confirmed
normal quit, Kitty notifies kesh, which clears both states and the pin mappings.
Kesh records the active Kitty process; if Kitty is force-terminated, its next
start detects the dead process, clears the leftover pins and aliases, and
reloads the mappings.

### Saved sessions

Catalogued in `saved-sessions.json`, with Kitty snapshots under the adjacent
sessions/ directory. `s` saves safely without capturing shell foreground
commands; `S` lists the detected foreground commands before confirmation and
Kitty reruns them when restoring. Saved entries remain after they are closed;
`enter` restores a closed entry or focuses it when open. `x` `y` on a closed
saved entry deletes its snapshot.

### Detail panel and layout

The list keeps a concise second column for paths, commands, counts, and other
scan-friendly context when space allows. A detail panel follows every selected
row — project, workspace, tab, window, agent, or worktree — and adapts its
fields to that row type. Wide layouts keep the list on the left with details
immediately beside it on the right inside a centered, width-capped workspace;
narrow layouts stack details below the list. Long detail values wrap across
lines with a hanging indent under their field label.

Session details show each unique window directory instead of treating the
first tab's directory as representative, deduplicating paths and summarizing
overflow.

### Row semantics and icons

A single-project Kitty session and its zoxide source are one logical folder
row, shown with ``. Multi-project sessions created by kesh remain separate
`` session rows so their individual folder sources stay available for
composing another session with `n`. SSH locations use ``. Green means an
entry is currently open; the icon does not change for saved or closed state.

### Pull-request status

Rows backed by a Git checkout lazily load their branch and PR summary when
focused, keeping startup fast, and show a warning when local HEAD differs from
the PR head. Worktree details include the branch, shortened path, and PR
summary. GitHub PR lifecycle status is shown when the branch and PR head SHA
match: green for open, purple for merged, red for closed without merging.

Kesh displays cached status immediately from the PR status cache, refreshes it
in the background when Worktrees opens, and throttles refreshes to once per
minute per repository. Capital `X` always bypasses the cache and revalidates
merged status before offering removal.

### Agents preview and status

The `Agents` filter previews the selected window's visible terminal screen,
refreshing once per second. Pi lifecycle status comes from the managed global
extension installed by `kesh agents setup pi`; it writes per-Kitty-window state
under the Agent statuses path. Kesh polls those small files once per second and
acknowledges finished or errored state when the window is focused.
