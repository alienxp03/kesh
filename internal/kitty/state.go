package kitty

import (
	"encoding/json"
	"fmt"
)

type State []OSWindow

type OSWindow struct {
	Tabs []Tab `json:"tabs"`
}

type Tab struct {
	ID       int      `json:"id"`
	Title    string   `json:"title"`
	IsActive bool     `json:"is_active"`
	Windows  []Window `json:"windows"`
}

type Window struct {
	ID                  int                 `json:"id"`
	Cmdline             []string            `json:"cmdline"`
	Title               string              `json:"title"`
	CWD                 string              `json:"cwd"`
	SessionName         string              `json:"session_name"`
	LastFocusedAt       float64             `json:"last_focused_at"`
	Env                 map[string]string   `json:"env"`
	ForegroundProcesses []ForegroundProcess `json:"foreground_processes"`
}

type ForegroundProcess struct {
	Cmdline []string `json:"cmdline"`
	CWD     string   `json:"cwd"`
}

func DecodeState(content []byte) (State, error) {
	var state State
	if err := json.Unmarshal(content, &state); err != nil {
		return nil, fmt.Errorf("decode kitty state: %w", err)
	}
	return state, nil
}

func (c Client) State() (State, error) {
	content, err := c.List()
	if err != nil {
		return nil, err
	}
	return DecodeState(content)
}
