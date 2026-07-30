package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/alienxp03/kesh/internal/agentstatus"
	"github.com/alienxp03/kesh/internal/config"
	"github.com/alienxp03/kesh/internal/domain"
	gitx "github.com/alienxp03/kesh/internal/git"
	kittyx "github.com/alienxp03/kesh/internal/kitty"
	"github.com/alienxp03/kesh/internal/state"
	"github.com/alienxp03/kesh/internal/system"
	wkc "github.com/alienxp03/kesh/internal/workspace/config"
)

// Run starts Kesh with the supplied command-line arguments. The command package
// owns process exit codes; the application returns errors so it remains testable.
func Run(args []string) error {
	kitty, zoxide := commands()
	filter, switchSlot, pinCommand, err := parseArgs(args)
	if err != nil {
		return err
	}
	switch pinCommand {
	case "init":
		return initProject()
	case "start":
		return runStart(kitty)
	case "begin-run":
		return beginKittyRun(kitty, currentKittyPID())
	case "clear-pins":
		return clearAllPins(kitty, true)
	case "end-run":
		return endKittyRun(kitty)
	case "agents-setup-pi", "agents-setup-codex", "agents-setup-claude":
		return setupAgentIntegration(strings.TrimPrefix(pinCommand, "agents-setup-"))
	case "agents-remove-pi", "agents-remove-codex", "agents-remove-claude":
		return removeAgentIntegration(strings.TrimPrefix(pinCommand, "agents-remove-"))
	case "agents-status":
		return printAgentIntegrationStatus()
	}
	if switchSlot != "" {
		return switchPin(kitty, zoxide, switchSlot)
	}

	fmt.Print("\033]2;kesh\007")
	entries, zoxideCtx, loadErr := loadEntriesFast(kitty)
	staleErr := clearStaleKittyRunStateIfNeeded()
	pins, pinErr := loadPins()
	names, nameErr := loadNames()
	if loadErr == nil && pinErr != nil {
		loadErr = pinErr
	}
	if loadErr == nil && staleErr != nil {
		loadErr = staleErr
	}
	if loadErr == nil && nameErr != nil {
		loadErr = nameErr
	}
	applyNames(entries, names)
	unmergeRenamedSessionSources(entries, names, &zoxideCtx)
	if loadErr == nil {
		var migrated bool
		pins, migrated = migrateLegacyPins(entries, pins)
		if migrated {
			loadErr = savePins(pins)
		}
	}
	if loadErr == nil {
		loadErr = syncPinShortcuts(kitty, pins)
	}
	applyPins(entries, pins)
	cloneRoot, cloneRootErr := loadCloneRoot()
	worktreeRoot, worktreeRootErr := loadWorktreeRoot()
	for _, configErr := range []error{cloneRootErr, worktreeRootErr} {
		if loadErr == nil && configErr != nil {
			loadErr = configErr
		}
	}
	agentStatusDir := config.FromEnvironment().AgentStatuses()
	m := model{
		entries: entries, err: loadErr, kitty: kitty, zoxide: zoxide, pins: pins, names: names,
		filter: filter, showPreview: true, selected: map[string]bool{}, agentStatusDir: agentStatusDir,
		cloneBaseRoot: cloneRoot, worktreeRoot: worktreeRoot,
		worktreeFilterEntryIndex: -1,
		zoxideCtx:                zoxideCtx, zoxidePending: zoxide != "",
	}
	if records, statusErr := agentstatus.ReadDirectory(agentStatusDir); statusErr == nil {
		statuses := make(map[int]agentLifecycleStatus, len(records))
		for windowID, record := range records {
			statuses[windowID] = agentLifecycleStatus{tool: record.Tool, status: record.Status}
		}
		m.applyAgentStatuses(statuses)
	}
	m.rebuildRows()
	m.startupCmd = tea.Batch(m.queuePreview(), queueAgentStatusRefresh(), m.queueAgentSpinner())
	_, err = tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

// UsageError reports invalid command-line input without coupling app to a process exit code.
type UsageError struct{ message string }

func (e *UsageError) Error() string { return e.message }

func parseArgs(args []string) (filter int, switchSlot, pinCommand string, err error) {
	switch {
	case len(args) == 0:
		return filterAll, "", "", nil
	case len(args) == 1 && args[0] == "init":
		return filterAll, "", "init", nil
	case len(args) == 1 && args[0] == "start":
		return filterAll, "", "start", nil
	case len(args) == 1 && args[0] == "agents":
		return filterAgents, "", "", nil
	case len(args) == 3 && args[0] == "agents" && args[1] == "setup" && validAgentIntegration(args[2]):
		return filterAll, "", "agents-setup-" + args[2], nil
	case len(args) == 3 && args[0] == "agents" && args[1] == "remove" && validAgentIntegration(args[2]):
		return filterAll, "", "agents-remove-" + args[2], nil
	case len(args) == 2 && args[0] == "agents" && args[1] == "status":
		return filterAll, "", "agents-status", nil
	case len(args) == 1 && args[0] == "ssh":
		return filterSSH, "", "", nil
	case len(args) == 1 && args[0] == "saved":
		return filterSaved, "", "", nil
	case len(args) == 1 && args[0] == "begin-run":
		return filterAll, "", "begin-run", nil
	case len(args) == 1 && args[0] == "clear-pins":
		return filterAll, "", "clear-pins", nil
	case len(args) == 2 && args[0] == "clear-pins" && args[1] == "--on-quit":
		return filterAll, "", "end-run", nil
	case len(args) == 2 && args[0] == "switch" && validSlot(args[1]):
		return filterAll, args[1], "", nil
	default:
		return 0, "", "", &UsageError{message: "usage: kesh [init | start | agents [setup TOOL | remove TOOL | status] | ssh | saved | clear-pins | switch SLOT] (TOOL must be pi, codex, or claude; SLOT must be 0-9)"}
	}
}

func validAgentIntegration(tool string) bool {
	return tool == "pi" || tool == "codex" || tool == "claude"
}

func setupAgentIntegration(tool string) error {
	var path string
	var err error
	if tool == "pi" {
		path, err = agentstatus.InstallPi()
	} else {
		path, err = agentstatus.InstallHooks(tool)
	}
	if err != nil {
		return err
	}
	fmt.Printf("installed %s agent status integration at %s\n", tool, path)
	switch tool {
	case "pi":
		fmt.Println("run /reload in existing Pi sessions, or restart Pi")
	case "codex":
		fmt.Println("restart Codex, then run /hooks to review and trust the Kesh hooks")
	case "claude":
		fmt.Println("restart Claude Code or run /hooks to verify the Kesh hooks")
	}
	return nil
}

func removeAgentIntegration(tool string) error {
	var path string
	var err error
	if tool == "pi" {
		path, err = agentstatus.RemovePi()
	} else {
		path, err = agentstatus.RemoveHooks(tool)
	}
	if err != nil {
		return err
	}
	if err := agentstatus.RemoveStatuses(config.FromEnvironment().AgentStatuses(), tool); err != nil {
		return err
	}
	fmt.Printf("removed %s agent status integration from %s\n", tool, path)
	return nil
}

func printAgentIntegrationStatus() error {
	for _, tool := range []string{"pi", "codex", "claude"} {
		var installed bool
		var path string
		var err error
		if tool == "pi" {
			installed, path, err = agentstatus.PiInstalled()
		} else {
			installed, path, err = agentstatus.HooksInstalled(tool)
		}
		if err != nil {
			return err
		}
		state := "not installed"
		if installed {
			state = "installed"
		}
		fmt.Printf("%s: %s (%s)\n", tool, state, path)
	}
	return nil
}

func initProject() error {
	workingDirectory, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("find current directory: %w", err)
	}
	path, err := wkc.WriteProjectTemplate(workingDirectory)
	if err != nil {
		return err
	}
	fmt.Printf("created %s\n", path)
	return nil
}

