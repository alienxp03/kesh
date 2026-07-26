# kesh

kesh is a keyboard-driven workspace manager for [Kitty](https://sw.kovidgoyal.net/kitty/).
It helps you find projects, open repeatable layouts, create Git worktrees, and
return to saved terminal sessions.

## Install

```sh
brew install alienxp03/tap/kesh
```

Upgrade later with:

```sh
brew upgrade kesh
```

## Kitty setup

kesh controls Kitty through remote control. Add this to `kitty.conf`:

```conf
allow_remote_control yes
listen_on unix:/tmp/kitty

map cmd+shift+o launch --type=overlay kesh
```

Reload Kitty after changing its configuration. You can also start kesh from a
shell by running:

```sh
kesh
```

## First use

1. Open kesh with `cmd+shift+o`, or run `kesh` from a shell.
2. Select a project and press `enter`.
3. Use `l` and `h` to move through sessions, tabs, and windows.

kesh discovers projects from Kitty, zoxide, and your saved sessions.

## Everyday actions

| Key | Action |
|---|---|
| `enter` | Open, focus, or restore the selected item |
| `n` | Create a named session from selected projects |
| `s` | Save the current layout |
| `S` | Save the layout and restart foreground commands when restored |
| `w` | Open the worktree manager |
| `c` | Clone a repository |
| `C` | Check out a GitHub pull request |
| `p` then `0`–`9` | Pin a session |
| `r` | Rename a session, tab, or window |
| `x` then `y` | Close or remove the selected item |
| `?` | Show all keymaps |
| `q` | Quit |

### Saved sessions

Press `s` on an open session to save its tabs, splits, and working directories.
Use `S` when you also want kesh to capture the foreground commands. Be careful:
restoring the session can run those commands again. See Kitty's
[`--use-foreground-process` documentation](https://sw.kovidgoyal.net/kitty/sessions/#the-save_as_session-action)
before using it with commands that have side effects.

Saved sessions remain in the list after Kitty closes. Select one and press
`enter` to restore it.

### Worktrees

Press `w`, then `n`, to create a branch worktree. kesh opens it in Kitty and
uses the project layout when one is configured.

### Agents

Run `kesh agents` to list active Claude, Codex, and pi windows. Press `enter`
to focus one and `p` to show its live terminal preview.

## Project configuration

Run this from a project root:

```sh
kesh init
```

This creates `.kesh.yaml`. A minimal layout looks like this:

```yaml
workspaces:
  - name: app
    repo: .
    panes:
      - commands: [nvim]
        focus: true
      - commands: [pnpm run dev]
        split: horizontal
      - split: horizontal
```

`workspaces` can describe multiple repositories. `panes` defines the tabs and
splits kesh opens. Worktree settings can add one-time setup such as copied
files, symlinks, hooks, and randomized ports.

Global paths are configured in `~/.config/kesh/config.yaml`:

```yaml
clone:
  root: ~/workspace
worktree:
  root: ~/workspace/worktrees
checkout:
  root: ~/workspace
```

## Manual Kitty sessions

kesh does not replace Kitty's native session files. If you use a Kitty mapping
such as:

```conf
map ctrl+b>k goto_session ~/path/to/project.kitty-session
```

that session appears in kesh automatically while it is open. Closed manual
Kitty sessions remain available through Kitty's own mapping; use kesh's `s` if
you want the session to remain restorable in the kesh list.

## Pins

kesh can generate native Kitty shortcuts for pinned sessions. Include the
generated file in `kitty.conf`:

```conf
include ~/.local/state/kesh/kitty-pins.conf

map cmd+0 kesh_pin_0
map cmd+1 kesh_pin_1
map cmd+2 kesh_pin_2
map cmd+3 kesh_pin_3
map cmd+4 kesh_pin_4
map cmd+5 kesh_pin_5
map cmd+6 kesh_pin_6
map cmd+7 kesh_pin_7
map cmd+8 kesh_pin_8
map cmd+9 kesh_pin_9
```

Pins belong to the current Kitty run. kesh clears them when Kitty exits
normally or when it detects a stale Kitty process.

## CLI

```text
kesh                         Open the workspace picker
kesh init                    Create .kesh.yaml in the current directory
kesh agents                 Show active agent windows
kesh ssh                    Show configured SSH hosts
kesh saved                  Show saved sessions
kesh switch SLOT             Focus pin 0–9
kesh clear-pins              Clear pins
kesh clear-pins --on-quit    Clear pins when Kitty quits
```

## Troubleshooting

### kesh cannot connect to Kitty

Confirm that Kitty contains:

```conf
allow_remote_control yes
listen_on unix:/tmp/kitty
```

Then reload Kitty and run `kesh` again.

### Nested panes are flattened

Enable Kitty's `tall` layout:

```conf
enabled_layouts splits:split_axis=horizontal,tall,stack
```

## Development

This section is for contributors. End users should install the Homebrew
package above.

```sh
make ci       # format check, lint, tests, and build
make build    # writes the development binary to ./bin/kesh
```

See [AGENTS.md](AGENTS.md) for project architecture and contributor
instructions. See [docs/manual-smoke.md](docs/manual-smoke.md) for checks that
require a real Kitty instance.
