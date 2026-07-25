package domain

import "strings"

// SessionEntry is the platform-neutral input for a generated terminal session.
type SessionEntry struct {
	Name      string
	Directory string
	SSHHost   string
}

// ComposedSessionName extracts a user-facing name from Kesh's session prefix.
func ComposedSessionName(session string) (string, bool) {
	name := strings.TrimPrefix(session, "kesh-")
	return name, name != session && name != ""
}
