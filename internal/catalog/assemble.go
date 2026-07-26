package catalog

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/alienxp03/kesh/internal/domain"
	"github.com/alienxp03/kesh/internal/kitty"
	"github.com/alienxp03/kesh/internal/state"
)

var unsafeSessionName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// Assemble builds the fast first-paint catalog from live Kitty state, saved
// sessions, and SSH configuration. Zoxide projects are merged separately.
func Assemble(
	kittyState kitty.State,
	saved state.SavedSessions,
	sshHosts []SSHHost,
	selfID int,
	home string,
) ([]domain.Entry, domain.CatalogContext) {
	type openSession struct {
		path        string
		displayName string
		focused     float64
		tabs        []domain.Tab
	}
	sessions := map[string]*openSession{}
	sessionNames := map[string]bool{}
	livePaths := map[string]bool{}
	unscopedTabs := map[string][]domain.Tab{}
	unscopedFocus := map[string]float64{}
	openSSH := map[string]float64{}

	for _, osWindow := range kittyState {
		for _, tab := range osWindow.Tabs {
			if IsKeshTab(tab.Windows) {
				continue
			}
			sessionName := ""
			layoutSessionName := ""
			canonicalPath := ""
			var windows []domain.Window
			var paths []string
			focused := float64(0)
			for _, window := range tab.Windows {
				if window.ID == selfID {
					continue
				}
				path := WindowPath(window)
				if path != "" {
					paths = append(paths, path)
				}
				if canonicalPath == "" {
					canonicalPath = path
				}
				if sessionName == "" && window.SessionName != "" {
					sessionName = window.SessionName
					canonicalPath = path
				}
				if layoutSessionName == "" {
					layoutSessionName = strings.TrimSpace(window.Env["KESH_KITTY_SESSION"])
				}
				focused = max(focused, window.LastFocusedAt)
				windows = append(windows, WindowFromKitty(window, home))
			}
			if len(windows) > 0 {
				agent := MergedWindowAgents(windows)
				title := CleanAgentTitle(tab.Title, agent)
				if title == "" {
					title = "tab " + strconv.Itoa(tab.ID)
				}
				item := domain.Tab{
					ID: tab.ID, Title: title,
					Detail: fmt.Sprintf("%d window%s", len(windows), plural(len(windows))),
					Agent:  agent, Windows: windows,
				}
				if sessionName == "" && canonicalPath != "" {
					unscopedTabs[canonicalPath] = append(unscopedTabs[canonicalPath], item)
					unscopedFocus[canonicalPath] = max(unscopedFocus[canonicalPath], focused)
				} else if sessionName != "" {
					// Only windows that belong to a named session contribute live
					// paths. Unscoped windows are not sessions and must not surface
					// as catalog entries; their open state is merged onto the
					// matching zoxide project via OpenTabs instead.
					for _, p := range paths {
						livePaths[p] = true
					}
					sessionNames[sessionName] = true
					session := sessions[sessionName]
					if session == nil {
						session = &openSession{path: canonicalPath}
						sessions[sessionName] = session
					}
					// Kesh-created layouts carry their human session name in the
					// environment. Keep it for presentation instead of replacing it
					// with the source folder basename below.
					if layoutSessionName != "" {
						session.displayName = layoutSessionName
					}
					session.focused = max(session.focused, focused)
					session.tabs = append(session.tabs, item)
				}
			}
			for _, window := range tab.Windows {
				if window.ID == selfID {
					continue
				}
				if host := SSHHostFromWindow(window); host != "" {
					openSSH[host] = max(openSSH[host], window.LastFocusedAt)
				}
			}
		}
	}

	var entries []domain.Entry
	order := 0
	mergedProjects := map[string]bool{}
	namedWorkspaces := make([]string, 0, len(sessions))
	for name := range sessions {
		if !strings.HasPrefix(name, "ssh-") {
			namedWorkspaces = append(namedWorkspaces, name)
		}
	}
	sort.Strings(namedWorkspaces)
	seenSavedSessions := map[string]bool{}
	for _, sessionName := range namedWorkspaces {
		session := sessions[sessionName]
		name := sessionName
		if session.displayName != "" {
			name = session.displayName
		} else if session.path != "" {
			name = filepath.Base(session.path)
		}
		_, composed := domain.ComposedSessionName(sessionName)
		if composedName, ok := domain.ComposedSessionName(sessionName); ok {
			name = composedName
		}
		record, savedSession := state.SavedSessionForName(saved, sessionName)
		sessionFile := ""
		if savedSession {
			name = record.Name
			sessionFile = record.SessionFile
			seenSavedSessions[record.SessionFile] = true
			composed = composed || len(record.Projects) > 1
		}
		detail := DisplayPath(session.path, home)
		if session.path == "" {
			detail = fmt.Sprintf("%d tab%s", len(session.tabs), plural(len(session.tabs)))
		}
		kind := "workspace"
		key := "workspace:" + sessionName
		if !composed && session.path != "" && !mergedProjects[session.path] {
			kind = "project"
			key = session.path
			mergedProjects[session.path] = true
		}
		entries = append(entries, domain.Entry{
			Key: key, Name: name, OriginalName: name, Detail: detail,
			Kind: kind, Path: session.path, Session: sessionName, SessionFile: sessionFile, Saved: savedSession,
			Open: true, LastFocused: session.focused,
			Agent: MergedTabAgents(session.tabs), Tabs: session.tabs, Order: order,
		})
		order++
	}

	// Unscoped tabs (windows with no session_name) are not sessions and do not
	// become entries here. They are carried as OpenTabs so the asynchronous
	// zoxide merge can attach their live state to a matching known project.

	savedFiles := make([]string, 0, len(saved.Sessions))
	for file := range saved.Sessions {
		savedFiles = append(savedFiles, file)
	}
	sort.Strings(savedFiles)
	for _, file := range savedFiles {
		if seenSavedSessions[file] {
			continue
		}
		record := saved.Sessions[file]
		path := ""
		if len(record.Projects) > 0 {
			path = record.Projects[0]
		}
		detail := "saved session"
		if path != "" {
			detail = DisplayPath(path, home)
		}
		_, composed := domain.ComposedSessionName(record.SessionName)
		composed = composed || len(record.Projects) > 1 || path == ""
		kind := "workspace"
		key := "workspace:" + record.SessionName
		if !composed && !mergedProjects[path] {
			kind = "project"
			key = path
			mergedProjects[path] = true
		}
		entries = append(entries, domain.Entry{
			Key: key, Name: record.Name, OriginalName: record.Name, Detail: detail,
			Kind: kind, Path: path, Session: record.SessionName, SessionFile: record.SessionFile,
			Saved: true, Order: order,
		})
		sessionNames[record.SessionName] = true
		order++
	}

	for _, host := range sshHosts {
		var tabs []domain.Tab
		sessionName := ""
		if _, ok := openSSH[host.Name]; ok {
			sessionName = "ssh-" + SafeName(host.Name)
			if session := sessions[sessionName]; session != nil {
				tabs = session.tabs
			}
		}
		entries = append(entries, domain.Entry{
			Key: "ssh://" + host.Name, Name: host.Name, OriginalName: host.Name, Detail: host.Target, Kind: "ssh",
			Session: sessionName, Open: sessionName != "", LastFocused: openSSH[host.Name],
			Agent: MergedTabAgents(tabs), Tabs: tabs, Order: order,
		})
		order++
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return domain.EntryLess(
			domain.EntryOrder{
				Open: entries[i].Open, LastFocused: entries[i].LastFocused, Saved: entries[i].Saved,
				Kind: entries[i].Kind, Order: entries[i].Order,
			},
			domain.EntryOrder{
				Open: entries[j].Open, LastFocused: entries[j].LastFocused, Saved: entries[j].Saved,
				Kind: entries[j].Kind, Order: entries[j].Order,
			},
		)
	})
	openTabs := make(map[string]domain.OpenTabState, len(unscopedTabs))
	for path, tabs := range unscopedTabs {
		openTabs[path] = domain.OpenTabState{Tabs: tabs, LastFocused: unscopedFocus[path]}
	}
	return entries, domain.CatalogContext{
		LivePaths: livePaths, MergedPaths: mergedProjects, SessionNames: sessionNames, Home: home,
		OpenTabs: openTabs,
	}
}