func commands() (string, string) {
	kitty := findCommand("kitty", "/Applications/kitty.app/Contents/MacOS/kitty")
	zoxide := findCommand("zoxide",
		filepath.Join(os.Getenv("HOME"), ".local", "bin", "zoxide"),
		filepath.Join(os.Getenv("HOME"), ".local", "share", "mise", "shims", "zoxide"),
		"/opt/homebrew/bin/zoxide",
	)
	return kitty, zoxide
}

func findCommand(name string, fallbacks ...string) string {
	if path, err := system.LookPath(name); err == nil {
		return path
	}
	for _, path := range fallbacks {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path
		}
	}
	return ""
}

func validSlot(slot string) bool {
	return len(slot) == 1 && slot[0] >= '0' && slot[0] <= '9'
}

func pinsPath() string              { return config.FromEnvironment().Pins() }
func savedSessionsPath() string     { return config.FromEnvironment().SavedSessions() }
func savedSessionDirectory() string { return config.FromEnvironment().Sessions() }
func pinShortcutsPath() string      { return config.FromEnvironment().PinShortcuts() }
func kittyRunPath() string          { return config.FromEnvironment().KittyRun() }
func configPath() string            { return config.FromEnvironment().File() }
func aliasesPath() string           { return config.FromEnvironment().Aliases() }

