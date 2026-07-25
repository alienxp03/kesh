package kitty

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/alienxp03/kesh/internal/domain"
)

// ComposedSessionContent renders one Kitty tab per domain session entry.
func ComposedSessionContent(name, home string, entries []domain.SessionEntry) string {
	var content strings.Builder
	content.WriteString("os_window_title ")
	content.WriteString(name)
	content.WriteString("\nlayout splits\n")
	for _, entry := range entries {
		content.WriteString("new_tab ")
		content.WriteString(entry.Name)
		content.WriteByte('\n')
		if entry.SSHHost != "" {
			content.WriteString("cd ")
			content.WriteString(home)
			content.WriteString("\nlaunch --title ")
			content.WriteString(strconv.Quote("ssh: " + entry.SSHHost))
			content.WriteString(" ssh ")
			content.WriteString(strconv.Quote(entry.SSHHost))
			content.WriteByte('\n')
			continue
		}
		content.WriteString("cd ")
		content.WriteString(entry.Directory)
		content.WriteString("\nlaunch --title ")
		content.WriteString(strconv.Quote(entry.Name))
		content.WriteByte('\n')
	}
	content.WriteString("focus\nfocus_os_window\n")
	return content.String()
}

// SingleSessionContent renders the session file used by a pinned project or
// SSH host.
func SingleSessionContent(home string, entry domain.SessionEntry) string {
	if entry.SSHHost != "" {
		return fmt.Sprintf(
			"layout splits\ncd %s\nlaunch --title \"ssh: %s\" ssh \"%s\"\nfocus\nfocus_os_window\n",
			home, entry.SSHHost, entry.SSHHost,
		)
	}
	return fmt.Sprintf(
		"layout splits\ncd %s\nlaunch --title \"%s\"\nfocus\nfocus_os_window\n",
		entry.Directory, entry.Name,
	)
}
