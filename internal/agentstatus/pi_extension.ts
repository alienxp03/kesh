import { mkdir, readFile, rename, rm, writeFile } from "node:fs/promises";
import { homedir } from "node:os";
import { dirname, join } from "node:path";
import type { ExtensionAPI, ExtensionContext } from "@earendil-works/pi-coding-agent";

const VERSION = 1;
const windowID = Number.parseInt(process.env.KITTY_WINDOW_ID ?? "", 10);
const stateHome = process.env.XDG_STATE_HOME || join(homedir(), ".local", "state");
const statusFile = join(stateHome, "kesh", "agent-status", `pi-${windowID}.json`);

type Status = "idle" | "working" | "finished" | "errored";

export default function (pi: ExtensionAPI) {
	if (!Number.isInteger(windowID) || windowID <= 0) return;

	let settledStatus: Status = "finished";

	async function writeStatus(status: Status, ctx: ExtensionContext) {
		await mkdir(dirname(statusFile), { recursive: true, mode: 0o700 });
		const temporary = `${statusFile}.${process.pid}.tmp`;
		const record = {
			version: VERSION,
			tool: "pi",
			windowId: windowID,
			pid: process.pid,
			sessionId: ctx.sessionManager.getSessionId(),
			status,
			updatedAt: new Date().toISOString(),
		};
		await writeFile(temporary, `${JSON.stringify(record)}\n`, { mode: 0o600 });
		await rename(temporary, statusFile);
	}

	pi.on("session_start", async (_event, ctx) => {
		settledStatus = "finished";
		await writeStatus("idle", ctx);
	});

	pi.on("agent_start", async (_event, ctx) => {
		settledStatus = "finished";
		await writeStatus("working", ctx);
	});

	pi.on("agent_end", async (event) => {
		const lastAssistant = [...event.messages]
			.reverse()
			.find((message) => message.role === "assistant") as { stopReason?: string } | undefined;
		settledStatus = lastAssistant?.stopReason === "error" ? "errored" : "finished";
	});

	pi.on("agent_settled", async (_event, ctx) => {
		await writeStatus(settledStatus, ctx);
	});

	pi.on("session_shutdown", async () => {
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