func loadCloneRoot() (string, error) {
	return config.CloneRoot(configPath(), os.Getenv("HOME"))
}

func loadWorktreeRoot() (string, error) {
	return config.WorktreeRoot(configPath(), os.Getenv("HOME"))
}

// loadRecipe discovers the nearest .kesh.yaml between path and its Git root
// and parses it with the workspace package's full schema. A missing recipe is
// not an error: it returns (nil, "", nil) so Kesh falls back to native mode.
func loadRecipe(path string) (*wkc.Config, string, error) {
	root, err := (gitx.Repository{Path: path}).Root()
	if err != nil {
		return nil, "", nil // Not a Git project: retain Kesh's native flow.
	}
	configPath, found, err := wkc.FindProjectPath(path, root)
	if err != nil {
		return nil, "", nil
	}
	if !found {
		return nil, "", nil
	}
	recipe, err := wkc.LoadProjectFile(configPath, os.Getenv("HOME"))
	if err != nil {
		return nil, configPath, fmt.Errorf("invalid %s: %w", wkc.ProjectFileName, err)
	}
	return &recipe, configPath, nil
}

// ensureWorktreeSelection initializes (or resizes) the per-workspace toggle
// state used by Workspaces mode. All configured workspaces are selected by
// default; an existing selection of the right size is preserved, so tabbing
// away and back keeps the user's picks.
func (m *model) ensureWorktreeSelection() {
	if m.worktreeRecipe == nil {
		m.worktreeSelected = nil
		m.worktreeWorkspaceCursor = 0
		return
	}
	n := len(m.worktreeRecipe.Workspaces)
	if n == 0 {
		m.worktreeSelected = nil
		m.worktreeWorkspaceCursor = 0
		return
	}
	if len(m.worktreeSelected) == n {
		return
	}
	m.worktreeSelected = make([]bool, n)
	for i := range m.worktreeSelected {
		m.worktreeSelected[i] = true
	}
	if m.worktreeWorkspaceCursor >= n {
		m.worktreeWorkspaceCursor = max(0, n-1)
	}
}

// selectedWorkspaceNames returns the names of the workspaces toggled on in
// "selected" mode, in recipe order.
func (m *model) selectedWorkspaceNames() []string {
	if m.worktreeRecipe == nil {
		return nil
	}
	var names []string
	for i, workspace := range m.worktreeRecipe.Workspaces {
		if i < len(m.worktreeSelected) && m.worktreeSelected[i] {
			names = append(names, workspace.Name)
		}
	}
	return names
}

func expandHomePath(path string) (string, error) {
	return config.ExpandHomePath(path, os.Getenv("HOME"))
}

func loadNames() (nameStore, error) {
	return state.LoadNames(aliasesPath())
}

func saveNames(names nameStore) error {
	return state.SaveNames(aliasesPath(), names)
}

func applyNames(entries []entry, names nameStore) {
	for index := range entries {
		if entries[index].originalName == "" {
			entries[index].originalName = entries[index].name
		}
		// A saved session has its own explicitly chosen display name. Do not
		// let an alias for the live Kitty session it came from overwrite it.
		if entries[index].saved {
			continue
		}
		entries[index].name = entries[index].originalName
		if entries[index].kind == "project" {
			if entries[index].session != "" {
				if alias := names["workspace:"+entries[index].session]; alias != "" {
					entries[index].name = alias
				}
			}
			continue
		}
		alias := names[entries[index].key]
		if alias == "" && entries[index].kind == "workspace" {
			// Before workspaces and projects had separate identities, workspace
			// aliases were stored under the project path.
			alias = names[entries[index].path]
		}
		if alias != "" {
			entries[index].name = alias
		}
	}
}

func loadPins() (pinStore, error) {
	return state.LoadPins(pinsPath(), savedSessionDirectory())
}

func loadSavedSessions() (savedSessionStore, error) {
	return state.LoadSavedSessions(savedSessionsPath(), savedSessionDirectory())
}

func saveSavedSessions(store savedSessionStore) error {
	return state.SaveSavedSessions(savedSessionsPath(), store)
}

func savePins(pins pinStore) error {
	return state.SavePins(pinsPath(), pins)
}

func pinShortcutsContent(pins pinStore) []byte {
	files := make(kittyx.PinSessionFiles, len(pins))
	for slot, target := range pins {
		files[slot] = target.SessionFile
	}
	return kittyx.PinShortcutsContent(files)
}

