package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/alienxp03/kesh/internal/config"
	"github.com/alienxp03/kesh/internal/domain"
	gitx "github.com/alienxp03/kesh/internal/git"
	kittyx "github.com/alienxp03/kesh/internal/kitty"
	"github.com/alienxp03/kesh/internal/state"
	"github.com/alienxp03/kesh/internal/system"
	tea "github.com/charmbracelet/bubbletea"
	"gopkg.in/yaml.v3"
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
	case "begin-run":
		return beginKittyRun(kitty, currentKittyPID())
	case "clear-pins":
		return clearAllPins(kitty, true)
	case "end-run":
		return endKittyRun(kitty)
	}
	if switchSlot != "" {
		return switchPin(kitty, zoxide, switchSlot)
	}

	fmt.Print("\033]2;kesh\007")
	entries, zoxideCtx, loadErr := loadEntriesFast(kitty)
	staleErr := clearStalePinsIfNeeded()
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
	checkoutRoot, checkoutRootErr := loadCheckoutRoot()
	worktreeRoot, worktreeRootErr := loadWorktreeRoot()
	for _, configErr := range []error{cloneRootErr, checkoutRootErr, worktreeRootErr} {
		if loadErr == nil && configErr != nil {
			loadErr = configErr
		}
	}
	m := model{
		entries: entries, err: loadErr, kitty: kitty, zoxide: zoxide, pins: pins, names: names,
		filter: filter, showPreview: true, selected: map[string]bool{},
		cloneBaseRoot: cloneRoot, checkoutBaseRoot: checkoutRoot, worktreeRoot: worktreeRoot,
		worktreeFilterEntryIndex: -1,
		zoxideCtx:                zoxideCtx, zoxidePending: zoxide != "",
	}
	m.rebuildRows()
	m.startupCmd = m.queuePreview()
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
	case len(args) == 1 && args[0] == "agents":
		return filterAgents, "", "", nil
	case len(args) == 1 && args[0] == "open":
		return filterOpen, "", "", nil
	case len(args) == 1 && args[0] == "projects":
		return filterProjects, "", "", nil
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
		return 0, "", "", &UsageError{message: "usage: kesh [agents | open | projects | ssh | saved | clear-pins | switch SLOT] (SLOT must be 0-9)"}
	}
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
func configDirectory() string       { return config.FromEnvironment().ConfigDirectory }
func configPath() string            { return config.FromEnvironment().File() }
func namesPath() string             { return config.FromEnvironment().Names() }

func loadCloneRoot() (string, error) {
	return config.CloneRoot(configPath(), os.Getenv("HOME"))
}

func loadWorktreeRoot() (string, error) {
	return config.WorktreeRoot(configPath(), os.Getenv("HOME"))
}

// loadCheckoutRoot returns the directory searched for an existing clone when
// checking out a pull request. It defaults to the clone root so the feature
// works with no configuration, and only falls back here when [checkout].root
// is unset — a configured value always wins.
func loadCheckoutRoot() (string, error) {
	return config.CheckoutRoot(configPath(), os.Getenv("HOME"))
}

// loadWktreeRecipe discovers the nearest recipe between path and its Git root.
func loadWktreeRecipe(path string) (*wktreeRecipe, string, error) {
	root, err := (gitx.Repository{Path: path}).Root()
	if err != nil {
		return nil, "", nil // Not a Git project: retain Kesh's native flow.
	}
	for dir := filepath.Clean(path); ; dir = filepath.Dir(dir) {
		candidate := filepath.Join(dir, ".wktree.yaml")
		content, readErr := os.ReadFile(candidate)
		if readErr == nil {
			var recipe wktreeRecipe
			if err := yaml.Unmarshal(content, &recipe); err != nil {
				return nil, candidate, fmt.Errorf("invalid .wktree.yaml: %w", err)
			}
			if recipe.WorkspaceMode == "" {
				recipe.WorkspaceMode = "single"
			}
			if recipe.WorkspaceMode != "single" && recipe.WorkspaceMode != "all" && recipe.WorkspaceMode != "selected" {
				return nil, candidate, fmt.Errorf("invalid .wktree.yaml workspace_mode %q", recipe.WorkspaceMode)
			}
			return &recipe, candidate, nil
		}
		if !os.IsNotExist(readErr) {
			return nil, candidate, readErr
		}
		if dir == root || dir == filepath.Dir(dir) {
			break
		}
	}
	return nil, "", nil
}

// ensureWorktreeSelection initializes (or resizes) the per-workspace toggle
// state used by "selected" mode. It defaults to the recipe's default_workspaces
// when configured, otherwise every workspace on. An existing selection of the
// right size is preserved, so tabbing away and back keeps the user's picks.
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
	defaults := make(map[string]bool, len(m.worktreeRecipe.DefaultWorkspaces))
	for _, name := range m.worktreeRecipe.DefaultWorkspaces {
		defaults[name] = true
	}
	for i, workspace := range m.worktreeRecipe.Workspaces {
		if len(defaults) > 0 {
			m.worktreeSelected[i] = defaults[workspace.Name]
		} else {
			m.worktreeSelected[i] = true
		}
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
	return state.LoadNames(namesPath())
}

func saveNames(names nameStore) error {
	return state.SaveNames(namesPath(), names)
}

func applyNames(entries []entry, names nameStore) {
	for index := range entries {
		if entries[index].originalName == "" {
			entries[index].originalName = entries[index].name
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

func savedSessionForName(store savedSessionStore, sessionName string) (savedSessionRecord, bool) {
	return state.SavedSessionForName(store, sessionName)
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

func currentKittyPID() int {
	if pid, err := strconv.Atoi(os.Getenv("KESH_KITTY_PID")); err == nil && pid > 0 {
		return pid
	}
	return os.Getppid()
}

// clearStalePinsIfNeeded resets pins left by a previous Kitty run when this is
// the first picker launch of a new run. Without the Kitty watcher, the picker
// is the only Kesh component that runs during a Kitty session, so it owns
// detecting that the previous Kitty (whether it quit normally or was
// force-killed) is gone. It only clears the persisted store; the picker's
// normal shortcut sync propagates the empty state to Kitty's keybindings.
func clearStalePinsIfNeeded() error {
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
	return savePins(pinStore{})
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
	if err := clearAllPins(kitty, true); err != nil {
		return fmt.Errorf("clear pins left by an unclean Kitty exit: %w", err)
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
	if err := clearAllPins(kitty, false); err != nil {
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
