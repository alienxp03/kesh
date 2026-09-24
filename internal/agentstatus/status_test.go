package agentstatus

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInstallPiIsIdempotentAndRemovable(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PI_CODING_AGENT_DIR", directory)

	path, err := InstallPi()
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(directory, "extensions", PiExtensionName) {
		t.Fatalf("extension path = %q", path)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) == 0 {
		t.Fatal("installed extension is empty")
	}
	if _, err := InstallPi(); err != nil {
		t.Fatalf("idempotent install: %v", err)
	}
	installed, _, err := PiInstalled()
	if err != nil || !installed {
		t.Fatalf("PiInstalled = %t, %v", installed, err)
	}
	if _, err := RemovePi(); err != nil {
		t.Fatal(err)
	}
	installed, _, err = PiInstalled()
	if err != nil || installed {
		t.Fatalf("PiInstalled after remove = %t, %v", installed, err)
	}
}

func TestPiExtensionIgnoresHeadlessSubagents(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is unavailable")
	}
	directory := t.TempDir()
	extension := filepath.Join(directory, "kesh-status.ts")
	if err := os.WriteFile(extension, piExtension, 0o600); err != nil {
		t.Fatal(err)
	}
	probe := filepath.Join(directory, "probe.mjs")
	script := `
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { join } from "node:path";
import { pathToFileURL } from "node:url";
const { default: install } = await import(pathToFileURL(process.argv[2]).href);
const file = join(process.env.XDG_STATE_HOME, "kesh", "agent-status", "pi-42.json");
function session(mode) {
  const handlers = new Map();
  const events = new Map();
  install({
    on(event, callback) { handlers.set(event, callback); },
    events: { on(event, callback) { events.set(event, callback); } },
  });
  const ctx = { mode, sessionManager: { getSessionId: () => mode } };
  return async (event, payload = {}) => event === "subagents"
    ? events.get("kesh:subagents")?.(payload)
    : handlers.get(event)?.(payload, ctx);
}
async function status() { try { return JSON.parse(await readFile(file, "utf8")); } catch { return null; } }
async function waitStatus(expected) {
  for (let i = 0; i < 100; i++) {
    if ((await status())?.status === expected) return;
    await new Promise((resolve) => setTimeout(resolve, 10));
  }
  assert.equal((await status())?.status, expected);
}
const child = session("print");
await child("session_start");
await child("agent_start");
assert.equal(await status(), null);
const parent = session("tui");
await parent("session_start");
await parent("agent_start");
assert.equal((await status()).status, "working");
await parent("subagents", { running: 1 });
await child("agent_settled");
await child("session_shutdown");
assert.equal((await status()).status, "working");
await parent("agent_end", { messages: [{ role: "assistant", stopReason: "stop" }] });
await parent("agent_settled");
assert.equal((await status()).status, "working", "child keeps status working after parent settles");
await parent("subagents", { running: 0 });
await waitStatus("finished");
await parent("session_shutdown");
assert.equal(await status(), null);
`
	if err := os.WriteFile(probe, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(node, "--no-warnings", probe, extension)
	command.Env = append(os.Environ(), "KITTY_WINDOW_ID=42", "XDG_STATE_HOME="+directory)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("Pi extension status probe: %v\n%s", err, output)
	}
}

func TestPiAgentDirectoryHonorsEnvironmentAndHome(t *testing.T) {
	t.Setenv("HOME", "/home/stan")
	t.Setenv("PI_CODING_AGENT_DIR", "~/custom-pi")
	if got := PiAgentDirectory(); got != "/home/stan/custom-pi" {
		t.Fatalf("configured agent directory = %q", got)
	}
	t.Setenv("PI_CODING_AGENT_DIR", "")
	if got := PiAgentDirectory(); got != "/home/stan/.pi/agent" {
		t.Fatalf("default agent directory = %q", got)
	}
}

func TestReadDirectoryAndAcknowledge(t *testing.T) {
	directory := t.TempDir()
	record := Record{
		Version: CurrentVersion, Tool: "pi", WindowID: 42, PID: 123,
		SessionID: "session", Status: "finished", UpdatedAt: time.Now().UTC(),
	}
	content, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "pi-42.json"), content, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "broken.json"), []byte("nope"), 0o600); err != nil {
		t.Fatal(err)
	}

	records, err := ReadDirectory(directory)
	if err != nil || len(records) != 1 || records[42].Status != "finished" {
		t.Fatalf("ReadDirectory = %#v, %v", records, err)
	}
	if err := Acknowledge(directory, "pi", 42); err != nil {
		t.Fatal(err)
	}
	records, err = ReadDirectory(directory)
	if err != nil || records[42].Status != "idle" {
		t.Fatalf("acknowledged records = %#v, %v", records, err)
	}
}

