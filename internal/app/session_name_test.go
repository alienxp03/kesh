package app

import "testing"

func TestDefaultSessionName(t *testing.T) {
	for _, tt := range []struct {
		name    string
		entries []entry
		want    string
	}{
		{"folder basename", []entry{{path: "/home/user/workspace/aurora", name: "Display name"}}, "aurora"},
		{"trailing slash", []entry{{path: "/workspace/aurora/"}}, "aurora"},
		{"SSH host", []entry{{kind: "ssh", name: "server"}}, "server"},
		{"multiple projects", []entry{{path: "/workspace/aurora"}, {path: "/workspace/api"}}, "aurora"},
		{"empty selection", nil, "session"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := defaultSessionName(tt.entries); got != tt.want {
				t.Fatalf("name = %q, want %q", got, tt.want)
			}
		})
	}
}
