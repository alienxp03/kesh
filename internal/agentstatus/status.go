// Package agentstatus installs agent integrations and exchanges lifecycle state
// between those integrations and Kesh.
package agentstatus

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	CurrentVersion  = 1
	PiExtensionName = "kesh-status.ts"
)

//go:embed pi_extension.ts
var piExtension []byte

type Record struct {
	Version   int       `json:"version"`
	Tool      string    `json:"tool"`
	WindowID  int       `json:"windowId"`
	PID       int       `json:"pid"`
	SessionID string    `json:"sessionId"`
	Status    string    `json:"status"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func PiAgentDirectory() string {
	if configured := os.Getenv("PI_CODING_AGENT_DIR"); configured != "" {
		return expandHome(configured)
	}
	return filepath.Join(os.Getenv("HOME"), ".pi", "agent")
}

func PiExtensionPath() string {
	return filepath.Join(PiAgentDirectory(), "extensions", PiExtensionName)
}

func InstallPi() (string, error) {
	path := PiExtensionPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return path, fmt.Errorf("create Pi extensions directory: %w", err)
	}
	if current, err := os.ReadFile(path); err == nil && string(current) == string(piExtension) {
		return path, nil
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return path, fmt.Errorf("read Pi extension: %w", err)
	}
	if err := atomicWrite(path, piExtension, 0o600); err != nil {
		return path, fmt.Errorf("install Pi extension: %w", err)
	}
	return path, nil
}

func RemovePi() (string, error) {
	path := PiExtensionPath()
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return path, fmt.Errorf("remove Pi extension: %w", err)
	}
	return path, nil
}

func RemoveStatuses(directory, tool string) error {
	if !validTool(tool) {
		return fmt.Errorf("unsupported agent integration %q", tool)
	}
	entries, err := os.ReadDir(directory)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read agent statuses: %w", err)
	}
	prefix := tool + "-"
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), prefix) || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		if err := os.Remove(filepath.Join(directory, entry.Name())); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("remove %s agent status: %w", tool, err)
		}
	}
	return nil
}

func PiInstalled() (bool, string, error) {
	path := PiExtensionPath()
	content, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, path, nil
	}
	if err != nil {
		return false, path, fmt.Errorf("read Pi extension: %w", err)
	}
	return string(content) == string(piExtension), path, nil
}

func ReadDirectory(directory string) (map[int]Record, error) {
	records := map[int]Record{}
	entries, err := os.ReadDir(directory)
	if errors.Is(err, fs.ErrNotExist) {
		return records, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read agent statuses: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		content, err := os.ReadFile(filepath.Join(directory, entry.Name()))
		if err != nil {
			continue
		}
		var record Record
		if json.Unmarshal(content, &record) != nil || !validRecord(record) {
			continue
		}
		if current, exists := records[record.WindowID]; !exists || record.UpdatedAt.After(current.UpdatedAt) {
			records[record.WindowID] = record
		}
	}
	return records, nil
}

func Acknowledge(directory, tool string, windowID int) error {
	if !validTool(tool) {
		return nil
	}
	path := filepath.Join(directory, fmt.Sprintf("%s-%d.json", tool, windowID))
	content, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var record Record
	if err := json.Unmarshal(content, &record); err != nil || !validRecord(record) || record.WindowID != windowID {
		return nil
	}
	if record.Status != "finished" && record.Status != "errored" {
		return nil
	}
	record.Status = "idle"
	record.UpdatedAt = time.Now().UTC()
	updated, err := json.Marshal(record)
	if err != nil {
		return err
	}
	return atomicWrite(path, append(updated, '\n'), 0o600)
}

func validRecord(record Record) bool {
	if record.Version != CurrentVersion || !validTool(record.Tool) || record.WindowID <= 0 || record.PID <= 0 {
		return false
	}
	switch record.Status {
	case "idle", "working", "finished", "errored":
		return true
	default:
		return false
	}
}

func validTool(tool string) bool {
	switch tool {
	case "pi", "codex", "claude":
		return true
	default:
		return false
	}
}

func atomicWrite(path string, content []byte, mode fs.FileMode) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".kesh-agent-status-*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

func expandHome(path string) string {
	if path == "~" {
		return os.Getenv("HOME")
	}
	if len(path) > 2 && path[:2] == "~/" {
		return filepath.Join(os.Getenv("HOME"), path[2:])
	}
	return path
}
