package kitty

import "testing"

func TestPreviousSessionWindow(t *testing.T) {
	state := State{
		{Tabs: []Tab{{IsActive: true, Windows: []Window{{ID: 99, SessionName: "elsewhere", IsFocused: true, LastFocusedAt: 100}}}}},
		{IsFocused: true, Tabs: []Tab{
			{IsActive: true, Windows: []Window{{ID: 1, SessionName: "current", IsFocused: true, LastFocusedAt: 90}}},
			{Windows: []Window{{ID: 2, SessionName: "current", LastFocusedAt: 85}}},
			{Windows: []Window{{ID: 3, SessionName: "previous", LastFocusedAt: 80}}},
			{Windows: []Window{{ID: 4, SessionName: "previous", LastFocusedAt: 88}}},
			{Windows: []Window{{ID: 5, SessionName: "older", LastFocusedAt: 70}}},
		}},
	}
	if got, ok := state.PreviousSessionWindow(); !ok || got != 4 {
		t.Fatalf("previous session window = %d, %t; want 4, true", got, ok)
	}
	state[1].Tabs[3].IsActive = true
	state[1].Tabs[3].Windows[0].IsFocused = true
	state[1].Tabs[0].IsActive = false
	state[1].Tabs[0].Windows[0].IsFocused = false
	state[1].Tabs[0].Windows[0].LastFocusedAt = 91
	if got, ok := state.PreviousSessionWindow(); !ok || got != 1 {
		t.Fatalf("return session window = %d, %t; want 1, true", got, ok)
	}
}

func TestPreviousSessionWindowWithoutOtherSession(t *testing.T) {
	state := State{{IsFocused: true, Tabs: []Tab{{IsActive: true, Windows: []Window{{ID: 1, SessionName: "same", IsFocused: true, LastFocusedAt: 3}}}, {Windows: []Window{{ID: 2, SessionName: "same", LastFocusedAt: 2}}}}}}
	if got, ok := state.PreviousSessionWindow(); ok || got != 0 {
		t.Fatalf("previous session window = %d, %t; want 0, false", got, ok)
	}
}

func TestDecodeState(t *testing.T) {
	state, err := DecodeState([]byte(`[{"tabs":[{"id":2,"title":"code","windows":[{"id":3,"cwd":"/repo","foreground_processes":[{"cmdline":["nvim"],"cwd":"/repo"}]}]}]}]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(state) != 1 || len(state[0].Tabs) != 1 || state[0].Tabs[0].Windows[0].ForegroundProcesses[0].Cmdline[0] != "nvim" {
		t.Fatalf("decoded state = %#v", state)
	}
}
