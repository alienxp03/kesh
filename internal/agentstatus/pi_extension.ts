import { mkdir, readFile, rename, rm, writeFile } from "node:fs/promises";
import { homedir } from "node:os";
import { dirname, join } from "node:path";
import type { ExtensionAPI, ExtensionContext } from "@earendil-works/pi-coding-agent";

const VERSION = 1;
const windowID = Number.parseInt(process.env.KITTY_WINDOW_ID ?? "", 10);
const stateHome = process.env.XDG_STATE_HOME || join(homedir(), ".local", "state");
const statusFile = join(stateHome, "kesh", "agent-status", `pi-${windowID}.json`);

type Status = "idle" | "working" | "finished" | "errored";
type StatusRecord = { lastDoneAt?: string; status?: Status; updatedAt?: string };

function isTerminalStatus(status: Status | undefined): boolean {
	return status === "finished" || status === "errored";
}

export default function (pi: ExtensionAPI) {
	if (!Number.isInteger(windowID) || windowID <= 0) return;

	let settledStatus: Status = "idle";
	let ownsStatus = false;
	let parentWorking = false;
	let runningSubagents = 0;
	let statusContext: ExtensionContext | undefined;
	let pendingWrite: Promise<void> = Promise.resolve();

	async function writeStatus(status: Status, ctx: ExtensionContext) {
		await mkdir(dirname(statusFile), { recursive: true, mode: 0o700 });
		let lastDoneAt: string | undefined;
		try {
			const current = JSON.parse(await readFile(statusFile, "utf8")) as StatusRecord;
			lastDoneAt = current.lastDoneAt;
			if (!lastDoneAt && isTerminalStatus(current.status)) lastDoneAt = current.updatedAt;
		} catch {
			// A missing or malformed previous record has no completion timestamp.
		}
		const updatedAt = new Date().toISOString();
		if (isTerminalStatus(status)) lastDoneAt = updatedAt;
		const temporary = `${statusFile}.${process.pid}.tmp`;
		const record = {
			version: VERSION,
			tool: "pi",
			windowId: windowID,
			pid: process.pid,
			sessionId: ctx.sessionManager.getSessionId(),
			status,
			updatedAt,
			...(lastDoneAt ? { lastDoneAt } : {}),
		};
		await writeFile(temporary, `${JSON.stringify(record)}\n`, { mode: 0o600 });
		await rename(temporary, statusFile);
	}

	function writeCurrentStatus(ctx: ExtensionContext): Promise<void> {
		const status = parentWorking || runningSubagents > 0 ? "working" : settledStatus;
		// Lifecycle hooks and subagent events may overlap. Serialize writes so
		// an older status cannot overwrite a newer one (or reuse the temp file).
		pendingWrite = pendingWrite.catch(() => {}).then(() => writeStatus(status, ctx));
		return pendingWrite;
	}

	pi.events.on("kesh:subagents", (data) => {
		if (!ownsStatus || !statusContext) return;
		const count = (data as { running?: number })?.running;
		if (typeof count !== "number" || !Number.isInteger(count) || count < 0 || count === runningSubagents) return;
		runningSubagents = count;
		void writeCurrentStatus(statusContext).catch(() => {});
	});

	pi.on("session_start", async (_event, ctx) => {
		// Headless Pi subagents inherit KITTY_WINDOW_ID, and SDK children can
		// even share the parent's PID. Only the visible TUI owns this window's
		// status file; otherwise children overwrite or delete the parent state.
		ownsStatus = ctx.mode === "tui";
		if (!ownsStatus) return;
		statusContext = ctx;
		parentWorking = false;
		runningSubagents = 0;
		settledStatus = "idle";
		await writeCurrentStatus(ctx);
	});

	pi.on("agent_start", async (_event, ctx) => {
		if (!ownsStatus || ctx.mode !== "tui") return;
		parentWorking = true;
		settledStatus = "finished";
		await writeCurrentStatus(ctx);
	});

	pi.on("agent_end", async (event) => {
		if (!ownsStatus) return;
		const lastAssistant = [...event.messages]
			.reverse()
			.find((message) => message.role === "assistant") as { stopReason?: string } | undefined;
		settledStatus = lastAssistant?.stopReason === "error" ? "errored" : "finished";
	});

	pi.on("agent_settled", async (_event, ctx) => {
		if (!ownsStatus || ctx.mode !== "tui") return;
		parentWorking = false;
		await writeCurrentStatus(ctx);
	});

	pi.on("session_shutdown", async () => {
		if (!ownsStatus) return;
		ownsStatus = false;
		statusContext = undefined;
		await pendingWrite.catch(() => {});
		// Read first so a stale shutdown from a replaced process cannot remove a
		// newer Pi process's status for the same Kitty window.
		try {
			const current = JSON.parse(await readFile(statusFile, "utf8")) as { pid?: number };
			if (current.pid === process.pid) await rm(statusFile, { force: true });
		} catch {
			// Missing or malformed status is already equivalent to no integration state.
		}
	});
}
