package domain

import (
	"regexp"
	"strings"
)

var composedSessionID = regexp.MustCompile(`^(.+)--[0-9a-f]{12}$`)

// SessionEntry is the platform-neutral input for a generated terminal session.
type SessionEntry struct {
	Name        string
	Directory   string
	SSHHost     string
	SessionName string
}

// ComposedSessionName extracts a user-facing name from Kesh's session prefix.
func ComposedSessionName(session string) (string, bool) {
	name := strings.TrimPrefix(session, "kesh-")
	if name == session || name == "" {
		return name, false
	}
	if match := composedSessionID.FindStringSubmatch(name); len(match) == 2 {
		name = match[1]
	}
	return name, true
}
