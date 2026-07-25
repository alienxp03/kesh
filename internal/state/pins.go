package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const CurrentPinVersion = 2

type PinTarget struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Kind        string `json:"kind,omitempty"`
	SessionFile string `json:"session_file,omitempty"`
	Version     int    `json:"version,omitempty"`
}

type Pins map[string]PinTarget

func LoadPins(path, sessionsDirectory string) (Pins, error) {
	pins := Pins{}
	content, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return pins, nil
	}
	if err != nil {
		return pins, fmt.Errorf("read pins: %w", err)
	}
	if err := json.Unmarshal(content, &pins); err != nil {
		return Pins{}, fmt.Errorf("invalid pin state: %w", err)
	}
	seenTargets := map[string]string{}
	sessionPrefix := filepath.Clean(sessionsDirectory) + string(os.PathSeparator)
	for slot, target := range pins {
		slotNumber, slotErr := strconv.Atoi(slot)
		if slotErr != nil || slotNumber < 0 || slotNumber > 9 || target.Key == "" {
			return Pins{}, fmt.Errorf("invalid pin entry for slot %q", slot)
		}
		if target.Kind != "" && target.Kind != "workspace" && target.Kind != "project" && target.Kind != "ssh" {
			return Pins{}, fmt.Errorf("invalid pin kind for slot %s", slot)
		}
		if target.Version != 0 && target.Version != CurrentPinVersion {
			return Pins{}, fmt.Errorf("unsupported pin version for slot %s", slot)
		}
		if target.SessionFile != "" && !strings.HasPrefix(filepath.Clean(target.SessionFile), sessionPrefix) {
			return Pins{}, fmt.Errorf("invalid session file for slot %s", slot)
		}
		if previous, exists := seenTargets[target.Key]; exists {
			return Pins{}, fmt.Errorf("session is pinned more than once: slots %s and %s", previous, slot)
		}
		seenTargets[target.Key] = slot
	}
	return pins, nil
}

func SavePins(path string, pins Pins) error {
	if err := atomicJSON(path, ".pins-*.json", pins, true); err != nil {
		return fmt.Errorf("save pins: %w", err)
	}
	return nil
}
