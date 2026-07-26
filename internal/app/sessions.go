package app

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/alienxp03/kesh/internal/domain"
	kittyx "github.com/alienxp03/kesh/internal/kitty"
	tea "github.com/charmbracelet/bubbletea"
)

func runRename(kitty string, e entry, selected row, title string, names nameStore) tea.Cmd {
	target := renameTarget{entryKey: e.key}
	if selected.windowIndex >= 0 {
		target.tabID = e.tabs[selected.tabIndex].id
		target.windowID = e.tabs[selected.tabIndex].windows[selected.windowIndex].id
	} else if selected.tabIndex >= 0 {
		target.tabID = e.tabs[selected.tabIndex].id
	}
	return func() tea.Msg {
		var err error
		if selected.windowIndex >= 0 {
			window := e.tabs[selected.tabIndex].windows[selected.windowIndex]
			err = (kittyx.Client{Executable: kitty}).SetWindowTitle(window.id, title)
		} else if selected.tabIndex >= 0 {
			tab := e.tabs[selected.tabIndex]
			err = (kittyx.Client{Executable: kitty}).SetTabTitle(tab.id, title)
		} else {
			title = strings.TrimSpace(title)
			updated := make(nameStore, len(names)+1)
			for key, name := range names {
				updated[key] = name
			}
			// A single-project Kitty session is represented by a project entry,
			// but its alias must still be keyed by session so it survives the
			// catalog's project/session merge on the next refresh.
			nameKey := e.key
			if e.session != "" {
				nameKey = "workspace:" + e.session
			}
			if title == "" {
				delete(updated, nameKey)
			} else {
				updated[nameKey] = title
			}
			err = saveNames(updated)
			names = updated
		}
		return renameMsg{selected: selected, target: target, title: title, names: names, err: err}
	}
}

func clearSessionAlias(e entry, selected row) error {
	if e.saved || e.session == "" || selected.tabIndex >= 0 || selected.windowIndex >= 0 {
		return nil
	}
	names, err := loadNames()
	if err != nil {
		return err
	}
	delete(names, "workspace:"+e.session)
	return saveNames(names)
}

func closeArgs(e entry, selected row) ([]string, error) {
	if selected.windowIndex >= 0 {
		window := e.tabs[selected.tabIndex].windows[selected.windowIndex]
		return []string{"@", "close-window", "--match", "id:" + strconv.Itoa(window.id)}, nil
	}
	if selected.tabIndex >= 0 {
		tab := e.tabs[selected.tabIndex]
		return []string{"@", "close-tab", "--match", "id:" + strconv.Itoa(tab.id)}, nil
	}
	if len(e.tabs) == 0 {
		return nil, fmt.Errorf("%s is not open", e.name)
	}
	matches := make([]string, 0, len(e.tabs))
	for _, tab := range e.tabs {
		matches = append(matches, "id:"+strconv.Itoa(tab.id))
	}
	return []string{"@", "close-tab", "--match", strings.Join(matches, " or ")}, nil
}

func deleteSavedSession(e entry) error {
	if !e.saved || e.sessionFile == "" {
		return fmt.Errorf("workspace is not saved")
	}
	store, err := loadSavedSessions()
	if err != nil {
		return err
	}
	delete(store.Sessions, filepath.Clean(e.sessionFile))
	if err := saveSavedSessions(store); err != nil {
		return err
	}
	if err := os.Remove(e.sessionFile); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete saved session file: %w", err)
	}
	return nil
}

func runClose(kitty, zoxide string, e entry, selected row) tea.Cmd {
	return func() tea.Msg {
		deletedSavedKey := ""
		var err error
		if e.saved && !e.open && selected.tabIndex < 0 {
			err = deleteSavedSession(e)
			deletedSavedKey = e.key
		} else {
			var args []string
			args, err = closeArgs(e, selected)
			if err == nil {
				err = (kittyx.Client{Executable: kitty}).Run(args...)
			}
			if err == nil {
				err = clearSessionAlias(e, selected)
			}
		}
		if err != nil {
			return closeMsg{err: err}
		}
		entries, err := loadEntries(kitty, zoxide)
		return closeMsg{entries: entries, deletedSavedKey: deletedSavedKey, err: err}
	}
}

func composedSessionPath(name string) string {
	// The file only bootstraps the in-memory Kitty session, so keep it outside
	// persistent pin state and remove it once goto_session has loaded it.
	return filepath.Join(os.TempDir(), "kitty-kesh-sessions", "kesh-"+name+".kitty-session")
}

