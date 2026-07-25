// Package state owns Kesh's versioned persisted formats and atomic writes.
package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func atomicJSON(path, pattern string, value any, newline bool) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	content, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if newline {
		content = append(content, '\n')
	}
	return atomicBytes(path, pattern, content)
}

func atomicBytes(path, pattern string, content []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), pattern)
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryName, path); err != nil {
		return fmt.Errorf("rename state file: %w", err)
	}
	return nil
}