func savePinShortcuts(pins pinStore) (bool, error) {
	files := make(kittyx.PinSessionFiles, len(pins))
	for slot, target := range pins {
		files[slot] = target.SessionFile
	}
	return kittyx.SavePinShortcuts(pinShortcutsPath(), files)
}

func syncPinShortcuts(kitty string, pins pinStore) error {
	files := make(kittyx.PinSessionFiles, len(pins))
	for slot, target := range pins {
		files[slot] = target.SessionFile
	}
	return (kittyx.Client{Executable: kitty}).SyncPinShortcuts(pinShortcutsPath(), files)
}

func clearAllPins(kitty string, reloadConfig bool) error {
	pins := pinStore{}
	if err := savePins(pins); err != nil {
		return err
	}
	if !reloadConfig {
		_, err := savePinShortcuts(pins)
		return err
	}
	return syncPinShortcuts(kitty, pins)
}

func clearAliases() error {
	return saveNames(nameStore{})
}

func clearKittyRunState(kitty string, reloadConfig bool) error {
	if err := clearAllPins(kitty, reloadConfig); err != nil {
		return fmt.Errorf("clear pins: %w", err)
	}
	if err := clearAliases(); err != nil {
		return fmt.Errorf("clear aliases: %w", err)
	}
	return nil
}

func currentKittyPID() int {
	if pid, err := strconv.Atoi(os.Getenv("KESH_KITTY_PID")); err == nil && pid > 0 {
		return pid
	}
	return os.Getppid()
}

// clearStaleKittyRunStateIfNeeded resets state left by a previous Kitty run
// when this is the first picker launch of a new run. Without the Kitty
// watcher, the picker is the only Kesh component that runs during a Kitty
// session, so it owns detecting that the previous Kitty (whether it quit
// normally or was force-killed) is gone. It writes the empty shortcut file;
// the picker's normal shortcut sync propagates the empty state to Kitty.
func clearStaleKittyRunStateIfNeeded() error {
	marker := kittyRunPath()
	if content, err := os.ReadFile(marker); err == nil {
		previousPID, parseErr := strconv.Atoi(strings.TrimSpace(string(content)))
		if parseErr == nil && previousPID > 0 && kittyProcessRunning(previousPID) {
			return nil
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read Kitty run marker: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(marker), 0o700); err != nil {
		return fmt.Errorf("create Kesh state directory: %w", err)
	}
	if err := os.WriteFile(marker, []byte(strconv.Itoa(currentKittyPID())+"\n"), 0o600); err != nil {
		return fmt.Errorf("save Kitty run marker: %w", err)
	}
	return clearKittyRunState("", false)
}

func beginKittyRun(kitty string, pid int) error {
	marker := kittyRunPath()
	content, err := os.ReadFile(marker)
	if err == nil {
		previousPID, parseErr := strconv.Atoi(strings.TrimSpace(string(content)))
		if parseErr == nil && previousPID > 0 && kittyProcessRunning(previousPID) {
			return nil
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read Kitty run marker: %w", err)
	}
	if err := clearKittyRunState(kitty, true); err != nil {
		return fmt.Errorf("clear Kitty run state left by an unclean Kitty exit: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(marker), 0o700); err != nil {
		return fmt.Errorf("create Kesh state directory: %w", err)
	}
	if err := os.WriteFile(marker, []byte(strconv.Itoa(pid)+"\n"), 0o600); err != nil {
		return fmt.Errorf("save Kitty run marker: %w", err)
	}
	return nil
}

func kittyProcessRunning(pid int) bool {
	return system.ProcessRunning(pid)
}

func endKittyRun(kitty string) error {
	if err := clearKittyRunState(kitty, false); err != nil {
		return err
	}
	if err := os.Remove(kittyRunPath()); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("clear Kitty run marker: %w", err)
	}
	return nil
}

func pinTargetForEntry(e entry) (pinTarget, error) {
	if e.sessionFile != "" {
		return pinTarget{Key: e.key, Name: e.name, Kind: e.kind, SessionFile: e.sessionFile, Version: currentPinVersion}, nil
	}
	directory := filepath.Join(filepath.Dir(pinsPath()), "sessions")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return pinTarget{}, fmt.Errorf("create pinned session directory: %w", err)
	}
	name := e.session
	if name == "" {
		if e.kind == "ssh" {
			name = "ssh-" + safeName(strings.TrimPrefix(e.key, "ssh://"))
		} else {
			name = safeName(filepath.Base(e.key))
			if e.nameTaken {
				name += "-" + shortHash(e.key)
			}
		}
	}
	path := filepath.Join(directory, safeName(name)+".kitty-session")
	sessionEntry := domain.SessionEntry{Name: filepath.Base(e.key), Directory: e.key}
	if e.kind == "ssh" {
		host := strings.TrimPrefix(e.key, "ssh://")
		sessionEntry = domain.SessionEntry{Name: e.name, SSHHost: host}
	} else {
		directory := e.key
		if e.path != "" {
			directory = e.path
		}
		sessionEntry.Name = filepath.Base(directory)
		sessionEntry.Directory = directory
	}
	content := kittyx.SingleSessionContent(os.Getenv("HOME"), sessionEntry)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return pinTarget{}, fmt.Errorf("write pinned session: %w", err)
	}
	return pinTarget{Key: e.key, Name: e.name, Kind: e.kind, SessionFile: path, Version: currentPinVersion}, nil
}

