package agentstatus

import (
	"encoding/json"
	"os"
	"path/filepath"
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
	if err := Acknowledge(directory, 42); err != nil {
		t.Fatal(err)
	}
	records, err = ReadDirectory(directory)
	if err != nil || records[42].Status != "idle" {
		t.Fatalf("acknowledged records = %#v, %v", records, err)
	}
}

func TestRemovePiStatusesKeepsOtherIntegrations(t *testing.T) {
	directory := t.TempDir()
	for _, name := range []string{"pi-1.json", "pi-2.json", "codex-1.json", "pi-not-status.txt"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := RemovePiStatuses(directory); err != nil {
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

func TestReadDirectoryRejectsUnknownAndMismatchedRecords(t *testing.T) {
	directory := t.TempDir()
	for name, record := range map[string]Record{
		"version.json": {Version: 99, Tool: "pi", WindowID: 1, PID: 1, Status: "idle"},
		"tool.json":    {Version: 1, Tool: "codex", WindowID: 2, PID: 1, Status: "idle"},
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
