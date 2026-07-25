# Kesh extraction and rearchitecture plan

## Objective

Move Kesh from Kitty's configuration tree into a standalone application at
`~/.dotfiles/apps/kesh`, then replace the current single-package, single-file
shape with explicit UI, domain, persistence, and system-integration boundaries.
The move must preserve current behavior, state formats, keyboard controls, and
Kitty integration.

This is a migration plan, not a feature redesign. Refactoring should happen in
small, testable steps rather than as a single rewrite.

## Current-state audit

The application currently lives at `config/kitty/scripts/kesh` and contains:

- `main.go`: 7,139 lines.
- `main_test.go`: 3,134 lines.
- One `package main` containing the CLI, Bubble Tea model, a roughly 1,100-line
  `Update`, all views, domain types, config/state persistence, and subprocess
  integrations for Kitty, Git, GitHub CLI, zoxide, SSH, and the OS URL opener.
- A `model` with many independent modal booleans and mode-specific fields. This
  allows invalid combinations and makes unrelated interactions share one key
  handler.
- Asynchronous Bubble Tea messages carrying slice indexes into mutable entry
  lists. Existing code already needs defensive re-resolution after refreshes;
  stable identities are safer than positional references.
- Tests with useful behavioral coverage, but all tests are coupled to the main
  package and many integration tests replace executables through `PATH`.
- Runtime coupling outside the Go module:
  - `config/kitty/kitty.conf` launches the binary from
    `~/.config/kitty/scripts/kesh/kesh`.
  - `config/kitty/scripts/kesh/clear_pins_on_quit.py` locates the binary beside
    itself.
  - `config/mise/config.toml` builds in the Kitty scripts directory.
  - Kesh's actual user config already lives separately under `config/kesh` and
    should stay there.

Baseline verified before planning:

```text
go test ./...  -> pass
go vet ./...   -> pass
```

The existing `docs/review.md` also identifies the monolithic file and duplicate
worktree surfaces as architectural debt. Its completed regression cases must
remain covered during the migration.

## Research findings that guide the design

