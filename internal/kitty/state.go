package kitty

import (
	"encoding/json"
	"fmt"
)

type State []OSWindow

type OSWindow struct {
	IsFocused bool  `json:"is_focused"`
	Tabs      []Tab `json:"tabs"`
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
	IsFocused           bool                `json:"is_focused"`
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

// PreviousSessionWindow selects the most recently focused window in a different
// session in the active OS window. Focus its window, not just its tab, so the
// target session's last active window is restored.
func (s State) PreviousSessionWindow() (int, bool) {
	for _, osWindow := range s {
		if !osWindow.IsFocused {
			continue
		}
		var currentSession string
		var active bool
		for _, tab := range osWindow.Tabs {
			if !tab.IsActive {
				continue
			}
			for _, window := range tab.Windows {
				if window.IsFocused {
					currentSession = window.SessionName
					active = true
					break
				}
			}
		}
		if !active {
			return 0, false
		}
		var selected int
		var lastFocused float64
		for _, tab := range osWindow.Tabs {
			for _, window := range tab.Windows {
				if window.SessionName != currentSession && window.LastFocusedAt > lastFocused {
					selected, lastFocused = window.ID, window.LastFocusedAt
				}
			}
		}
		return selected, selected != 0
	}
	return 0, false
}

func (c Client) State() (State, error) {
	content, err := c.List()
	if err != nil {
		return nil, err
	}
	return DecodeState(content)
}
