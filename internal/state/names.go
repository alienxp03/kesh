package state

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type Names map[string]string

func LoadNames(path string) (Names, error) {
	names := Names{}
	content, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return names, nil
	}
	if err != nil {
		return names, fmt.Errorf("read workspace names: %w", err)
	}
	if err := json.Unmarshal(content, &names); err != nil {
		return Names{}, fmt.Errorf("invalid workspace names: %w", err)
	}
	for key, name := range names {
		if key == "" || strings.TrimSpace(name) == "" {
			return Names{}, fmt.Errorf("invalid workspace name for %q", key)
		}
	}
	return names, nil
}

func SaveNames(path string, names Names) error {
	if err := atomicJSON(path, ".names-*.json", names, true); err != nil {
		return fmt.Errorf("save workspace names: %w", err)
	}
	return nil
}