// MergeZoxide converts a zoxide query into source-project catalog entries.
func MergeZoxide(output []byte, context domain.CatalogContext) []domain.Entry {
	paths := strings.FieldsFunc(string(output), func(character rune) bool {
		return character == '\n' || character == '\r'
	})
	known := map[string]bool{}
	for _, path := range paths {
		known[path] = true
	}
	for path := range context.LivePaths {
		if !known[path] {
			paths = append(paths, path)
		}
	}
	var entries []domain.Entry
	order := 0
	for _, path := range paths {
		if path == "" || path == "/" || context.MergedPaths[path] {
			continue
		}
		name := filepath.Base(path)
		entry := domain.Entry{
			Key: path, Name: name, OriginalName: name, Detail: DisplayPath(path, context.Home),
			Kind: "project", Path: path, NameTaken: context.SessionNames[SafeName(name)], Order: order,
		}
		// A zoxide project that is open in an unscoped Kitty window inherits
		// that window's live state — without it, the window itself is not shown.
		if open, ok := context.OpenTabs[path]; ok {
			entry.Open = true
			entry.LastFocused = open.LastFocused
			entry.Tabs = open.Tabs
			entry.Agent = MergedTabAgents(open.Tabs)
		}
		entries = append(entries, entry)
		order++
	}
	return entries
}

