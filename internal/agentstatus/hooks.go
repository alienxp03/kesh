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
)

const AgentHookName = "kesh-status.py"

//go:embed agent_hook.py
var agentHook []byte

type hookEvent struct {
	name    string
	matcher string
	status  string
}

type hookIntegration struct {
	tool       string
	directory  string
	configPath string
	events     []hookEvent
}

func CodexDirectory() string {
	if configured := os.Getenv("CODEX_HOME"); configured != "" {
		return expandHome(configured)
	}
	return filepath.Join(os.Getenv("HOME"), ".codex")
}

func ClaudeDirectory() string {
	if configured := os.Getenv("CLAUDE_CONFIG_DIR"); configured != "" {
		return expandHome(configured)
	}
	return filepath.Join(os.Getenv("HOME"), ".claude")
}

func integration(tool string) (hookIntegration, error) {
	var result hookIntegration
	switch tool {
	case "codex":
		result = hookIntegration{
			tool: tool, directory: CodexDirectory(), configPath: filepath.Join(CodexDirectory(), "hooks.json"),
			events: []hookEvent{
				{name: "SessionStart", matcher: "startup|resume|clear", status: "idle"},
				{name: "UserPromptSubmit", status: "working"},
				{name: "Stop", status: "finished"},
				{name: "SessionEnd", status: "remove"},
			},
		}
	case "claude":
		result = hookIntegration{
			tool: tool, directory: ClaudeDirectory(), configPath: filepath.Join(ClaudeDirectory(), "settings.json"),
			events: []hookEvent{
				{name: "SessionStart", matcher: "startup|resume|clear|fork", status: "idle"},
				{name: "UserPromptSubmit", status: "working"},
				{name: "Stop", status: "finished"},
				{name: "StopFailure", status: "errored"},
				{name: "SessionEnd", status: "remove"},
			},
		}
	default:
		return result, fmt.Errorf("unsupported agent integration %q", tool)
	}
	return result, nil
}

func InstallHooks(tool string) (string, error) {
	integration, err := integration(tool)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Join(integration.directory, "hooks"), 0o700); err != nil {
		return integration.configPath, fmt.Errorf("create %s hooks directory: %w", tool, err)
	}
	scriptPath := filepath.Join(integration.directory, "hooks", AgentHookName)
	if err := atomicWrite(scriptPath, agentHook, 0o700); err != nil {
		return integration.configPath, fmt.Errorf("install %s status hook: %w", tool, err)
	}
	document, err := readJSONObject(integration.configPath)
	if err != nil {
		return integration.configPath, err
	}
	hooks, err := objectField(document, "hooks")
	if err != nil {
		return integration.configPath, fmt.Errorf("update %s: %w", integration.configPath, err)
	}
	for _, event := range integration.events {
		command := hookCommand(scriptPath, tool, event.status)
		groups, err := hookGroups(hooks[event.name])
		if err != nil {
			return integration.configPath, fmt.Errorf("update %s hook: %w", event.name, err)
		}
		groups = removeManagedCommand(groups, scriptPath)
		handler := map[string]any{
			"type":          "command",
			"command":       command,
			"timeout":       3,
			"statusMessage": "Updating Kesh agent status",
		}
		group := map[string]any{"hooks": []any{handler}}
		if event.matcher != "" {
			group["matcher"] = event.matcher
		}
		hooks[event.name] = append(groups, group)
	}
	if err := writeJSONObject(integration.configPath, document); err != nil {
		return integration.configPath, err
	}
	return integration.configPath, nil
}

func RemoveHooks(tool string) (string, error) {
	integration, err := integration(tool)
	if err != nil {
		return "", err
	}
	scriptPath := filepath.Join(integration.directory, "hooks", AgentHookName)
	document, err := readJSONObject(integration.configPath)
	if err != nil {
		return integration.configPath, err
	}
	if hooksValue, exists := document["hooks"]; exists {
		hooks, ok := hooksValue.(map[string]any)
		if !ok {
			return integration.configPath, fmt.Errorf("hooks in %s must be an object", integration.configPath)
		}
		for _, event := range integration.events {
			groups, err := hookGroups(hooks[event.name])
			if err != nil {
				return integration.configPath, err
			}
			groups = removeManagedCommand(groups, scriptPath)
			if len(groups) == 0 {
				delete(hooks, event.name)
			} else {
				hooks[event.name] = groups
			}
		}
	}
	if _, err := os.Stat(integration.configPath); err == nil {
		if err := writeJSONObject(integration.configPath, document); err != nil {
			return integration.configPath, err
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return integration.configPath, err
	}
	if err := os.Remove(scriptPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return integration.configPath, fmt.Errorf("remove %s status hook: %w", tool, err)
	}
	return integration.configPath, nil
}

func HooksInstalled(tool string) (bool, string, error) {
	integration, err := integration(tool)
	if err != nil {
		return false, "", err
	}
	scriptPath := filepath.Join(integration.directory, "hooks", AgentHookName)
	content, err := os.ReadFile(scriptPath)
	if errors.Is(err, fs.ErrNotExist) {
		return false, integration.configPath, nil
	}
	if err != nil {
		return false, integration.configPath, err
	}
	if string(content) != string(agentHook) {
		return false, integration.configPath, nil
	}
	document, err := readJSONObject(integration.configPath)
	if err != nil {
		return false, integration.configPath, err
	}
	hooksValue, ok := document["hooks"].(map[string]any)
	if !ok {
		return false, integration.configPath, nil
	}
	for _, event := range integration.events {
		groups, err := hookGroups(hooksValue[event.name])
		if err != nil || !containsCommand(groups, hookCommand(scriptPath, tool, event.status)) {
			return false, integration.configPath, err
		}
	}
	return true, integration.configPath, nil
}

func readJSONObject(path string) (map[string]any, error) {
	content, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var document map[string]any
	if err := json.Unmarshal(content, &document); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return document, nil
}

func objectField(document map[string]any, key string) (map[string]any, error) {
	if value, exists := document[key]; exists {
		object, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s must be an object", key)
		}
		return object, nil
	}
	object := map[string]any{}
	document[key] = object
	return object, nil
}

func hookGroups(value any) ([]any, error) {
	if value == nil {
		return nil, nil
	}
	groups, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("hook groups must be an array")
	}
	return groups, nil
}

func removeManagedCommand(groups []any, scriptPath string) []any {
	result := make([]any, 0, len(groups))
	for _, value := range groups {
		group, ok := value.(map[string]any)
		if !ok {
			result = append(result, value)
			continue
		}
		handlers, ok := group["hooks"].([]any)
		if !ok {
			result = append(result, value)
			continue
		}
		kept := handlers[:0]
		for _, handlerValue := range handlers {
			handler, ok := handlerValue.(map[string]any)
			command, _ := handler["command"].(string)
			if !ok || !strings.Contains(command, scriptPath) {
				kept = append(kept, handlerValue)
			}
		}
		if len(kept) > 0 {
			group["hooks"] = kept
			result = append(result, group)
		}
	}
	return result
}

func containsCommand(groups []any, expected string) bool {
	for _, value := range groups {
		group, _ := value.(map[string]any)
		handlers, _ := group["hooks"].([]any)
		for _, handlerValue := range handlers {
			handler, _ := handlerValue.(map[string]any)
			if command, _ := handler["command"].(string); command == expected {
				return true
			}
		}
	}
	return false
}

func hookCommand(scriptPath, tool, status string) string {
	return shellQuote(scriptPath) + " " + tool + " " + status
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func writeJSONObject(path string, document map[string]any) error {
	content, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := atomicWrite(path, append(content, '\n'), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
