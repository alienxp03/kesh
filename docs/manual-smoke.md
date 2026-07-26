# Kesh manual Kitty smoke checklist

Run this after `mise run kesh` on a machine with Kitty available.

- Launch Kesh from both Kitty mappings (full picker and `agents`).
- Focus, open, and close a workspace, tab, and window.
- Search, rename, create a composed session, clone, and check out a PR.
- Save and restore a session with and without foreground commands.
- With a nested `.kesh.yaml` pane layout, reload Kitty config with `Cmd+R` and verify the layout remains nested rather than flattening into splits.
- Create, open, pull, remove, and open the PR for a worktree.
- Pin, switch, and unpin; then verify a normal Kitty quit clears pin mappings.
- Restart after a forced Kitty termination and verify stale pins are cleared.
- Confirm existing config, state, and PR cache files load without migration errors.
- Verify URL opening on the host platform.
