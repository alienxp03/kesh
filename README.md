# kesh

Bubble Tea picker for browsing zoxide projects, Kitty workspaces, tabs,
windows, SSH hosts, and active Codex/pi agents.

## Install

Kitty launches the installed artifact at `~/.local/bin/kesh`.

### Homebrew (release binary)

```sh
brew install alienxp03/tap/kesh
brew upgrade kesh
```

### From source

```sh
go build -o "${TMPDIR:-/tmp}/kesh-dev" ./cmd/kesh
```

## Kitty setup

Kesh drives Kitty through remote control, so two lines are **required**:

```conf
allow_remote_control yes
listen_on unix:/tmp/kitty

# Open the pickers.
map cmd+shift+o launch --type=overlay ~/.local/bin/kesh
map cmd+shift+p launch --type=tab      ~/.local/bin/kesh agents
```

**Suggested** additions — session tab bar, session cycling, pinned-session
shortcuts, and handing `ctrl+j/k` to the pickers:

```conf
# Filter the tab bar to the active session.
tab_bar_filter session:~

# Cycle to the previous session.
map cmd+p goto_session -1

# Free Cmd+0..9 for Kesh pins; switch tabs with Ctrl+number instead.
# map ctrl+1..9 goto_tab 1..9

# Kesh writes Cmd+0..9 → goto_session mappings here.
# map cmd+0..9   (blanked so Kesh can bind them)
include ~/.local/state/kesh/kitty-pins.conf

# Let the pickers handle Ctrl+J/K instead of moving panes.
map --when-focus-on title:project-picker ctrl+j
map --when-focus-on title:project-picker ctrl+k
map --when-focus-on title:kesh ctrl+j
map --when-focus-on title:kesh ctrl+k
```

## Keys

The picker starts in normal mode. Green means an entry is currently open.

| Key | Action |
|---|---|
| `j` `k` · `ctrl+j` `ctrl+k` | Select a row |
| `enter` | Open a session / focus a tab / focus a window |
| `l` / `h` | Descend into / return from session → tabs → windows |
| `e` | Expand or collapse the selected session or tab |
| `/` | Fuzzy-filter; `tab` / `shift+tab` change filter; `enter` / `esc` returns to command mode |
| `space` | Toggle a project or SSH host for a multi-tab session |
| `n` | Name and create a session (one tab per selected item) |
| `c` | Clone a Git repository into an editable destination |
| `C` | Check out a GitHub PR (URL, `owner/repo#123`, or a bare number on a selected project) |
| `s` | Save the selected entry's tabs, splits, and working directories |
| `S` | Also save foreground commands so restoring reruns them |
| `p` `0`–`9` | Pin session to a slot (repeat to replace); `p` `x` unpins |
| `r` | Rename session / tab / window (empty session name resets it) |
| `w` | Open the project's Worktrees surface |
| `o` | Open the selected worktree's exact PR in the browser |
| `D` `y` | Destroy the focused entry; in Worktrees, destroy worktree + branch |
| `X` `y` | Remove non-current worktrees merged by Git ancestry or a matching PR |
| `x` `y` | Close workspace / tab / window; in Worktrees, remove the worktree |
| `q` | Quit |

Arrow keys also move through rows and the hierarchy.

### Worktrees

Opened with `w`, closed with `esc`. Rows are ordered: default branch, open PR,
merged PR, closed PR, then entries without a matching PR.

| Key | Action |
|---|---|
| `n` | Create |
| `enter` | Open / focus |
| `r` | Fetch and refresh |
| `p` | Pull |
| `g` | Open the exact PR |
| `x` | Remove the worktree |
| `D` | Destroy worktree and branch |
| `X` | Remove merged worktrees |
| `space` | Enable bulk pull / removal |

### Agents

A flat, most-recently-focused list of Kitty windows running Codex or pi.

| Key | Action |
|---|---|
| `enter` | Focus the agent window |
| `p` | Toggle the live terminal preview |
| `/` | Fuzzy-search agent, project, tab, command, and directory fields |

## Configuration

All optional — `${XDG_CONFIG_HOME:-~/.config}/kesh/config.yaml`. These are the
defaults:

```yaml
clone:      # where new clones land
  root: ~/workspace

worktree:   # where worktrees are created
  root: ~/worktree

checkout:   # where existing clones are searched (defaults to the clone root)
  # root: ~/workspace
```

Press `c` for a clone form (Git URL + inferred destination; `tab` switches
fields, `enter` clones, then adds to zoxide and opens the workspace). Press
`C` to check out a PR — re-checking the same PR focuses its existing worktree.
Workspace names are aliases stored in `names.json`; search matches both the
alias and the original project or SSH name.

## How it works

Each row is one source: a single-project Kitty session and its zoxide entry
share a folder row; multi-project sessions stay as separate session rows;
SSH locations are marked distinctly. A detail panel follows the selected row
and adapts to its type — project, workspace, tab, window, agent, or worktree.

Pins (`Cmd+0`–`Cmd+9`) switch sessions through Kitty's native `goto_session`
without starting Kesh each time; they live for the current Kitty run and clear
on quit. Saved states restore tabs, splits, and working directories (and
optionally rerun commands); closed saved entries stay available to reopen.

### Filters & launch commands

Every filter has a matching subcommand. In Kitty, `Cmd+Shift+O` opens the full
hierarchy as an overlay and `Cmd+Shift+P` opens Agents in a tab.

| Command | Opens |
|---|---|
| `kesh` | All (full hierarchy) |
| `kesh open` | Open sessions |
| `kesh projects` | Projects |
| `kesh ssh` | SSH hosts |
| `kesh saved` | Saved sessions |
| `kesh agents` | Active agents |

---

Architecture, validation, the release process, and behavioral contracts live
in [AGENTS.md](AGENTS.md).