func composedSessionName(session string) (string, bool) {
	return domain.ComposedSessionName(session)
}

func composedSessionContent(name string, entries []entry) string {
	sessionEntries := make([]domain.SessionEntry, 0, len(entries))
	for _, entry := range entries {
		sessionEntry := domain.SessionEntry{Name: entry.name, Directory: entry.key}
		if entry.kind == "ssh" {
			sessionEntry.Directory = ""
			sessionEntry.SSHHost = strings.TrimPrefix(entry.key, "ssh://")
		}
		sessionEntries = append(sessionEntries, sessionEntry)
	}
	return kittyx.ComposedSessionContent(name, os.Getenv("HOME"), sessionEntries)
}

func savedSessionFilePath(sessionName string) string {
	// Preserve the Kitty session name in the filename so goto_session reports
	// the same identity after a restore and Kesh can merge it with the saved row.
	name := sessionName
	if name == "" || name == "." || name == ".." || filepath.Base(name) != name || strings.ContainsAny(name, "\r\n") {
		name = safeName(sessionName) + "-" + shortHash(sessionName)
	}
	return filepath.Join(savedSessionDirectory(), name+".kitty-session")
}

func workspaceProjects(e entry) []string {
	seen := map[string]bool{}
	var projects []string
	add := func(path string) {
		path, err := expandHomePath(path)
		if err != nil || !filepath.IsAbs(path) || seen[path] {
			return
		}
		seen[path] = true
		projects = append(projects, path)
	}
	add(e.path)
	for _, tab := range e.tabs {
		for _, window := range tab.windows {
			add(window.detail)
		}
	}
	return projects
}

func workspaceForegroundCommands(e entry) []string {
	shells := map[string]bool{"sh": true, "bash": true, "zsh": true, "fish": true, "nu": true}
	seen := map[string]bool{}
	var commands []string
	for _, tab := range e.tabs {
		for _, window := range tab.windows {
			command := strings.TrimSpace(window.fullCommand)
			name := strings.TrimPrefix(filepath.Base(window.command), "-")
			if command == "" || shells[name] || seen[command] {
				continue
			}
			seen[command] = true
			commands = append(commands, command)
		}
	}
	return commands
}

func runUnsave(kitty, zoxide string, e entry, entryIndex int) tea.Cmd {
	return func() tea.Msg {
		if err := deleteSavedSession(e); err != nil {
			return closeMsg{err: err}
		}
		entries, err := loadEntries(kitty, zoxide)
		return closeMsg{entries: entries, deletedSavedKey: e.key, err: err}
	}
}

func runSaveSession(kitty string, e entry, entryIndex int, name string, foregroundCommands bool) tea.Cmd {
	return func() tea.Msg {
		sessionName := e.session
		if sessionName == "" {
			return saveSessionMsg{entryIndex: entryIndex, entryKey: e.key, err: fmt.Errorf("workspace has no Kitty session name")}
		}
		file := e.sessionFile
		if file == "" {
			file = savedSessionFilePath(sessionName)
		}
		if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
			return saveSessionMsg{entryIndex: entryIndex, entryKey: e.key, err: fmt.Errorf("create saved session directory: %w", err)}
		}
		if err := (kittyx.Client{Executable: kitty}).SaveSession(regexp.QuoteMeta(sessionName), file, foregroundCommands); err != nil {
			return saveSessionMsg{entryIndex: entryIndex, entryKey: e.key, err: fmt.Errorf("save Kitty session: %w", err)}
		}
		if err := os.Chmod(file, 0o600); err != nil {
			return saveSessionMsg{entryIndex: entryIndex, entryKey: e.key, err: fmt.Errorf("secure saved session: %w", err)}
		}
		store, err := loadSavedSessions()
		if err != nil {
			return saveSessionMsg{entryIndex: entryIndex, entryKey: e.key, err: err}
		}
		for existingFile, record := range store.Sessions {
			if record.SessionName == sessionName && existingFile != file {
				delete(store.Sessions, existingFile)
			}
		}
		record := savedSessionRecord{
			Name: strings.TrimSpace(name), SessionName: sessionName, SessionFile: filepath.Clean(file),
			Projects: workspaceProjects(e), ForegroundCommands: foregroundCommands,
			SavedAt: time.Now().UTC().Format(time.RFC3339),
		}
		store.Sessions[record.SessionFile] = record
		if err := saveSavedSessions(store); err != nil {
			return saveSessionMsg{entryIndex: entryIndex, entryKey: e.key, err: err}
		}
		return saveSessionMsg{entryIndex: entryIndex, entryKey: e.key, record: record}
	}
}