func migrateLegacyPins(entries []entry, pins pinStore) (pinStore, bool) {
	updated := make(pinStore, len(pins))
	changed := false
	for slot, target := range pins {
		if target.Version == 0 {
			target.Version = currentPinVersion
			changed = true
			if target.Kind == "project" {
				var fallback *entry
				for index := range entries {
					candidate := &entries[index]
					if candidate.kind != "workspace" || candidate.path != target.Key {
						continue
					}
					if fallback == nil {
						fallback = candidate
					}
					if candidate.name == target.Name {
						fallback = candidate
						break
					}
				}
				if fallback != nil {
					target.Key = fallback.key
					target.Name = fallback.name
					target.Kind = "workspace"
				}
			}
		}

		matched := false
		for _, candidate := range entries {
			if candidate.key == target.Key {
				matched = true
				break
			}
		}
		if !matched && target.Kind == "workspace" {
			sessionName := strings.TrimPrefix(target.Key, "workspace:")
			for _, candidate := range entries {
				if candidate.kind != "project" || candidate.session != sessionName {
					continue
				}
				target.Key = candidate.key
				target.Name = candidate.name
				target.Kind = "project"
				changed = true
				break
			}
		}
		updated[slot] = target
	}
	return updated, changed
}

func applyPins(entries []entry, pins pinStore) {
	for index := range entries {
		entries[index].pin = ""
	}
	for slot, target := range pins {
		for index := range entries {
			if entries[index].key == target.Key {
				entries[index].pin = slot
				break
			}
		}
	}
}

func switchPin(kitty, zoxide, slot string) error {
	pins, err := loadPins()
	if err != nil {
		return err
	}
	target, ok := pins[slot]
	if !ok {
		return fmt.Errorf("no session is pinned to slot %s", slot)
	}
	if target.Kind == "project" {
		if info, err := os.Stat(target.Key); err != nil || !info.IsDir() {
			return fmt.Errorf("pinned project is unavailable: %s", target.Key)
		}
	}
	if target.SessionFile != "" {
		if info, err := os.Stat(target.SessionFile); err != nil || info.IsDir() {
			return fmt.Errorf("pinned session file is unavailable: %s", target.SessionFile)
		}
		return (kittyx.Client{Executable: kitty}).GotoSession(target.SessionFile)
	}

	// Older pin entries are migrated on their first use.
	os.Unsetenv("KITTY_WINDOW_ID")
	entries, err := loadEntries(kitty, zoxide)
	if err != nil {
		return err
	}
	for _, candidate := range entries {
		if candidate.key != target.Key {
			continue
		}
		if candidate.kind == "project" && !candidate.open {
			if info, err := os.Stat(candidate.key); err != nil || !info.IsDir() {
				return fmt.Errorf("pinned project is unavailable: %s", candidate.key)
			}
		}
		migrated, err := pinTargetForEntry(candidate)
		if err != nil {
			return err
		}
		pins[slot] = migrated
		if err := savePins(pins); err != nil {
			return err
		}
		if err := syncPinShortcuts(kitty, pins); err != nil {
			return err
		}
		return (kittyx.Client{Executable: kitty}).GotoSession(migrated.SessionFile)
	}
	return fmt.Errorf("pinned session is no longer available: %s", target.Name)
}

func (m model) Init() tea.Cmd {
	if m.zoxidePending {
		return tea.Batch(m.startupCmd, fetchZoxideEntries(m.zoxide, m.zoxideCtx))
	}
	return m.startupCmd
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		return m.updateKey(key)
	}
	return m.updateMessage(msg)
}
