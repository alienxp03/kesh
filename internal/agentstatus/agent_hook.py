#!/usr/bin/env python3
"""Write Kesh agent lifecycle state from Codex or Claude Code hooks."""

import json
import os
import sys
from datetime import datetime, timezone
from pathlib import Path

VERSION = 1
VALID_TOOLS = {"codex", "claude"}
VALID_STATUSES = {"idle", "working", "finished", "errored", "remove"}


def main() -> None:
    if len(sys.argv) != 3 or sys.argv[1] not in VALID_TOOLS or sys.argv[2] not in VALID_STATUSES:
        return
    tool, status = sys.argv[1], sys.argv[2]
    try:
        window_id = int(os.environ.get("KITTY_WINDOW_ID", ""))
    except ValueError:
        return
    if window_id <= 0:
        return

    try:
        payload = json.load(sys.stdin)
    except (json.JSONDecodeError, OSError):
        payload = {}
    session_id = str(payload.get("session_id", ""))
    state_home = Path(os.environ.get("XDG_STATE_HOME", Path.home() / ".local" / "state"))
    status_file = state_home / "kesh" / "agent-status" / f"{tool}-{window_id}.json"

    if status == "remove":
        try:
            current = json.loads(status_file.read_text(encoding="utf-8"))
            if not session_id or current.get("sessionId") == session_id:
                status_file.unlink(missing_ok=True)
        except (OSError, json.JSONDecodeError):
            pass
        return

    status_file.parent.mkdir(parents=True, mode=0o700, exist_ok=True)
    record = {
        "version": VERSION,
        "tool": tool,
        "windowId": window_id,
        "pid": os.getppid(),
        "sessionId": session_id,
        "status": status,
        "updatedAt": datetime.now(timezone.utc).isoformat().replace("+00:00", "Z"),
    }
    temporary = status_file.with_name(f".{status_file.name}.{os.getpid()}.tmp")
    temporary.write_text(json.dumps(record, separators=(",", ":")) + "\n", encoding="utf-8")
    temporary.chmod(0o600)
    temporary.replace(status_file)


if __name__ == "__main__":
    main()
