package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const CurrentSavedSessionVersion = 1

type SavedSessionRecord struct {
	Name               string   `json:"name"`
	SessionName        string   `json:"session_name"`
	SessionFile        string   `json:"session_file"`
	Projects           []string `json:"projects,omitempty"`
	ForegroundCommands bool     `json:"foreground_commands,omitempty"`
	SavedAt            string   `json:"saved_at"`
}

type SavedSessions struct {
	Version  int                           `json:"version"`
	Sessions map[string]SavedSessionRecord `json:"sessions"`
}

func LoadSavedSessions(path, sessionsDirectory string) (SavedSessions, error) {
	store := SavedSessions{Version: CurrentSavedSessionVersion, Sessions: map[string]SavedSessionRecord{}}
	content, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return store, nil
	}
	if err != nil {
		return store, fmt.Errorf("read saved sessions: %w", err)
	}
	if err := json.Unmarshal(content, &store); err != nil {
		return SavedSessions{}, fmt.Errorf("invalid saved session state: %w", err)
	}
	if store.Version != CurrentSavedSessionVersion || store.Sessions == nil {
		return SavedSessions{}, fmt.Errorf("unsupported saved session state version: %d", store.Version)
	}
	directory := filepath.Clean(sessionsDirectory) + string(os.PathSeparator)
	seenNames := map[string]bool{}
	for key, record := range store.Sessions {
		file := filepath.Clean(record.SessionFile)
		if key != file || !hasPathPrefix(file, directory) {
			return SavedSessions{}, fmt.Errorf("invalid saved session file: %s", record.SessionFile)
		}
		if record.Name == "" || record.SessionName == "" || seenNames[record.SessionName] {
			return SavedSessions{}, fmt.Errorf("invalid saved session metadata for %s", file)
		}
		seenNames[record.SessionName] = true
		for _, project := range record.Projects {
			if !filepath.IsAbs(project) {
				return SavedSessions{}, fmt.Errorf("invalid saved session project: %s", project)
			}
		}
	}
	return store, nil
}

func hasPathPrefix(path, directory string) bool {
	return len(path) > len(directory) && path[:len(directory)] == directory
}

func SaveSavedSessions(path string, store SavedSessions) error {
	store.Version = CurrentSavedSessionVersion
	if store.Sessions == nil {
		store.Sessions = map[string]SavedSessionRecord{}
	}
	if err := atomicJSON(path, ".saved-sessions-*.json", store, true); err != nil {
		return fmt.Errorf("save saved sessions: %w", err)
	}
	return nil
}

func SavedSessionForName(store SavedSessions, sessionName string) (SavedSessionRecord, bool) {
	for _, record := range store.Sessions {
		if record.SessionName == sessionName {
			return record, true
		}
	}
	return SavedSessionRecord{}, false
}