func IsKeshTab(windows []kitty.Window) bool {
	if len(windows) == 0 {
		return false
	}
	for _, window := range windows {
		commands := append([][]string{window.Cmdline}, foregroundCmdlines(window)...)
		found := false
		for _, command := range commands {
			if len(command) > 0 && filepath.Base(command[0]) == "kesh" {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func WindowPath(window kitty.Window) string {
	if path := window.Env["PWD"]; path != "" {
		return path
	}
	return window.CWD
}

func WindowFromKitty(window kitty.Window, home string) domain.Window {
	command := ""
	fullCommand := ""
	detail := WindowPath(window)
	if len(window.ForegroundProcesses) > 0 {
		process := window.ForegroundProcesses[len(window.ForegroundProcesses)-1]
		if len(process.Cmdline) > 0 {
			command = filepath.Base(process.Cmdline[0])
			fullCommand = strings.TrimSpace(strings.Join(process.Cmdline, " "))
		}
		if process.CWD != "" {
			detail = process.CWD
		}
	}
	agent := AgentFromWindow(window)
	title := CleanAgentTitle(window.Title, agent)
	if title == "" {
		title = command
	}
	if title == "" {
		title = "window " + strconv.Itoa(window.ID)
	}
	return domain.Window{
		ID: window.ID, Title: title, Detail: DisplayPath(detail, home), Command: command,
		FullCommand: fullCommand, Agent: agent, LastFocused: window.LastFocusedAt, CWD: detail,
	}
}

func CleanAgentTitle(title, agent string) string {
	if strings.Contains(agent, "pi") {
		if _, cleanTitle, found := strings.Cut(title, "π - "); found {
			title = cleanTitle
		}
	}
	if strings.Contains(agent, "codex") {
		if _, cleanTitle, found := strings.Cut(title, "󰚩 - "); found {
			title = cleanTitle
		}
	}
	return title
}

func AgentFromWindow(window kitty.Window) string {
	pi, codex, claude := false, false, false
	for _, process := range window.ForegroundProcesses {
		command := " " + strings.ToLower(strings.Join(process.Cmdline, " ")) + " "
		pi = pi || strings.Contains(command, " pi ") || strings.Contains(command, "/pi ")
		codex = codex || strings.Contains(command, " codex ") || strings.Contains(command, "/codex ")
		claude = claude || strings.Contains(command, " claude ") || strings.Contains(command, "/claude ") || strings.Contains(command, "/claude.exe ")
	}
	return mergedAgentFlags(pi, codex, claude)
}

func MergedWindowAgents(windows []domain.Window) string {
	pi, codex, claude := false, false, false
	for _, window := range windows {
		pi = pi || strings.Contains(window.Agent, "pi")
		codex = codex || strings.Contains(window.Agent, "codex")
		claude = claude || strings.Contains(window.Agent, "claude")
	}
	return mergedAgentFlags(pi, codex, claude)
}

func MergedTabAgents(tabs []domain.Tab) string {
	pi, codex, claude := false, false, false
	for _, tab := range tabs {
		pi = pi || strings.Contains(tab.Agent, "pi")
		codex = codex || strings.Contains(tab.Agent, "codex")
		claude = claude || strings.Contains(tab.Agent, "claude")
	}
	return mergedAgentFlags(pi, codex, claude)
}

func SSHHostFromWindow(window kitty.Window) string {
	for _, process := range window.ForegroundProcesses {
		if len(process.Cmdline) > 1 && filepath.Base(process.Cmdline[0]) == "ssh" {
			return process.Cmdline[1]
		}
	}
	return ""
}

func DisplayPath(path, home string) string {
	if path == home {
		return "~"
	}
	if strings.HasPrefix(path, home+string(filepath.Separator)) {
		return "~" + strings.TrimPrefix(path, home)
	}
	return path
}

func SafeName(value string) string { return unsafeSessionName.ReplaceAllString(value, "_") }

func foregroundCmdlines(window kitty.Window) [][]string {
	commands := make([][]string, 0, len(window.ForegroundProcesses))
	for _, process := range window.ForegroundProcesses {
		commands = append(commands, process.Cmdline)
	}
	return commands
}

func mergedAgentFlags(pi, codex, claude bool) string {
	agents := make([]string, 0, 3)
	if pi {
		agents = append(agents, "pi")
	}
	if codex {
		agents = append(agents, "codex")
	}
	if claude {
		agents = append(agents, "claude")
	}
	return strings.Join(agents, ",")
}

func plural(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}