func TestRemoveStatusesKeepsOtherIntegrations(t *testing.T) {
	directory := t.TempDir()
	for _, name := range []string{"pi-1.json", "pi-2.json", "codex-1.json", "pi-not-status.txt"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := RemoveStatuses(directory, "pi"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"pi-1.json", "pi-2.json"} {
		if _, err := os.Stat(filepath.Join(directory, name)); !os.IsNotExist(err) {
			t.Fatalf("%s was not removed", name)
		}
	}
	for _, name := range []string{"codex-1.json", "pi-not-status.txt"} {
		if _, err := os.Stat(filepath.Join(directory, name)); err != nil {
			t.Fatalf("%s should remain: %v", name, err)
		}
	}
}

func TestHookIntegrationsPreserveExistingConfiguration(t *testing.T) {
	for _, tool := range []string{"codex", "claude"} {
		t.Run(tool, func(t *testing.T) {
			directory := t.TempDir()
			if tool == "codex" {
				t.Setenv("CODEX_HOME", directory)
			} else {
				t.Setenv("CLAUDE_CONFIG_DIR", directory)
			}
			integration, err := integration(tool)
			if err != nil {
				t.Fatal(err)
			}
			existing := `{"model":"custom","hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo existing"}]}]}}`
			if err := os.WriteFile(integration.configPath, []byte(existing), 0o600); err != nil {
				t.Fatal(err)
			}

			path, err := InstallHooks(tool)
			if err != nil || path != integration.configPath {
				t.Fatalf("InstallHooks = %q, %v", path, err)
			}
			if _, err := InstallHooks(tool); err != nil {
				t.Fatalf("idempotent install: %v", err)
			}
			installed, _, err := HooksInstalled(tool)
			if err != nil || !installed {
				t.Fatalf("HooksInstalled = %t, %v", installed, err)
			}
			document, err := readJSONObject(integration.configPath)
			if err != nil || document["model"] != "custom" {
				t.Fatalf("preserved config = %#v, %v", document, err)
			}
			preToolGroups, _ := hookGroups(document["hooks"].(map[string]any)["PreToolUse"])
			if !containsCommand(preToolGroups, "echo existing") {
				t.Fatalf("existing hook was removed: %#v", preToolGroups)
			}

			if _, err := RemoveHooks(tool); err != nil {
				t.Fatal(err)
			}
			installed, _, err = HooksInstalled(tool)
			if err != nil || installed {
				t.Fatalf("HooksInstalled after remove = %t, %v", installed, err)
			}
			document, err = readJSONObject(integration.configPath)
			if err != nil || document["model"] != "custom" {
				t.Fatalf("config after remove = %#v, %v", document, err)
			}
			preToolGroups, _ = hookGroups(document["hooks"].(map[string]any)["PreToolUse"])
			if !containsCommand(preToolGroups, "echo existing") {
				t.Fatalf("existing hook was removed on uninstall: %#v", preToolGroups)
			}
		})
	}
}

func TestAgentHookWritesAndRemovesLifecycleStatus(t *testing.T) {
	directory := t.TempDir()
	stateHome := t.TempDir()
	t.Setenv("CODEX_HOME", directory)
	if _, err := InstallHooks("codex"); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(directory, "hooks", AgentHookName)
	runHook := func(status string) {
		command := exec.Command(script, "codex", status)
		command.Env = append(os.Environ(), "KITTY_WINDOW_ID=42", "XDG_STATE_HOME="+stateHome)
		command.Stdin = strings.NewReader(`{"session_id":"session-1"}`)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("hook %s: %v: %s", status, err, output)
		}
	}

	runHook("working")
	records, err := ReadDirectory(filepath.Join(stateHome, "kesh", "agent-status"))
	if err != nil || records[42].Tool != "codex" || records[42].Status != "working" {
		t.Fatalf("working status = %#v, %v", records, err)
	}
	runHook("finished")
	if err := Acknowledge(filepath.Join(stateHome, "kesh", "agent-status"), "codex", 42); err != nil {
		t.Fatal(err)
	}
	records, err = ReadDirectory(filepath.Join(stateHome, "kesh", "agent-status"))
	if err != nil || records[42].Status != "idle" {
		t.Fatalf("acknowledged status = %#v, %v", records, err)
	}
	runHook("remove")
	records, err = ReadDirectory(filepath.Join(stateHome, "kesh", "agent-status"))
	if err != nil || len(records) != 0 {
		t.Fatalf("removed status = %#v, %v", records, err)
	}
}

func TestReadDirectoryRejectsUnknownAndMismatchedRecords(t *testing.T) {
	directory := t.TempDir()
	for name, record := range map[string]Record{
		"version.json": {Version: 99, Tool: "pi", WindowID: 1, PID: 1, Status: "idle"},
		"tool.json":    {Version: 1, Tool: "unknown", WindowID: 2, PID: 1, Status: "idle"},
		"status.json":  {Version: 1, Tool: "pi", WindowID: 3, PID: 1, Status: "mystery"},
	} {
		content, _ := json.Marshal(record)
		if err := os.WriteFile(filepath.Join(directory, name), content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	records, err := ReadDirectory(directory)
	if err != nil || len(records) != 0 {
		t.Fatalf("invalid records = %#v, %v", records, err)
	}
}
