package app

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/alienxp03/kesh/internal/agentstatus"
)

func fetchAgentStatuses(directory string) tea.Cmd {
	return func() tea.Msg {
		records, err := agentstatus.ReadDirectory(directory)
		statuses := make(map[int]string, len(records))
		for windowID, record := range records {
			statuses[windowID] = record.Status
		}
		return agentStatusMsg{statuses: statuses, err: err}
	}
}

func queueAgentStatusRefresh() tea.Cmd {
	return tea.Tick(agentStatusRefreshInterval, func(time.Time) tea.Msg {
		return agentStatusTickMsg{}
	})
}

func (m *model) queueAgentSpinner() tea.Cmd {
	if m.agentSpinnerPending || !m.hasWorkingAgent() {
		return nil
	}
	m.agentSpinnerPending = true
	return tea.Tick(agentSpinnerInterval, func(time.Time) tea.Msg {
		return agentSpinnerTickMsg{}
	})
}

func (m model) hasWorkingAgent() bool {
	for _, entry := range m.entries {
		for _, tab := range entry.tabs {
			for _, window := range tab.windows {
				if window.agentStatus == "working" {
					return true
				}
			}
		}
	}
	return false
}

func (m *model) applyAgentStatuses(statuses map[int]string) {
	for entryIndex := range m.entries {
		for tabIndex := range m.entries[entryIndex].tabs {
			for windowIndex := range m.entries[entryIndex].tabs[tabIndex].windows {
				window := &m.entries[entryIndex].tabs[tabIndex].windows[windowIndex]
				window.agentStatus = ""
				if strings.Contains(window.agent, "pi") {
					window.agentStatus = statuses[window.id]
				}
			}
		}
	}
}

func (m *model) acknowledgeAgentStatus(windowID int) {
	for entryIndex := range m.entries {
		for tabIndex := range m.entries[entryIndex].tabs {
			for windowIndex := range m.entries[entryIndex].tabs[tabIndex].windows {
				window := &m.entries[entryIndex].tabs[tabIndex].windows[windowIndex]
				if window.id == windowID && (window.agentStatus == "finished" || window.agentStatus == "errored") {
					window.agentStatus = "idle"
				}
			}
		}
	}
}

func acknowledgeThen(directory string, windowID int, action tea.Cmd) tea.Cmd {
	if action == nil {
		return nil
	}
	return func() tea.Msg {
		_ = agentstatus.Acknowledge(directory, windowID)
		return action()
	}
}
