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
		statuses := make(map[int]agentLifecycleStatus, len(records))
		for windowID, record := range records {
			statuses[windowID] = agentLifecycleStatus{tool: record.Tool, status: record.Status}
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

func (m *model) applyAgentStatuses(statuses map[int]agentLifecycleStatus) {
	for entryIndex := range m.entries {
		for tabIndex := range m.entries[entryIndex].tabs {
			for windowIndex := range m.entries[entryIndex].tabs[tabIndex].windows {
				window := &m.entries[entryIndex].tabs[tabIndex].windows[windowIndex]
				window.agentStatus = ""
				status := statuses[window.id]
				if agentStatusTool(window.agent) == status.tool {
					window.agentStatus = status.status
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

func agentStatusTool(agent string) string {
	agent = strings.ToLower(agent)
	for _, tool := range []string{"pi", "codex", "claude"} {
		if strings.Contains(agent, tool) {
			return tool
		}
	}
	return ""
}

func acknowledgeThen(directory, tool string, windowID int, action tea.Cmd) tea.Cmd {
	if action == nil {
		return nil
	}
	return func() tea.Msg {
		_ = agentstatus.Acknowledge(directory, tool, windowID)
		return action()
	}
}
