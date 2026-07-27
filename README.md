# kesh

kesh is a keyboard-driven [Kitty](https://sw.kovidgoyal.net/kitty/) workspace manager.

![kesh](docs/images/kesh.png)

## Features

- **Keyboard-driven Kitty picker** — browse and switch projects, sessions, tabs, windows, SSH hosts, and saved layouts.
- **Project layouts** — define workspaces, panes, startup commands, and worktree setup in `.kesh.yaml`.
- **Zoxide integration** — discover and open your frequently used projects quickly.
- **Save and restore sessions** — build on Kitty’s [session support](https://sw.kovidgoyal.net/kitty/sessions/) to save tabs, splits, and working directories, with optional foreground command restoration.
- **Git worktrees and pull requests** — create worktrees, clone repositories, and check out GitHub pull requests.
- **Native Kitty pins** — assign Kitty shortcuts to sessions during the current Kitty run.
- **Agent visibility** — find active `Codex`, `Claude` and `pi` windows with live terminal previews.

## Install

```sh
brew install alienxp03/tap/kesh
```

## Kitty setup

kesh controls Kitty through remote control. Add this to `kitty.conf`:

```conf
allow_remote_control yes
listen_on unix:/tmp/kitty
enabled_layouts splits:split_axis=horizontal,tall,stack

map cmd+shift+o launch --type=overlay kesh
```

Reload Kitty after changing its configuration. The `enabled_layouts` setting
keeps the nested pane layouts used by kesh intact. You can also start kesh from
a shell by running:

```sh
kesh
```

## First use

1. Open kesh with `cmd+shift+o`, or run `kesh` from a shell.
2. Select a project and press `enter`.
3. Use `l` and `h` to move through sessions, tabs, and windows.

## Common keybindings

| Key              | Action                                                        |
| ---------------- | ------------------------------------------------------------- |
| `enter`          | Open, focus, or restore the selected item                     |
| `n`              | Create a named session; use `space` to select multiple projects |
| `o`              | Open a project with its configured `.kesh.yaml` layout        |
| `s`              | Save the current layout                                       |
| `S`              | Save the layout and restart foreground commands when restored |
| `w`              | Open the worktree manager                                     |
| `c`              | Clone a repository                                            |
| `C`              | Check out a GitHub pull request                               |
| `p` then `0`–`9` | Pin a session                                                 |
| `r`              | Rename a session, tab, or window                              |
| `x` then `y`     | Close or remove the selected item                             |
| `?`              | Show all keymaps                                              |
| `q`              | Quit                                                          |

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

## Development

This section is for contributors. End users should install the Homebrew
package above.

```sh
make ci       # format check, lint, tests, and build
make build    # installs the development binary to ~/.local/bin/kesh
```

See [AGENTS.md](AGENTS.md) for project architecture and contributor
instructions.