1. The official Go module-layout guidance says a larger command may split
   supporting functionality into `internal` packages. `internal` prevents
   accidental external consumers and leaves those APIs free to evolve. It also
   notes that a command-only module does not require a `cmd` directory, but a
   `cmd` entry point is useful when a repository contains application docs,
   integrations, and supporting packages. Source:
   [Organizing a Go module](https://go.dev/doc/modules/layout).
2. Bubble Tea v1 defines `Model` as `Init`, `Update`, and `View`, and defines a
   `Cmd` as an I/O operation returning a message. The framework explicitly
   expects I/O such as disk access and timers to live in commands rather than
   rendering or synchronous state transitions:
   [`tea.go` at Kesh's v1.3.10 dependency](https://github.com/charmbracelet/bubbletea/blob/9edf69c677c7353eca5fae6d3ea3986af39717b7/tea.go#L39-L65).
3. The matching v1.3.10 tutorial recommends commands for disk/network I/O and
   handling their result messages in `Update`:
   [commands tutorial](https://github.com/charmbracelet/bubbletea/blob/9edf69c677c7353eca5fae6d3ea3986af39717b7/tutorials/commands/README.md#L48-L105).
4. Bubble Tea's composable-view example demonstrates a parent model routing
   messages to focused child state and batching returned commands. Kesh should
   apply that principle to its modes, without turning every helper into a
   separate component:
   [composable views example](https://github.com/charmbracelet/bubbletea/blob/fc707bb7ea0161405bb6c653ec93f6a9c6a72fe1/examples/composable-views/main.go#L66-L112).

Consequences for Kesh:

- Keep Bubble Tea state transitions in one application package, but split them
  by mode and concern.
- Keep rendering pure: no subprocess or filesystem work in `View`.
- Put subprocess, filesystem, clock, and opener behavior behind narrow
  boundaries so state transitions can be tested deterministically.
- Use `internal` packages; Kesh does not currently promise a public Go API.
- Do not create packages merely to reduce line counts. A package must own a
  coherent responsibility and have a clear dependency direction.

## Target repository layout

```text
apps/kesh/
├── README.md
├── go.mod
├── go.sum
├── cmd/
│   └── kesh/
│       └── main.go
├── docs/
│   ├── plan.md
│   └── review.md
└── internal/
    ├── app/
    │   ├── app.go
    │   ├── model.go
    │   ├── messages.go
    │   ├── mode.go
    │   ├── update.go
    │   ├── update_normal.go
    │   ├── update_worktrees.go
    │   ├── update_agents.go
    │   ├── update_forms.go
    │   ├── rows.go
    │   ├── view.go
    │   ├── view_detail.go
    │   ├── view_popup.go
    │   ├── view_rows.go
    │   └── styles.go
    ├── domain/
    │   ├── entry.go
    │   ├── worktree.go
    │   ├── pull_request.go
    │   ├── destroy.go
    │   └── session.go
    ├── config/
    │   ├── config.go
    │   └── paths.go
    ├── state/
    │   ├── json.go
    │   ├── names.go
    │   ├── pins.go
    │   ├── saved_sessions.go
    │   └── pr_cache.go
    ├── kitty/
    │   ├── client.go
    │   ├── state.go
    │   ├── sessions.go
    │   └── pins.go
    ├── git/
    │   ├── repository.go
    │   ├── status.go
    │   └── worktrees.go
    ├── github/
    │   ├── pull_request.go
    │   └── status.go
    ├── catalog/
    │   ├── catalog.go
    │   ├── zoxide.go
    │   └── ssh.go
    └── system/
        ├── command.go
        ├── opener.go
        └── process.go
```

The exact file count may change while extracting code. The package boundaries
and dependency direction are the important part:

```text
cmd/kesh -> app -> domain
                 -> config/state
                 -> kitty/git/github/catalog
                 -> system

config/state/kitty/git/github/catalog -> domain where shared entities are needed
platform packages must not import app
```

### Responsibilities

- `cmd/kesh`: argument parsing, dependency construction, terminal title, program
  startup, and exit-code/error handling only.
- `app`: Bubble Tea model/messages, mode transitions, row projection, command
  scheduling, and rendering. It owns presentation state, not subprocess syntax
  or JSON formats.
- `domain`: pure application entities and algorithms: entry identity, worktree
  parsing/sorting decisions, PR matching, session composition inputs, and
  destruction plans. No Bubble Tea, Lip Gloss, environment, filesystem, or
  `os/exec` imports.
- `config`: TOML loading and XDG/home path resolution.
- `state`: versioned persisted formats and atomic JSON writes. Existing on-disk
  paths, permissions, versions, and migration behavior remain compatible.
- `kitty`: Kitty remote-control calls, state decoding, focus/close/session
  operations, and generated Kitty session content.
- `git`: repository/worktree discovery and mutation.
- `github`: PR input parsing and `gh` queries. GitHub-specific status cache
  policy may call `state`, but serialization itself stays in `state`.
- `catalog`: assembles entries from Kitty, saved sessions, zoxide, and SSH. It
  preserves the current fast-first-paint/async-zoxide behavior.
- `system`: the small reusable execution boundaries: command runner, process
  checks, clock if needed, and platform URL opener.

Avoid a generic `utils` package. Helpers should remain with the concept they
serve.

## Key design changes

### 1. Replace boolean modal state with an explicit mode

Replace combinations such as `searching`, `renaming`, `creating`, `cloning`,
`prCheckout`, `pinning`, `closing`, and related busy flags with:

```go
type modeKind uint8

const (
    modeNormal modeKind = iota
    modeSearch
    modeRename
    modeCreateSession
    modeClone
    modeCheckoutPR
    modePin
    modeSaveConfirm
    modeCloseConfirm
    modeWorktreeCreate
)

type modeState struct {
    kind modeKind
    // One typed payload appropriate to kind.
}
```

Use small payload structs (`cloneForm`, `checkoutForm`, `closeConfirmation`,
etc.) so each mode owns its input, focus, validation error, and pending status.
Transitions should go through named methods such as `beginClone`, `cancelMode`,
and `submitClone`. This makes impossible state combinations unrepresentable and
lets key handling dispatch by active mode.

Busy state that belongs to background refreshes rather than a modal operation
can remain on the relevant feature state (`preview.pending`,
`worktrees.refreshing`, `prStatus.pendingByRepo`).

### 2. Split update routing by message and active mode

Keep the required public method small:

```go
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.KeyMsg:
        return m.updateKey(msg)
    default:
        return m.updateMessage(msg)
    }
}
```

`updateKey` first handles global cancellation/quit rules, then delegates to the
active form/confirmation mode, Agents view, Worktrees view, or normal browser.
`updateMessage` delegates result messages to the owning feature. This preserves
Bubble Tea's unidirectional flow while removing the current deeply nested key
switch.

Message types should carry stable identifiers (entry key, path, Kitty window
ID, repository key, or request ID), not only entry/tab/window indexes. On
receipt, resolve the current object by identity and ignore stale responses.
Use monotonically increasing request IDs for previews and any operation where
multiple requests for the same identity can overlap.

### 3. Introduce narrow I/O boundaries

Create a `system.Runner` abstraction that captures command, arguments,
directory, environment, stdin, and output. Production uses `os/exec`; tests use
a recording fake. Do not expose raw `exec.Cmd` throughout the app.

Higher-level packages should expose intent-oriented operations, for example:

- `kitty.Client.List`, `FocusWindow`, `CloseWindow`, `LoadConfig`,
  `OpenSession`, `CaptureScreen`.
- `git.Repository.Worktrees`, `Fetch`, `Pull`, `RemoveWorktree`,
  `CreateWorktree`, `AheadBehind`.
- `github.Client.PullRequests`, `PullRequestHead`, `MergedHeads`.

Interfaces should be defined by consumers and kept small. Do not create one
large service interface mirroring every existing function.

All external work continues to run inside `tea.Cmd` closures. Constructors may
read immutable startup configuration, but no `View` or row-render function may
perform I/O.

### 4. Separate persisted records from UI models

Keep JSON-facing structs in `state`, with explicit versions and conversion to
and from domain values. Preserve:

- `${XDG_STATE_HOME:-~/.local/state}/kesh/*`
- `${XDG_CONFIG_HOME:-~/.config}/kesh/*`
- `${XDG_CACHE_HOME:-~/.cache}/kesh/pr-status.json`
- file and directory modes
- legacy pin migration
- atomic temporary-write-and-rename behavior

The refactor must not silently rewrite or invalidate current files. Add golden
fixtures for every currently supported persisted version before moving those
functions.

### 5. Choose one worktree presentation model

The current richer Worktrees filter and older inline `wt-head`/`wt-item`
expansion duplicate state and actions. During the first extraction, preserve
both to avoid behavior changes. After parity is established:

- Make the Worktrees filter the primary management surface.
- Remove inline worktree mutation/actions from the main hierarchy.
- Replace the main-view `e` behavior, if retained, with navigation into the
  Worktrees filter for the selected project rather than a second rendering
  path.
- Remove obsolete row-section variants only after tests prove all supported
  entry points route to the primary surface.

This is the one intentional internal simplification in scope. Any visible key
change must be documented in the README and backed by transition tests.

## Runtime and dotfiles integration

Source code must no longer be built inside `~/.config/kitty`.

1. Build `apps/kesh/cmd/kesh` to `${HOME}/.local/bin/kesh` using the mise task.
   The binary is a build artifact and must not be committed or symlinked from
   the source directory.
2. Update `kitty.conf` launch mappings to use `~/.local/bin/kesh`.
3. Keep the Kitty watcher as Kitty configuration, not Go application source.
   Move it to a clear path such as
   `config/kitty/scripts/kesh_clear_pins_on_quit.py` and make it invoke
   `~/.local/bin/kesh` (with a controlled fallback to `shutil.which("kesh")`
   only if desired). Update the `watcher` directive accordingly.
4. Remove the old ignored binary and old Go module directory after all callers
   are switched.
5. Keep `config/kesh/config.toml` and `config/kesh/names.json` in place; these
   are user configuration managed by dotfiles, not application source.
6. Add `go test ./...` and `go vet ./...` for `apps/kesh` to the relevant mise
   validation path. The build task should use an atomic temporary output and
   rename so a failed build cannot destroy the last working binary.

Recommended task shape:

```sh
cd "$HOME/.dotfiles/apps/kesh"
mkdir -p "$HOME/.local/bin"
tmp="$(mktemp "$HOME/.local/bin/.kesh.XXXXXX")"
go build -o "$tmp" ./cmd/kesh
chmod 0755 "$tmp"
mv "$tmp" "$HOME/.local/bin/kesh"
```

The temporary file needs a cleanup trap on failure in the actual task.

## Migration phases

### Phase 0 — Freeze behavior and inventory integration

- Record the current `go test`, `go vet`, and `gofmt` baselines.
- Add missing regression tests for CLI modes, Kitty watcher lifecycle, current
  state fixtures, Worktrees-tab actions, async stale-message handling, and the
  exact key help shown by each view.
- Add a small manual smoke checklist for real Kitty behavior that subprocess
  fakes cannot prove.
- Do not change behavior in this phase.

**Gate:** all existing and new characterization tests pass in the old location.

### Phase 1 — Move the module without restructuring it

- Create `apps/kesh` and move `go.mod`, `go.sum`, README, review notes, Go source,
  and tests with Git-aware moves.
- Initially keep the source as `package main` so failures can only come from the
  path/integration move.
- Change the module path from the placeholder `module kesh` to a stable local
  module identity. If this dotfiles repository has no intended public import
  path, use a repository-qualified path consistent with its Git remote rather
  than inventing a public API.
- Update mise build/test tasks, Kitty launch mappings, and watcher invocation.
- Build to `~/.local/bin/kesh`; remove the source-tree binary.

**Gate:** unit tests, vet, build, Kitty launch, Agents launch, pin begin/end-run,
and direct `kesh switch SLOT` smoke tests pass from the new binary.

### Phase 2 — Mechanical file split inside one package

Before introducing package APIs, split the moved `package main` by concern:
`model`, `messages`, `update`, `view`, `entries`, `state`, `kitty`, `worktree`,
`pr`, and `session`. Move tests beside the matching files.

This phase should be movement-only, aside from resolving duplicate helper names
or imports. It gives reviewable diffs and makes extraction dependencies visible
without simultaneously changing symbol visibility.

**Gate:** no intended runtime behavior change; tests, vet, build, and gofmt pass.

### Phase 3 — Extract pure domain and persistence packages

- Move pure entities/algorithms first: worktree parsing and sorting, PR matching,
  entry ranking, session composition, destroy-plan calculation, and path
  validation.
- Extract XDG paths/config and persisted records next.
- Add package-level tests and JSON golden fixtures before deleting equivalent
  main-package tests.
- Keep conversion at package boundaries explicit; do not export UI-only fields
  merely to make extraction easy.

**Gate:** package tests prove format compatibility and pure logic; no fixture
changes unless explicitly reviewed as a migration.

### Phase 4 — Extract system integrations

- Add the command-runner boundary.
- Extract Kitty, Git, GitHub, zoxide/SSH catalog, process, and opener code one
  integration at a time.
- Replace `PATH` mutation tests with recording fakes for unit behavior while
  retaining a smaller set of executable-stub integration tests to verify
  argument construction and output parsing end to end.
- Preserve error output/context from the current `commandError` behavior.

**Gate:** no direct `os/exec` use remains in `app` or `domain`; integration
packages have parser tests and command-contract tests.

### Phase 5 — Reshape the Bubble Tea application

- Move the UI into `internal/app` and reduce `cmd/kesh/main.go` to composition.
- Introduce the explicit mode and typed mode payloads.
- Split update routing and view rendering by mode.
- Replace positional async message addressing with stable identities/request
  IDs.
- Keep startup's fast Kitty load and asynchronous zoxide merge unchanged.
- Keep all I/O in Bubble Tea commands.

Do this mode by mode (rename, create, clone, checkout, save, pin, close,
worktree), with tests passing after each conversion. Do not replace the whole
model in one commit.

**Gate:** transition-table tests cover every mode's enter/edit/submit/cancel,
late async results are ignored, and all existing UI snapshots/behavior tests
pass.

### Phase 6 — Consolidate worktree UX and clean up

- Route worktree entry points to one management surface.
- Delete unreachable row sections, duplicate renderers, and obsolete helpers.
- Update README/help text and the architecture section of `docs/review.md`.
- Run dead-code/static analysis and inspect exported symbols; reduce exports
  that were only needed during migration.

**Gate:** Worktrees tests cover enter, create, pull, PR open, remove, merged
remove, destroy, bulk selection, search, cursor preservation, and return to the
originating filter.

### Phase 7 — Final verification and removal of old paths

Automated:

```sh
cd ~/.dotfiles/apps/kesh
test -z "$(gofmt -l $(find . -name '*.go' -type f))"
go test -race ./...
go vet ./...
go build ./cmd/kesh
```

Also run the repository's normal dotfiles validation. Confirm no tracked or
configuration reference remains to `config/kitty/scripts/kesh/kesh` or the old
Go module.

Manual Kitty smoke test:

- Launch full and Agents views from their Kitty mappings.
- Focus/open/close a workspace, tab, and window.
- Search, rename, create a composed session, clone, and check out a PR.
- Save/restore a session with and without foreground commands.
- Create/open/pull/remove a worktree and open its PR.
- Pin, switch, unpin, normal Kitty quit, and simulated unclean restart.
- Verify existing config, state, and cache files load without migration errors.
- Verify macOS URL opening; retain Linux opener unit coverage.

Only after these checks should the old source directory and stale binary be
removed.

## Testing strategy

- **Pure unit tests:** domain sorting/parsing/planning, state validation,
  path/config handling, row projection, mode transitions, rendering helpers.
- **Transition tests:** table-driven Bubble Tea key/message sequences. Assert
  resulting mode, selected stable identity, errors, and emitted command intent.
- **Persistence compatibility tests:** checked-in fixtures for names, pins,
  saved sessions, PR cache, and generated Kitty session/config content.
- **Command contract tests:** recording runner assertions for executable,
  arguments, cwd, environment, and stdin.
- **Parser integration tests:** feed realistic Kitty/Git/gh/zoxide/SSH outputs
  without launching processes.
- **Limited process integration tests:** retain temporary fake executables for
  the production runner and multi-command workflows.
- **Race test:** `go test -race ./...`, especially around commands and message
  result handling.
- **Manual integration:** real Kitty remote control and watcher callbacks.

Prefer behavior assertions over full-screen golden snapshots. Use small golden
outputs only for stable generated files or tightly scoped render components;
full terminal snapshots are too sensitive to width, ANSI styling, and copy
changes.

## Commit strategy

Keep commits independently buildable and behavior-preserving where possible:

1. Characterization tests.
2. Move source and change runtime paths.
3. Mechanical same-package file split.
4. Domain/config/state extraction, one package per commit.
5. Runner and platform extraction, one integration per commit.
6. App mode refactors, one mode per commit.
7. Worktree-surface consolidation.
8. Documentation and cleanup.

Do not mix unrelated feature work into these commits. Existing unrelated
working-tree changes, including the current modification to
`config/zsh/aliases.zsh`, must remain untouched.

## Risks and mitigations

- **Breaking Kitty at login:** install the new binary before changing mappings;
  use an atomic build output and keep the old binary until smoke tests pass.
- **Persisted-state loss:** freeze formats with fixtures and preserve versions,
  permissions, and atomic writes.
- **Stale async results targeting the wrong row:** use stable identities and
  request IDs rather than mutable indexes.
- **Package cycles:** enforce the dependency direction above; domain never
  imports app/platform, and platform never imports app.
- **Over-abstraction:** extract by responsibility, define interfaces at the
  consumer, and avoid a universal service container or repository pattern.
- **Behavior drift during a giant rewrite:** use the phased move, same-package
  split, and one-mode-at-a-time conversion.
- **Cross-platform regressions:** keep opener/process logic isolated and test
  macOS and Linux command selection.

## Definition of done

- Canonical application source is `~/.dotfiles/apps/kesh`.
- No Go source or built binary lives under the Kitty config tree.
- Kitty invokes `~/.local/bin/kesh`; its watcher remains a thin Kitty-specific
  adapter.
- `cmd/kesh/main.go` is composition only.
- Bubble Tea state uses explicit modes rather than independent modal booleans.
- App/domain code performs no direct subprocess I/O.
- Persisted state remains backward compatible.
- The duplicate worktree management path is removed or reduced to navigation
  into the primary Worktrees surface.
- `gofmt`, `go test -race ./...`, `go vet ./...`, build, dotfiles validation,
  and the real Kitty smoke checklist pass.
- README and architecture documentation describe the new source, build,
  install, runtime, and package layout.

## Documentation location

This plan is application-owned documentation and lives canonically at
`~/.dotfiles/apps/kesh/docs/plan.md`. The existing review notes should join it
under `apps/kesh/docs` when the source migration begins.
