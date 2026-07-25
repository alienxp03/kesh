package kitty

import "testing"

func TestDecodeState(t *testing.T) {
	state, err := DecodeState([]byte(`[{"tabs":[{"id":2,"title":"code","windows":[{"id":3,"cwd":"/repo","foreground_processes":[{"cmdline":["nvim"],"cwd":"/repo"}]}]}]}]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(state) != 1 || len(state[0].Tabs) != 1 || state[0].Tabs[0].Windows[0].ForegroundProcesses[0].Cmdline[0] != "nvim" {
		t.Fatalf("decoded state = %#v", state)
	}
}
