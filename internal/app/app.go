package app

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/alienxp03/dotfiles/apps/kesh/internal/config"
	"github.com/alienxp03/dotfiles/apps/kesh/internal/system"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/sahilm/fuzzy"
	"gopkg.in/yaml.v3"
)

// exec is a temporary compatibility shim while command-oriented integrations
// are extracted from app. It keeps all process execution behind system.Runner.
var exec = struct {
	Command  func(string, ...string) *system.Process
	LookPath func(string) (string, error)
}{system.Command, system.LookPath}

type kittyState []struct {
	Tabs []struct {
		ID       int           `json:"id"`
		Title    string        `json:"title"`
		IsActive bool          `json:"is_active"`
		Windows  []kittyWindow `json:"windows"`
	} `json:"tabs"`
}

type kittyWindow struct {
	ID                  int               `json:"id"`
	Cmdline             []string          `json:"cmdline"`
	Title               string            `json:"title"`
	CWD                 string            `json:"cwd"`
	SessionName         string            `json:"session_name"`
	LastFocusedAt       float64           `json:"last_focused_at"`
	Env                 map[string]string `json:"env"`
	ForegroundProcesses []struct {
		Cmdline []string `json:"cmdline"`
		CWD     string   `json:"cwd"`
	} `json:"foreground_processes"`
}

type windowItem struct {
	id               int
	title            string
	detail           string
	cwd              string
	command          string
	fullCommand      string
	agent            string
	lastFocused      float64
	worktrees        []worktreeItem
	worktreesLoaded  bool
	worktreesOpen    bool
	worktreesPending bool
	pathPR           pathPRInfo
}

type tabItem struct {
	id       int
	title    string
	detail   string
	agent    string
	expanded bool
	windows  []windowItem
}

type worktreeItem struct {
	path      string
	branch    string
	head      string
	current   bool
	isDefault bool
	prStatus  string
	prURL     string
	prNumber  int
	prExact   bool
	prRepoKey string
	dirty     bool
	behind    int
	ahead     int
	changes   []string
}

type worktreeFilterRow struct {
	worktree  worktreeItem
	entryName string
	entryPath string
}

type entry struct {
	key              string
	name             string
	originalName     string
	detail           string
	kind             string
	path             string
	session          string
	sessionFile      string
	saved            bool
	open             bool
	lastFocused      float64
	nameTaken        bool
	agent            string
	expanded         bool
	tabs             []tabItem
	order            int
	pin              string
	pathPR           pathPRInfo
	worktrees        []worktreeItem
	worktreesLoaded  bool
	worktreesOpen    bool
	worktreesPending bool
}

type row struct {
	entryIndex   int
	tabIndex     int
	windowIndex  int
	section      string // "", "wt-head", "wt-item", "wt-filter"
	wt           int    // index into the entry or window worktree list for "wt-item" rows
	worktreePath string // for worktree filter mode
}

type actionMsg struct{ err error }
type openPRMsg struct{ err error }

type closeMsg struct {
	entries         []entry
	deletedSavedKey string
	err             error
}

type previewMsg struct {
	windowID int
	content  string
	err      error
}

type previewRefreshMsg struct{ windowID int }

type commandResult struct {
	output []byte
	err    error
}

type pinTarget struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Kind        string `json:"kind,omitempty"`
	SessionFile string `json:"session_file,omitempty"`
	Version     int    `json:"version,omitempty"`
}

const (
	currentPinVersion          = 2
	currentSavedSessionVersion = 1
	previewRefreshInterval     = time.Second
)

type pinStore map[string]pinTarget

type savedSessionRecord struct {
	Name               string   `json:"name"`
	SessionName        string   `json:"session_name"`
	SessionFile        string   `json:"session_file"`
	Projects           []string `json:"projects,omitempty"`
	ForegroundCommands bool     `json:"foreground_commands,omitempty"`
	SavedAt            string   `json:"saved_at"`
}

type savedSessionStore struct {
	Version  int                           `json:"version"`
	Sessions map[string]savedSessionRecord `json:"sessions"`
}

type nameStore map[string]string

type renameMsg struct {
	selected row
	title    string
	names    nameStore
	err      error
}

type createMsg struct{ err error }

type cloneMsg struct{ err error }

type prCheckoutMsg struct{ err error }

type prPreviewMsg struct {
	value  string
	branch string
}

type saveSessionMsg struct {
	entryIndex int
	record     savedSessionRecord
	err        error
}

type worktreeMsg struct {
	err error
}

type worktreeListMsg struct {
	entryIndex  int
	tabIndex    int
	windowIndex int
	dir         string
	worktrees   []worktreeItem
	err         error
}

type worktreeRemoveMsg struct {
	entryIndex  int
	tabIndex    int
	windowIndex int
	forceTried  bool
	err         error
}

type worktreeFetchedMsg struct {
	dir        string
	entryIndex int
	err        error
}

type worktreePullMsg struct {
	dir        string
	entryIndex int
	err        error
}

type mergedWorktreeListMsg struct {
	selected  row
	dir       string
	worktrees []worktreeItem
	err       error
}

type mergedWorktreeRemoveMsg struct {
	selected row
	dir      string
	err      error
}

type bulkWorktreeRemoveMsg struct {
	entryIndex int
	dir        string
	err        error
}

type prInfo struct {
	Status string
	URL    string
	Number int
}

type pathPRInfo struct {
	Branch      string
	Head        string
	RepoKey     string
	PullRequest prInfo
	Exact       bool
}

type prStatusMsg struct {
	repoKey      string
	pullRequests map[string]prInfo
	err          error
}

type pathPRMsg struct {
	path string
	info pathPRInfo
}

type prStatusCacheEntry struct {
	Branch string `json:"branch"`
	Head   string `json:"head"`
	Status string `json:"status"`
	URL    string `json:"url,omitempty"`
	Number int    `json:"number,omitempty"`
}

type prStatusRepositoryCache struct {
	FetchedAt string               `json:"fetched_at"`
	Entries   []prStatusCacheEntry `json:"entries"`
}

type prStatusCacheStore struct {
	Version      int                                `json:"version"`
	Repositories map[string]prStatusRepositoryCache `json:"repositories"`
}

type wktreeRecipe struct {
	WorkspaceMode     string   `yaml:"workspace_mode"`
	DefaultWorkspaces []string `yaml:"default_workspaces"`
	Terminal          struct {
		SessionName string `yaml:"session_name"`
	} `yaml:"terminal"`
	Workspaces []struct {
		Name  string `yaml:"name"`
		Repo  string `yaml:"repo"`
		Panes []struct {
			Command    string   `yaml:"command"`
			Commands   []string `yaml:"commands"`
			Split      string   `yaml:"split"`
			Focus      bool     `yaml:"focus"`
			Percentage int      `yaml:"percentage"`
		} `yaml:"panes"`
	} `yaml:"workspaces"`
}

type keshConfig struct {
	Clone struct {
		Root string `toml:"root"`
	} `toml:"clone"`
	Worktree struct {
		Root string `toml:"root"`
	} `toml:"worktree"`
	Checkout struct {
		Root string `toml:"root"`
	} `toml:"checkout"`
}

type model struct {
	entries                  []entry
	rows                     []row
	cursor                   int
	query                    string
	mode                     modeKind
	renameValue              string
	createValue              string
	cloneBusy                bool
	prCheckoutBusy           bool
	prCheckoutValue          string
	prCheckoutBranch         string
	checkoutRoot             string
	saving                   bool
	saveForeground           bool
	saveEntry                int
	cloneDestinationFocus    bool
	cloneDestinationEdited   bool
	cloneRepository          string
	cloneDestination         string
	cloneRoot                string
	selected                 map[string]bool
	wtBulkSelected           map[string]bool // Worktree-tab bulk selection, keyed by worktree path
	bulkWorktrees            []worktreeItem  // selected worktrees targeted by a bulk remove
	pinEntry                 int
	confirmSlot              string
	closeBusy                bool
	closeRow                 row
	destroyPlan              *destroyPlan
	pins                     pinStore
	names                    nameStore
	filter                   int
	previousFilter           int
	worktreeFilterEntryIndex int
	worktreeFilterRows       []worktreeFilterRow
	zoxideCtx                zoxideMergeContext
	zoxidePending            bool
	width                    int
	height                   int
	err                      error
	kitty                    string
	zoxide                   string
	preview                  string
	previewErr               error
	previewID                int
	previewBusy              bool
	showPreview              bool
	worktreeBranch           string
	worktreePaths            []string
	worktreeBusy             bool
	worktreeRoot             string
	worktreeRecipe           *wktreeRecipe
	worktreeRecipePath       string
	worktreeRecipeMode       string
	worktreeCustomWorkspaces bool
	worktreeSelected         []bool
	worktreeWorkspaceCursor  int
	worktreeForcePrompt      bool
	mergedWorktrees          []worktreeItem
	mergedWorktreeBusy       bool
	worktreePullBusy         bool
	prStatusPending          map[string]bool
	prStatusLastFetch        map[string]time.Time
	pathPRChecked            map[string]bool
	startupCmd               tea.Cmd
}

// modeKind makes modal combinations unrepresentable. Background busy state
// remains on the feature that owns it (for example cloneBusy).
type modeKind uint8

const (
	modeNormal modeKind = iota
	modeSearch
	modeRename
	modeCreateSession
	modeClone
	modeCheckoutPR
	modePin
	modeSaveConfirm
	modeCloseConfirm
	modeWorktreeCreate
)

const (
	filterAll = iota
	filterAgents
	filterOpen
	filterProjects
	filterSSH
	filterSaved
	filterWorktrees
)

const (
	prStatusCacheVersion = 2
	prStatusThrottle     = time.Minute
)

var (
	accentStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true)
	dimStyle          = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	mutedStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("250"))
	openStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	selectedStyle     = lipgloss.NewStyle().Background(lipgloss.Color("236")).Foreground(lipgloss.Color("230")).Bold(true)
	selectedTextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("230")).Bold(true)
	focusStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("255")).Bold(true)
	projectStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("75"))
	prOpenStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true)
	prMergedStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("141")).Bold(true)
	prClosedStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
	sshStyle          = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	piStyle           = lipgloss.NewStyle().Foreground(lipgloss.Color("81")).Bold(true)
	codexStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true)
	errorStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	ansiPattern       = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)
	backgroundSGR     = regexp.MustCompile(`\x1b\[(48(:[0-9]*)+|48(;[0-9]*)+|49)m`)
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
	pins, pinErr := loadPins()
	names, nameErr := loadNames()
	if loadErr == nil && pinErr != nil {
		loadErr = pinErr
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
	worktreeRoot, _ := loadWorktreeRoot()
	m := model{
		entries: entries, err: loadErr, kitty: kitty, zoxide: zoxide, pins: pins, names: names,
		filter: filter, showPreview: true, selected: map[string]bool{},
		worktreeRoot: worktreeRoot, worktreeFilterEntryIndex: -1,
		zoxideCtx: zoxideCtx, zoxidePending: zoxide != "",
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
	case len(args) == 1 && args[0] == "begin-run":
		return filterAll, "", "begin-run", nil
	case len(args) == 1 && args[0] == "clear-pins":
		return filterAll, "", "clear-pins", nil
	case len(args) == 2 && args[0] == "clear-pins" && args[1] == "--on-quit":
		return filterAll, "", "end-run", nil
	case len(args) == 2 && args[0] == "switch" && validSlot(args[1]):
		return filterAll, args[1], "", nil
	default:
		return 0, "", "", &UsageError{message: "usage: kesh [agents | clear-pins | switch SLOT] (SLOT must be 0-9)"}
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
	if path, err := exec.LookPath(name); err == nil {
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
	home := os.Getenv("HOME")
	root := filepath.Join(home, "workspace")
	content, err := os.ReadFile(configPath())
	if err != nil {
		if os.IsNotExist(err) {
			return root, nil
		}
		return "", fmt.Errorf("read Kesh config: %w", err)
	}
	var config keshConfig
	if _, err := toml.Decode(string(content), &config); err != nil {
		return "", fmt.Errorf("invalid Kesh config: %w", err)
	}
	if configured := strings.TrimSpace(config.Clone.Root); configured != "" {
		root, err = expandHomePath(configured)
		if err != nil {
			return "", fmt.Errorf("invalid clone root: %w", err)
		}
	}
	if !filepath.IsAbs(root) {
		return "", fmt.Errorf("clone root must be an absolute or home-relative path")
	}
	return filepath.Clean(root), nil
}

func loadWorktreeRoot() (string, error) {
	home := os.Getenv("HOME")
	root := filepath.Join(home, "worktree")
	content, err := os.ReadFile(configPath())
	if err != nil {
		if os.IsNotExist(err) {
			return root, nil
		}
		return "", fmt.Errorf("read Kesh config: %w", err)
	}
	var config keshConfig
	if _, err := toml.Decode(string(content), &config); err != nil {
		return "", fmt.Errorf("invalid Kesh config: %w", err)
	}
	if configured := strings.TrimSpace(config.Worktree.Root); configured != "" {
		root, err = expandHomePath(configured)
		if err != nil {
			return "", fmt.Errorf("invalid worktree root: %w", err)
		}
	}
	if !filepath.IsAbs(root) {
		return "", fmt.Errorf("worktree root must be an absolute or home-relative path")
	}
	return filepath.Clean(root), nil
}

// loadCheckoutRoot returns the directory searched for an existing clone when
// checking out a pull request. It defaults to the clone root so the feature
// works with no configuration, and only falls back here when [checkout].root
// is unset — a configured value always wins.
func loadCheckoutRoot() (string, error) {
	root, err := loadCloneRoot()
	if err != nil {
		return "", err
	}
	content, err := os.ReadFile(configPath())
	if err != nil {
		if os.IsNotExist(err) {
			return root, nil
		}
		return "", fmt.Errorf("read Kesh config: %w", err)
	}
	var config keshConfig
	if _, err := toml.Decode(string(content), &config); err != nil {
		return "", fmt.Errorf("invalid Kesh config: %w", err)
	}
	if configured := strings.TrimSpace(config.Checkout.Root); configured != "" {
		root, err = expandHomePath(configured)
		if err != nil {
			return "", fmt.Errorf("invalid checkout root: %w", err)
		}
	}
	if !filepath.IsAbs(root) {
		return "", fmt.Errorf("checkout root must be an absolute or home-relative path")
	}
	return filepath.Clean(root), nil
}

// loadWktreeRecipe discovers the nearest recipe between path and its Git root.
func loadWktreeRecipe(path string) (*wktreeRecipe, string, error) {
	rootOutput, err := exec.Command("git", "-C", path, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return nil, "", nil // Not a Git project: retain Kesh's native flow.
	}
	root := strings.TrimSpace(string(rootOutput))
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
	path = strings.TrimSpace(path)
	if strings.ContainsAny(path, "\r\n") {
		return "", fmt.Errorf("path cannot contain a line break")
	}
	switch {
	case path == "":
		return "", fmt.Errorf("path is required")
	case path == "~":
		return os.Getenv("HOME"), nil
	case strings.HasPrefix(path, "~/"):
		return filepath.Join(os.Getenv("HOME"), strings.TrimPrefix(path, "~/")), nil
	case strings.HasPrefix(path, "~"):
		return "", fmt.Errorf("user-specific home paths are not supported: %s", path)
	default:
		return filepath.Clean(path), nil
	}
}

func loadNames() (nameStore, error) {
	names := nameStore{}
	content, err := os.ReadFile(namesPath())
	if os.IsNotExist(err) {
		return names, nil
	}
	if err != nil {
		return names, fmt.Errorf("read workspace names: %w", err)
	}
	if err := json.Unmarshal(content, &names); err != nil {
		return nameStore{}, fmt.Errorf("invalid workspace names: %w", err)
	}
	for key, name := range names {
		if key == "" || strings.TrimSpace(name) == "" {
			return nameStore{}, fmt.Errorf("invalid workspace name for %q", key)
		}
	}
	return names, nil
}

func saveNames(names nameStore) error {
	path := namesPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create workspace name directory: %w", err)
	}
	content, err := json.MarshalIndent(names, "", "  ")
	if err != nil {
		return fmt.Errorf("encode workspace names: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".names-*.json")
	if err != nil {
		return fmt.Errorf("create workspace name state: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(append(content, '\n')); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryName, path); err != nil {
		return fmt.Errorf("save workspace names: %w", err)
	}
	return nil
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
	pins := pinStore{}
	content, err := os.ReadFile(pinsPath())
	if os.IsNotExist(err) {
		return pins, nil
	}
	if err != nil {
		return pins, fmt.Errorf("read pins: %w", err)
	}
	if err := json.Unmarshal(content, &pins); err != nil {
		return pinStore{}, fmt.Errorf("invalid pin state: %w", err)
	}
	seenTargets := map[string]string{}
	sessionsDirectory := filepath.Clean(filepath.Join(filepath.Dir(pinsPath()), "sessions")) + string(os.PathSeparator)
	for slot, target := range pins {
		if !validSlot(slot) || target.Key == "" {
			return pinStore{}, fmt.Errorf("invalid pin entry for slot %q", slot)
		}
		if target.Kind != "" && target.Kind != "workspace" && target.Kind != "project" && target.Kind != "ssh" {
			return pinStore{}, fmt.Errorf("invalid pin kind for slot %s", slot)
		}
		if target.Version != 0 && target.Version != currentPinVersion {
			return pinStore{}, fmt.Errorf("unsupported pin version for slot %s", slot)
		}
		if target.SessionFile != "" && !strings.HasPrefix(filepath.Clean(target.SessionFile), sessionsDirectory) {
			return pinStore{}, fmt.Errorf("invalid session file for slot %s", slot)
		}
		if previous, exists := seenTargets[target.Key]; exists {
			return pinStore{}, fmt.Errorf("session is pinned more than once: slots %s and %s", previous, slot)
		}
		seenTargets[target.Key] = slot
	}
	return pins, nil
}

func loadSavedSessions() (savedSessionStore, error) {
	store := savedSessionStore{Version: currentSavedSessionVersion, Sessions: map[string]savedSessionRecord{}}
	content, err := os.ReadFile(savedSessionsPath())
	if os.IsNotExist(err) {
		return store, nil
	}
	if err != nil {
		return store, fmt.Errorf("read saved sessions: %w", err)
	}
	if err := json.Unmarshal(content, &store); err != nil {
		return savedSessionStore{}, fmt.Errorf("invalid saved session state: %w", err)
	}
	if store.Version != currentSavedSessionVersion || store.Sessions == nil {
		return savedSessionStore{}, fmt.Errorf("unsupported saved session state version: %d", store.Version)
	}
	directory := filepath.Clean(savedSessionDirectory()) + string(os.PathSeparator)
	seenNames := map[string]bool{}
	for key, record := range store.Sessions {
		file := filepath.Clean(record.SessionFile)
		if key != file || !strings.HasPrefix(file, directory) {
			return savedSessionStore{}, fmt.Errorf("invalid saved session file: %s", record.SessionFile)
		}
		if record.Name == "" || record.SessionName == "" || seenNames[record.SessionName] {
			return savedSessionStore{}, fmt.Errorf("invalid saved session metadata for %s", file)
		}
		seenNames[record.SessionName] = true
		for _, project := range record.Projects {
			if !filepath.IsAbs(project) {
				return savedSessionStore{}, fmt.Errorf("invalid saved session project: %s", project)
			}
		}
	}
	return store, nil
}

func saveSavedSessions(store savedSessionStore) error {
	path := savedSessionsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create saved session directory: %w", err)
	}
	store.Version = currentSavedSessionVersion
	if store.Sessions == nil {
		store.Sessions = map[string]savedSessionRecord{}
	}
	content, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return fmt.Errorf("encode saved sessions: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".saved-sessions-*.json")
	if err != nil {
		return fmt.Errorf("create saved session state: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(append(content, '\n')); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryName, path); err != nil {
		return fmt.Errorf("save saved sessions: %w", err)
	}
	return nil
}

func savedSessionForName(store savedSessionStore, sessionName string) (savedSessionRecord, bool) {
	for _, record := range store.Sessions {
		if record.SessionName == sessionName {
			return record, true
		}
	}
	return savedSessionRecord{}, false
}

func savePins(pins pinStore) error {
	path := pinsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create pin directory: %w", err)
	}
	content, err := json.MarshalIndent(pins, "", "  ")
	if err != nil {
		return fmt.Errorf("encode pins: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".pins-*.json")
	if err != nil {
		return fmt.Errorf("create pin state: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(append(content, '\n')); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryName, path); err != nil {
		return fmt.Errorf("save pins: %w", err)
	}
	return nil
}

func pinShortcutsContent(pins pinStore) []byte {
	var content strings.Builder
	content.WriteString("# Generated by kesh. Changes will be overwritten.\n")
	for slot := 0; slot <= 9; slot++ {
		key := strconv.Itoa(slot)
		target, pinned := pins[key]
		if !pinned || target.SessionFile == "" {
			fmt.Fprintf(&content, "map cmd+%s\n", key)
			continue
		}
		fmt.Fprintf(&content, "map cmd+%s goto_session %s\n", key, strconv.Quote(target.SessionFile))
	}
	return []byte(content.String())
}

func savePinShortcuts(pins pinStore) (bool, error) {
	path := pinShortcutsPath()
	content := pinShortcutsContent(pins)
	if current, err := os.ReadFile(path); err == nil && string(current) == string(content) {
		return false, nil
	} else if err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("read Kitty pin shortcuts: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return false, fmt.Errorf("create Kitty shortcut directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".kitty-pins-*.conf")
	if err != nil {
		return false, fmt.Errorf("create Kitty pin shortcuts: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return false, err
	}
	if _, err := temporary.Write(content); err != nil {
		temporary.Close()
		return false, err
	}
	if err := temporary.Close(); err != nil {
		return false, err
	}
	if err := os.Rename(temporaryName, path); err != nil {
		return false, fmt.Errorf("save Kitty pin shortcuts: %w", err)
	}
	return true, nil
}

func syncPinShortcuts(kitty string, pins pinStore) error {
	changed, err := savePinShortcuts(pins)
	if err != nil || !changed {
		return err
	}
	return run(kitty, "@", "load-config")
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
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
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
	var content string
	if e.kind == "ssh" {
		host := strings.TrimPrefix(e.key, "ssh://")
		content = fmt.Sprintf("layout splits\ncd %s\nlaunch --title \"ssh: %s\" ssh \"%s\"\nfocus\nfocus_os_window\n", os.Getenv("HOME"), host, host)
	} else {
		path := e.key
		if e.path != "" {
			path = e.path
		}
		content = fmt.Sprintf("layout splits\ncd %s\nlaunch --title \"%s\"\nfocus\nfocus_os_window\n", path, filepath.Base(path))
	}
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
		return run(kitty, "@", "action", "goto_session", target.SessionFile)
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
		return run(kitty, "@", "action", "goto_session", migrated.SessionFile)
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
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case zoxideEntriesMsg:
		// Zoxide source projects arrive after first paint so the slow query
		// never blocks the UI. Merge, re-apply aliases/pins, and refresh.
		m.zoxidePending = false
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.err = nil
		if len(msg.entries) > 0 {
			m.entries = append(m.entries, msg.entries...)
			sortEntries(m.entries)
			applyNames(m.entries, m.names)
			applyPins(m.entries, m.pins)
			m.rebuildRows()
		}
		return m, nil
	case actionMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		return m, tea.Quit
	case openPRMsg:
		m.err = msg.err
		return m, nil
	case closeMsg:
		m.closeBusy = false
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		if msg.deletedSavedKey != "" {
			updatedPins := make(pinStore, len(m.pins))
			for slot, target := range m.pins {
				if target.Key != msg.deletedSavedKey {
					updatedPins[slot] = target
				}
			}
			if len(updatedPins) != len(m.pins) {
				if err := savePins(updatedPins); err != nil {
					m.err = err
					return m, nil
				}
				if err := syncPinShortcuts(m.kitty, updatedPins); err != nil {
					m.err = err
					return m, nil
				}
				m.pins = updatedPins
			}
		}
		preserveExpandedState(m.entries, msg.entries)
		m.entries = msg.entries
		applyNames(m.entries, m.names)
		applyPins(m.entries, m.pins)
		m.mode = modeNormal
		m.err = nil
		m.previewID = 0
		m.preview = ""
		m.previewErr = nil
		m.previewBusy = false
		m.rebuildRows()
		return m, m.queuePreview()
	case destroyMsg:
		m.closeBusy = false
		m.destroyPlan = nil
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		// runDestroy reloads every entry, so the Worktree tab's entry index may
		// have shifted and its worktrees are no longer loaded. Capture the
		// project path before the swap, re-resolve it by path, and refetch its
		// worktree list so the tab reflects the removal.
		var tabPath string
		if m.filter == filterWorktrees && m.worktreeFilterEntryIndex >= 0 && m.worktreeFilterEntryIndex < len(m.entries) {
			tabPath = m.entries[m.worktreeFilterEntryIndex].path
		}
		preserveExpandedState(m.entries, msg.entries)
		m.entries = msg.entries
		applyNames(m.entries, m.names)
		applyPins(m.entries, m.pins)
		m.mode = modeNormal
		m.err = nil
		m.previewID = 0
		m.preview = ""
		m.previewErr = nil
		m.previewBusy = false
		if m.filter == filterWorktrees && tabPath != "" {
			for i := range m.entries {
				if m.entries[i].path == tabPath {
					m.worktreeFilterEntryIndex = i
					return m, fetchWorktrees(tabPath, i, -1, -1)
				}
			}
			// The project itself was destroyed; leave the tab.
			m.filter = m.previousFilter
			if m.filter == filterWorktrees {
				m.filter = filterAll
			}
			m.worktreeFilterEntryIndex = -1
			m.worktreeFilterRows = nil
		}
		m.rebuildRows()
		return m, m.queuePreview()
	case previewMsg:
		if msg.windowID != m.previewID {
			return m, nil
		}
		m.previewBusy = false
		m.preview = msg.content
		m.previewErr = msg.err
		return m, m.queuePreviewRefresh(msg.windowID)
	case previewRefreshMsg:
		if !m.shouldRefreshPreview(msg.windowID) {
			return m, nil
		}
		return m, fetchPreview(m.kitty, msg.windowID)
	case renameMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		entry := &m.entries[msg.selected.entryIndex]
		if msg.selected.windowIndex >= 0 {
			entry.tabs[msg.selected.tabIndex].windows[msg.selected.windowIndex].title = msg.title
		} else if msg.selected.tabIndex >= 0 {
			entry.tabs[msg.selected.tabIndex].title = msg.title
		} else {
			m.names = msg.names
			entry.name = msg.title
			if entry.name == "" {
				entry.name = entry.originalName
			}
			m.rebuildRows()
		}
		m.mode = modeNormal
		m.renameValue = ""
		m.err = nil
	case createMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		return m, tea.Quit
	case cloneMsg:
		m.cloneBusy = false
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		return m, tea.Quit
	case prCheckoutMsg:
		m.prCheckoutBusy = false
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		return m, tea.Quit
	case prPreviewMsg:
		// A lookup may finish after the input changed; never show its stale path.
		if m.mode == modeCheckoutPR && msg.value == m.prCheckoutValue {
			m.prCheckoutBranch = msg.branch
		}
		return m, nil
	case worktreeMsg:
		if m.worktreeBusy {
			// Creation completed.
			m.worktreeBusy = false
			m.mode = modeNormal
			if msg.err != nil {
				m.err = msg.err
				return m, nil
			}
			if m.filter == filterWorktrees && m.worktreeFilterEntryIndex >= 0 && m.worktreeFilterEntryIndex < len(m.entries) {
				entryIndex := m.worktreeFilterEntryIndex
				return m, fetchWorktrees(m.entries[entryIndex].path, entryIndex, -1, -1)
			}
			return m, tea.Quit
		}
		// Validation runs only on Enter. On success, proceed to create;
		// otherwise surface the error without leaving the form.
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.worktreeBusy = true
		return m, m.createWorktree()
	case worktreeListMsg:
		updated := false
		if w := m.windowAt(msg.entryIndex, msg.tabIndex, msg.windowIndex); w != nil && w.cwd == msg.dir {
			w.worktreesPending = false
			if msg.err != nil {
				w.worktreesLoaded = false
				m.err = msg.err
				return m, nil
			}
			w.worktrees = msg.worktrees
			w.worktreesLoaded = true
			w.worktreesOpen = true
			updated = true
		} else if e := m.closedEntryAt(msg.entryIndex, msg.tabIndex, msg.windowIndex); e != nil && e.path == msg.dir {
			e.worktreesPending = false
			if msg.err != nil {
				e.worktreesLoaded = false
				m.err = msg.err
				return m, nil
			}
			e.worktrees = msg.worktrees
			e.worktreesLoaded = true
			e.worktreesOpen = true
			updated = true
		}
		// Handle worktree filter mode
		if m.filter == filterWorktrees && msg.entryIndex == m.worktreeFilterEntryIndex && msg.err == nil {
			if msg.entryIndex >= 0 && msg.entryIndex < len(m.entries) {
				e := &m.entries[msg.entryIndex]
				e.worktrees = msg.worktrees
				e.worktreesLoaded = true
				e.worktreesPending = false
				m.rebuildWorktreeRows()
				return m, m.refreshPRStatuses(msg.dir, false)
			}
		}
		if updated {
			m.err = nil
			m.rebuildRows()
			return m, m.refreshPRStatuses(msg.dir, false)
		}
		return m, nil
	case worktreeFetchedMsg:
		// Reload even if fetch failed so local worktree state remains useful, but
		// make the failed remote refresh visible to the user.
		if msg.err != nil {
			m.err = msg.err
		}
		cmds := []tea.Cmd{fetchWorktrees(msg.dir, msg.entryIndex, -1, -1)}
		if msg.entryIndex >= 0 && msg.entryIndex < len(m.entries) {
			cmds = append(cmds, m.refreshPRStatuses(m.entries[msg.entryIndex].path, true))
		}
		return m, tea.Batch(cmds...)
	case worktreePullMsg:
		m.worktreePullBusy = false
		if msg.err != nil {
			// Bulk pull may partially succeed, so surface the error but still
			// reload — the worktrees that did pull have new ahead/behind state.
			m.err = msg.err
			return m, fetchWorktrees(msg.dir, msg.entryIndex, -1, -1)
		}
		m.err = nil
		return m, fetchWorktrees(msg.dir, msg.entryIndex, -1, -1)
	case pathPRMsg:
		if m.pathPRChecked == nil {
			m.pathPRChecked = map[string]bool{}
		}
		m.pathPRChecked[msg.path] = true
		for index := range m.entries {
			if m.entries[index].path == msg.path {
				m.entries[index].pathPR = msg.info
			}
			for tabIndex := range m.entries[index].tabs {
				for windowIndex := range m.entries[index].tabs[tabIndex].windows {
					window := &m.entries[index].tabs[tabIndex].windows[windowIndex]
					if window.cwd == msg.path {
						window.pathPR = msg.info
					}
				}
			}
		}
		if msg.info.RepoKey != "" {
			return m, m.refreshPRStatuses(msg.path, false)
		}
		return m, nil
	case prStatusMsg:
		if m.prStatusPending != nil {
			m.prStatusPending[msg.repoKey] = false
		}
		if msg.err != nil {
			if strings.Contains(msg.repoKey, "github.com") {
				m.err = fmt.Errorf("refresh PR status: %w", msg.err)
			}
			return m, nil
		}
		if m.prStatusLastFetch == nil {
			m.prStatusLastFetch = map[string]time.Time{}
		}
		m.prStatusLastFetch[msg.repoKey] = time.Now()
		focusedWorktree := m.focusedWorktreePath()
		m.applyPRStatuses(msg.repoKey, msg.pullRequests)
		m.rebuildRows()
		m.restoreFocusedWorktree(focusedWorktree)
		return m, nil
	case mergedWorktreeListMsg:
		m.mergedWorktreeBusy = false
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		if len(msg.worktrees) == 0 {
			m.err = fmt.Errorf("no merged worktrees to remove")
			return m, nil
		}
		m.closeRow = msg.selected
		m.mergedWorktrees = msg.worktrees
		m.mode = modeCloseConfirm
		m.closeBusy = false
		m.worktreeForcePrompt = false
		m.err = nil
		return m, nil
	case mergedWorktreeRemoveMsg:
		m.closeBusy = false
		m.mode = modeNormal
		m.mergedWorktrees = nil
		m.worktreeForcePrompt = false
		if msg.err != nil {
			m.invalidateWorktrees(msg.selected)
			m.err = msg.err
			m.rebuildRows()
			return m, nil
		}
		m.err = nil
		return m, fetchWorktrees(msg.dir, msg.selected.entryIndex, msg.selected.tabIndex, msg.selected.windowIndex)
	case bulkWorktreeRemoveMsg:
		m.closeBusy = false
		m.mode = modeNormal
		m.bulkWorktrees = nil
		m.worktreeForcePrompt = false
		m.wtBulkSelected = nil
		if msg.err != nil {
			m.err = msg.err
		} else {
			m.err = nil
		}
		if msg.entryIndex >= 0 && msg.entryIndex < len(m.entries) {
			return m, fetchWorktrees(msg.dir, msg.entryIndex, -1, -1)
		}
		m.rebuildRows()
		return m, nil
	case worktreeRemoveMsg:
		m.closeBusy = false
		if msg.err != nil {
			if !msg.forceTried {
				// Normal remove failed (dirty, locked, etc.) — offer force.
				m.worktreeForcePrompt = true
				m.err = msg.err
				return m, nil
			}
			// Force failed too: surface the error and leave the form.
			m.mode = modeNormal
			m.worktreeForcePrompt = false
			m.err = msg.err
			return m, nil
		}
		// Removed: refresh that window's worktree list and close the popup.
		m.mode = modeNormal
		m.worktreeForcePrompt = false
		m.err = nil
		// In the Worktree tab the removal targets the tab's project; windowAt
		// and closedEntryAt do not resolve it (open entries, tabIndex -1), so
		// refetch the tab's worktree list directly.
		if m.filter == filterWorktrees && msg.entryIndex >= 0 && msg.entryIndex < len(m.entries) && msg.entryIndex == m.worktreeFilterEntryIndex {
			return m, fetchWorktrees(m.entries[msg.entryIndex].path, msg.entryIndex, -1, -1)
		}
		if w := m.windowAt(msg.entryIndex, msg.tabIndex, msg.windowIndex); w != nil {
			return m, fetchWorktrees(w.cwd, msg.entryIndex, msg.tabIndex, msg.windowIndex)
		}
		if e := m.closedEntryAt(msg.entryIndex, msg.tabIndex, msg.windowIndex); e != nil {
			return m, fetchWorktrees(e.path, msg.entryIndex, -1, -1)
		}
		return m, nil
	case saveSessionMsg:
		m.saving = false
		m.mode = modeNormal
		m.saveForeground = false
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		entry := &m.entries[msg.entryIndex]
		entry.saved = true
		entry.sessionFile = msg.record.SessionFile
		pinsChanged := false
		for slot, target := range m.pins {
			if target.Key == entry.key && target.SessionFile != msg.record.SessionFile {
				target.SessionFile = msg.record.SessionFile
				m.pins[slot] = target
				pinsChanged = true
			}
		}
		if pinsChanged {
			if err := savePins(m.pins); err != nil {
				m.err = err
				return m, nil
			}
			if err := syncPinShortcuts(m.kitty, m.pins); err != nil {
				m.err = err
				return m, nil
			}
		}
		m.err = nil
		return m, nil
	case tea.KeyMsg:
		key := msg.String()
		if key == "ctrl+c" {
			return m, tea.Quit
		}
		if m.saving || m.mergedWorktreeBusy {
			return m, nil
		}
		if m.mode == modeSaveConfirm {
			switch key {
			case "esc":
				m.mode = modeNormal
				m.saveForeground = false
				m.err = nil
			case "y":
				m.mode = modeNormal
				m.saving = true
				m.err = nil
				entry := m.entries[m.saveEntry]
				return m, runSaveSession(m.kitty, entry, m.saveEntry, m.saveForeground)
			default:
				m.err = fmt.Errorf("press y to confirm or esc to cancel")
			}
			return m, nil
		}
		if m.mode == modePin {
			switch {
			case key == "esc":
				m.mode = modeNormal
				m.confirmSlot = ""
				m.err = nil
			case key == "x":
				m.unpinSelected()
			case validSlot(key):
				m.assignPin(key)
			default:
				m.err = fmt.Errorf("pin slot must be a digit from 0 to 9")
			}
			return m, nil
		}
		if m.mode == modeCloseConfirm {
			if m.closeBusy {
				return m, nil
			}
			isWorktreeRow := m.closeRow.section == "wt-item"
			switch key {
			case "esc":
				m.mode = modeNormal
				m.worktreeForcePrompt = false
				m.mergedWorktrees = nil
				m.destroyPlan = nil
				m.err = nil
			case "y":
				if m.destroyPlan != nil {
					m.closeBusy = true
					m.err = nil
					selected := m.closeRow
					return m, runDestroy(m.kitty, m.zoxide, m.entries[selected.entryIndex], *m.destroyPlan)
				}
				if len(m.mergedWorktrees) > 0 {
					m.closeBusy = true
					m.err = nil
					return m, m.runRemoveMergedWorktrees()
				}
				if m.closeRow.section == "wt-bulk" {
					m.closeBusy = true
					m.err = nil
					return m, m.runRemoveWorktrees()
				}
				if isWorktreeRow {
					if m.worktreeForcePrompt {
						m.err = fmt.Errorf("press f to force, or esc to cancel")
						return m, nil
					}
					m.closeBusy = true
					m.err = nil
					return m, m.runRemoveWorktree(false)
				}
				m.closeBusy = true
				m.err = nil
				selected := m.closeRow
				return m, runClose(m.kitty, m.zoxide, m.entries[selected.entryIndex], selected)
			case "f":
				if isWorktreeRow {
					m.closeBusy = true
					m.err = nil
					return m, m.runRemoveWorktree(true)
				}
				m.err = fmt.Errorf("press y to confirm or esc to cancel")
			default:
				m.err = fmt.Errorf("press y to confirm or esc to cancel")
			}
			return m, nil
		}
		if m.mode == modeClone {
			if m.cloneBusy {
				return m, nil
			}
			switch key {
			case "esc":
				m.resetClone()
			case "tab", "shift+tab":
				m.cloneDestinationFocus = !m.cloneDestinationFocus
				m.err = nil
			case "enter":
				if _, err := repositoryName(m.cloneRepository); err != nil {
					m.err = err
					return m, nil
				}
				destination, err := resolveCloneDestination(m.cloneDestination, m.cloneRoot)
				if err != nil {
					m.err = err
					return m, nil
				}
				m.cloneBusy = true
				m.err = nil
				return m, runClone(m.kitty, m.zoxide, m.cloneRepository, destination)
			case "backspace":
				value := &m.cloneRepository
				if m.cloneDestinationFocus {
					value = &m.cloneDestination
					m.cloneDestinationEdited = true
				}
				runes := []rune(*value)
				if len(runes) > 0 {
					*value = string(runes[:len(runes)-1])
				}
				m.refreshCloneDestination()
			case "ctrl+u":
				if m.cloneDestinationFocus {
					m.cloneDestination = ""
					m.cloneDestinationEdited = true
				} else {
					m.cloneRepository = ""
				}
				m.refreshCloneDestination()
			default:
				if len(msg.Runes) > 0 && !msg.Alt {
					if m.cloneDestinationFocus {
						m.cloneDestination += string(msg.Runes)
						m.cloneDestinationEdited = true
					} else {
						m.cloneRepository += string(msg.Runes)
					}
					m.refreshCloneDestination()
				}
			}
			return m, nil
		}
		if m.mode == modeCreateSession {
			switch key {
			case "esc":
				m.mode = modeNormal
				m.createValue = ""
				m.err = nil
			case "enter":
				name := safeName(m.createValue)
				if name == "" {
					m.err = fmt.Errorf("session name is required")
				} else {
					return m, runCreateSession(m.kitty, m.selectedEntries(), name)
				}
			case "backspace":
				runes := []rune(m.createValue)
				if len(runes) > 0 {
					m.createValue = string(runes[:len(runes)-1])
				}
			case "ctrl+u":
				m.createValue = ""
			default:
				if len(msg.Runes) > 0 && !msg.Alt && !msg.Paste {
					m.createValue += string(msg.Runes)
				}
			}
			return m, nil
		}
		if m.mode == modeRename {
			switch key {
			case "esc":
				m.mode = modeNormal
				m.renameValue = ""
			case "enter":
				if len(m.rows) > 0 {
					selected := m.rows[m.cursor]
					return m, runRename(m.kitty, m.entries[selected.entryIndex], selected, m.renameValue, m.names)
				}
			case "backspace":
				runes := []rune(m.renameValue)
				if len(runes) > 0 {
					m.renameValue = string(runes[:len(runes)-1])
				}
			case "ctrl+u":
				m.renameValue = ""
			default:
				if len(msg.Runes) > 0 && !msg.Alt && !msg.Paste {
					m.renameValue += string(msg.Runes)
				}
			}
			return m, nil
		}
		if m.mode == modeWorktreeCreate {
			switch key {
			case "esc":
				m.mode = modeNormal
				m.worktreeBranch = ""
				m.worktreePaths = nil
				m.err = nil
			case "tab", "shift+tab":
				if m.worktreeRecipe != nil {
					// The three user-facing choices are native, the template's
					// declared default, and an explicit workspace override.
					modes := []string{"native", "template"}
					if len(m.worktreeRecipe.Workspaces) > 1 {
						modes = append(modes, "workspaces")
					}
					current := "template"
					if m.worktreeRecipeMode == "none" {
						current = "native"
					} else if m.worktreeCustomWorkspaces {
						current = "workspaces"
					}
					index := 0
					for i, mode := range modes {
						if mode == current {
							index = i
							break
						}
					}
					if key == "shift+tab" {
						index = (index - 1 + len(modes)) % len(modes)
					} else {
						index = (index + 1) % len(modes)
					}
					switch modes[index] {
					case "native":
						m.worktreeRecipeMode = "none"
						m.worktreeCustomWorkspaces = false
					case "template":
						m.worktreeRecipeMode = m.worktreeRecipe.WorkspaceMode
						m.worktreeCustomWorkspaces = false
					case "workspaces":
						m.worktreeRecipeMode = "selected"
						m.worktreeCustomWorkspaces = true
						m.worktreeSelected = make([]bool, len(m.worktreeRecipe.Workspaces))
						for i := range m.worktreeSelected {
							m.worktreeSelected[i] = true
						}
					}
				}
			case "up", "ctrl+k":
				if m.worktreeCustomWorkspaces && m.worktreeWorkspaceCursor > 0 {
					m.worktreeWorkspaceCursor--
				}
			case "down", "ctrl+j":
				if m.worktreeCustomWorkspaces && m.worktreeWorkspaceCursor < len(m.worktreeSelected)-1 {
					m.worktreeWorkspaceCursor++
				}
			case "enter":
				if m.worktreeBranch == "" {
					m.err = fmt.Errorf("branch name is required")
					return m, nil
				}
				m.err = nil
				if m.worktreeRecipe != nil && m.worktreeRecipeMode != "none" {
					var selected []string
					if m.worktreeRecipeMode == "selected" {
						selected = m.selectedWorkspaceNames()
						if len(selected) == 0 {
							m.err = fmt.Errorf("select at least one workspace")
							return m, nil
						}
					}
					m.worktreeBusy = true
					return m, runWktreeNew(m.worktreeRecipePath, m.worktreeRecipeMode, m.worktreeBranch, selected)
				}
				return m, m.validateWorktreeBranch()
			case "backspace":
				runes := []rune(m.worktreeBranch)
				if len(runes) > 0 {
					m.worktreeBranch = string(runes[:len(runes)-1])
					m.worktreePaths = m.calculateWorktreePaths()
					m.err = nil
				}
			case "ctrl+u":
				m.worktreeBranch = ""
				m.worktreePaths = m.calculateWorktreePaths()
				m.err = nil
			default:
				if m.worktreeCustomWorkspaces && key == " " && len(m.worktreeSelected) > 0 {
					m.worktreeSelected[m.worktreeWorkspaceCursor] = !m.worktreeSelected[m.worktreeWorkspaceCursor]
					return m, nil
				}
				if len(msg.Runes) > 0 && !msg.Alt && !msg.Paste {
					m.worktreeBranch += string(msg.Runes)
					m.worktreePaths = m.calculateWorktreePaths()
					m.err = nil
				}
			}
			return m, nil
		}
		if m.mode == modeCheckoutPR {
			if m.prCheckoutBusy {
				return m, nil
			}
			previousValue := m.prCheckoutValue
			switch key {
			case "esc":
				m.mode = modeNormal
				m.prCheckoutValue = ""
				m.prCheckoutBranch = ""
				m.checkoutRoot = ""
				m.err = nil
			case "enter":
				owner, repo, number, useSelected, err := parsePullRequestInput(m.prCheckoutValue)
				if err != nil {
					m.err = err
					return m, nil
				}
				selectedRepoPath := ""
				if useSelected {
					if len(m.rows) == 0 || m.cursor < 0 || m.cursor >= len(m.rows) {
						m.err = fmt.Errorf("select a project or paste a full PR URL")
						return m, nil
					}
					selectedRepoPath = m.entries[m.rows[m.cursor].entryIndex].path
					if selectedRepoPath == "" {
						m.err = fmt.Errorf("select a project or paste a full PR URL")
						return m, nil
					}
				}
				m.prCheckoutBusy = true
				m.err = nil
				return m, runCheckoutPR(m.kitty, m.zoxide, owner, repo, number, selectedRepoPath, m.checkoutRoot, m.cloneRoot)
			case "backspace":
				runes := []rune(m.prCheckoutValue)
				if len(runes) > 0 {
					m.prCheckoutValue = string(runes[:len(runes)-1])
					m.err = nil
				}
			case "ctrl+u":
				m.prCheckoutValue = ""
				m.err = nil
			default:
				if len(msg.Runes) > 0 && !msg.Alt {
					m.prCheckoutValue += string(msg.Runes)
					m.err = nil
				}
			}
			if m.prCheckoutValue == previousValue {
				return m, nil
			}
			m.prCheckoutBranch = ""
			if owner, repo, number, selected, err := parsePullRequestInput(m.prCheckoutValue); err == nil && !selected {
				return m, resolvePRPreview(m.prCheckoutValue, owner, repo, number)
			}
			return m, nil
		}
		if m.mode == modeSearch {
			switch key {
			case "esc", "enter":
				m.mode = modeNormal
			case "up", "ctrl+k":
				if m.cursor > 0 {
					m.cursor--
				}
			case "down", "ctrl+j":
				if m.cursor+1 < len(m.rows) {
					m.cursor++
				}
			case " ":
				m.query += " "
				m.rebuildRows()
			case "backspace":
				runes := []rune(m.query)
				if len(runes) > 0 {
					m.query = string(runes[:len(runes)-1])
					m.rebuildRows()
				}
			case "ctrl+u":
				m.query = ""
				m.rebuildRows()
			default:
				if len(msg.Runes) > 0 && !msg.Alt && !msg.Paste {
					m.query += string(msg.Runes)
					m.rebuildRows()
				}
			}
			return m, m.queuePreview()
		}
		switch key {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "esc":
			// Escape returns to command mode from transient modes; once there,
			// it is intentionally a no-op so a repeated key cannot close Kesh.
			if m.filter == filterWorktrees {
				// Worktree rows are not selectable; discard any main-list selection
				// before returning so it cannot affect subsequent bulk actions.
				m.selected = map[string]bool{}
				m.wtBulkSelected = nil
				// Return to previous filter
				m.filter = m.previousFilter
				m.worktreeFilterEntryIndex = -1
				m.worktreeFilterRows = nil
				m.query = ""
				m.rebuildRows()
				return m, nil
			}
			return m, nil
		case "/":
			m.mode = modeSearch
		case "up", "ctrl+k", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "ctrl+j", "j":
			if m.cursor+1 < len(m.rows) {
				m.cursor++
			}
		case "ctrl+d", "pgdown":
			m.cursor = min(m.cursor+m.listPageSize(), max(0, len(m.rows)-1))
		case "ctrl+u", "pgup":
			m.cursor = max(0, m.cursor-m.listPageSize())
		case "home":
			m.cursor = 0
		case "end", "G":
			m.cursor = max(0, len(m.rows)-1)
		case "right", "l":
			m.expandOrDescend()
		case "left", "h":
			m.ascendOrCollapse()
		case "e":
			// Toggle the selected node's subtree (session or tab).
			m.toggleExpandAtCursor()
		case " ":
			m.toggleSelected()
		case "enter":
			if len(m.rows) == 0 {
				return m, nil
			}
			r := m.rows[m.cursor]
			if m.filter == filterWorktrees && r.section == "wt-filter" {
				// Focus the worktree
				if m.cursor >= len(m.worktreeFilterRows) {
					return m, nil
				}
				wtRow := m.worktreeFilterRows[m.cursor]
				wt := wtRow.worktree
				entry := m.entries[m.worktreeFilterEntryIndex]

				// Find if there's a window with this worktree path
				windowIDs, err := worktreeWindowIDs(m.kitty, wt.path)
				if err != nil {
					m.err = fmt.Errorf("find existing worktree window: %w", err)
					return m, nil
				}
				if len(windowIDs) > 0 {
					// Focus the existing window
					return m, func() tea.Msg {
						return actionMsg{err: run(m.kitty, "@", "focus-window", "--match", "id:"+strconv.Itoa(windowIDs[0]))}
					}
				}
				// No window found, open a new tab with the worktree
				return m, runAction(m.kitty, m.zoxide, entry, row{
					entryIndex:   m.worktreeFilterEntryIndex,
					tabIndex:     -1,
					windowIndex:  -1,
					worktreePath: wt.path,
				})
			}
			return m, runAction(m.kitty, m.zoxide, m.entries[r.entryIndex], r)
		case "n":
			if m.filter == filterWorktrees {
				return m, m.beginWorktreeCreate()
			}
			if len(m.selected) == 0 {
				m.err = fmt.Errorf("select at least one project or SSH host first")
				return m, nil
			}
			m.mode = modeCreateSession
			m.createValue = ""
			m.err = nil
			return m, nil
		case "c":
			root, err := loadCloneRoot()
			if err != nil {
				m.err = err
				return m, nil
			}
			m.mode = modeClone
			m.cloneRoot = root
			m.cloneRepository = ""
			m.cloneDestination = displayPath(root, os.Getenv("HOME"))
			m.cloneDestinationFocus = false
			m.cloneDestinationEdited = false
			m.err = nil
			return m, nil
		case "C":
			cloneRoot, err := loadCloneRoot()
			if err != nil {
				m.err = err
				return m, nil
			}
			checkoutRoot, err := loadCheckoutRoot()
			if err != nil {
				m.err = err
				return m, nil
			}
			m.mode = modeCheckoutPR
			m.prCheckoutValue = ""
			m.prCheckoutBranch = ""
			m.cloneRoot = cloneRoot
			m.checkoutRoot = checkoutRoot
			m.err = nil
			return m, nil
		case "r":
			if m.filter == filterWorktrees && m.worktreeFilterEntryIndex >= 0 && m.worktreeFilterEntryIndex < len(m.entries) {
				return m, fetchOriginThenReload(m.entries[m.worktreeFilterEntryIndex].path, m.worktreeFilterEntryIndex)
			}
			m.beginRename()
		case "s", "S":
			if len(m.rows) == 0 {
				return m, nil
			}
			selected := m.rows[m.cursor]
			entry := m.entries[selected.entryIndex]
			if !entry.open {
				m.err = fmt.Errorf("save an open project or workspace")
				return m, nil
			}
			if entry.session == "" {
				m.err = fmt.Errorf("unnamed workspaces cannot be saved yet")
				return m, nil
			}
			m.mode = modeSaveConfirm
			m.saveForeground = key == "S"
			m.saveEntry = selected.entryIndex
			m.err = nil
			return m, nil
		case "g":
			if m.filter == filterWorktrees && len(m.worktreeFilterRows) > 0 && m.cursor < len(m.worktreeFilterRows) {
				wt := m.worktreeFilterRows[m.cursor].worktree
				if wt.prURL != "" {
					prURL := wt.prURL
					return m, func() tea.Msg {
						return openPRMsg{err: openURL(prURL)}
					}
				}
				m.err = fmt.Errorf("no PR associated with this worktree")
				return m, nil
			}
			if m.filter == filterWorktrees {
				return m, m.openWorktreePR()
			}
			// vim-style: jump to the top of the list.
			m.cursor = 0
		case "x":
			if m.filter == filterWorktrees && len(m.wtBulkSelected) > 0 {
				// Bulk remove every selected worktree. The tab list can be a
				// filtered subset, so resolve targets from worktreeFilterRows by
				// path. Route through the confirm popup like the single x.
				targets := m.selectedWorktreeItems()
				if len(targets) == 0 {
					m.err = fmt.Errorf("no matching worktrees to remove")
					return m, nil
				}
				m.bulkWorktrees = targets
				m.closeRow = row{
					entryIndex:  m.worktreeFilterEntryIndex,
					tabIndex:    -1,
					windowIndex: -1,
					section:     "wt-bulk",
				}
				m.mode = modeCloseConfirm
				m.closeBusy = false
				m.worktreeForcePrompt = false
				m.mergedWorktrees = nil
				m.destroyPlan = nil
				m.err = nil
				return m, nil
			}
			if m.filter == filterWorktrees && len(m.worktreeFilterRows) > 0 && m.cursor < len(m.worktreeFilterRows) {
				// Route through the confirm popup (like main-mode x) instead of
				// force-removing immediately. The tab list can be a filtered
				// subset, so address the worktree by path, not by wt index.
				wt := m.worktreeFilterRows[m.cursor].worktree
				m.closeRow = row{
					entryIndex:   m.worktreeFilterEntryIndex,
					tabIndex:     -1,
					windowIndex:  -1,
					section:      "wt-item",
					worktreePath: wt.path,
				}
				m.mode = modeCloseConfirm
				m.closeBusy = false
				m.worktreeForcePrompt = false
				m.mergedWorktrees = nil
				m.err = nil
				return m, nil
			}
			m.beginClose()
		case "X":
			return m, m.findMergedWorktrees()
		case "D":
			if m.filter == filterWorktrees && len(m.worktreeFilterRows) > 0 && m.cursor >= 0 && m.cursor < len(m.worktreeFilterRows) {
				// In the Worktree tab, destroy the selected worktree only — not
				// the whole project. The tab list can be a filtered subset, so
				// resolve the worktree from worktreeFilterRows, not by index.
				wt := m.worktreeFilterRows[m.cursor].worktree
				branch := wt.branch
				if branch == "" || branch == "(detached)" {
					branch = ""
				}
				plan := destroyPlan{entryName: wt.branch, worktreePath: wt.path, branch: branch}
				m.closeRow = row{
					entryIndex:   m.worktreeFilterEntryIndex,
					tabIndex:     -1,
					windowIndex:  -1,
					section:      "wt-item",
					worktreePath: wt.path,
				}
				m.destroyPlan = &plan
				m.mode = modeCloseConfirm
				m.closeBusy = false
				m.worktreeForcePrompt = false
				m.mergedWorktrees = nil
				m.err = nil
				return m, nil
			}
			if len(m.rows) == 0 {
				return m, nil
			}
			selected := m.rows[m.cursor]
			entry := m.entries[selected.entryIndex]
			plan := detectDestroyPlan(entry)
			if selected.section == "wt-item" {
				if wt, ok := m.worktreeForRow(selected); ok {
					branch := wt.branch
					if branch == "" || branch == "(detached)" {
						branch = ""
					}
					plan = destroyPlan{entryName: wt.branch, worktreePath: wt.path, branch: branch}
				}
			}
			m.closeRow = selected
			m.destroyPlan = &plan
			m.mode = modeCloseConfirm
			m.closeBusy = false
			m.worktreeForcePrompt = false
			m.mergedWorktrees = nil
			m.err = nil
			return m, nil
		case "w":
			// Open the Worktree tab for the project under the cursor.
			if len(m.rows) == 0 {
				m.err = fmt.Errorf("no entry selected")
				return m, nil
			}
			selected := m.rows[m.cursor]
			if selected.section != "" {
				m.err = fmt.Errorf("select a project entry, not a worktree or window")
				return m, nil
			}
			m.previousFilter = m.filter
			m.worktreeFilterEntryIndex = selected.entryIndex
			m.filter = filterWorktrees
			m.query = ""
			m.worktreeFilterRows = nil

			// Fetch worktrees if not already loaded
			entry := m.entries[selected.entryIndex]
			if !entry.worktreesLoaded {
				return m, fetchWorktrees(entry.path, selected.entryIndex, -1, -1)
			}
			m.rebuildWorktreeRows()
			return m, nil
		case "p":
			if m.filter == filterWorktrees && len(m.wtBulkSelected) > 0 {
				if m.worktreePullBusy {
					return m, nil
				}
				targets := m.selectedWorktreeItems()
				if len(targets) == 0 {
					m.err = fmt.Errorf("no matching worktrees to pull")
					return m, nil
				}
				entry := m.entries[m.worktreeFilterEntryIndex]
				m.worktreePullBusy = true
				m.err = nil
				return m, pullWorktrees(targets, entry.path, m.worktreeFilterEntryIndex)
			}
			if m.filter == filterWorktrees && m.cursor >= 0 && m.cursor < len(m.worktreeFilterRows) {
				if m.worktreePullBusy {
					return m, nil
				}
				wt := m.worktreeFilterRows[m.cursor].worktree
				entry := m.entries[m.worktreeFilterEntryIndex]
				m.worktreePullBusy = true
				m.err = nil
				return m, pullWorktree(wt.path, entry.path, m.worktreeFilterEntryIndex)
			}
			if m.hasSelectedAgentWindow() {
				m.showPreview = !m.showPreview
				if m.showPreview {
					m.previewID = 0
				}
			} else {
				m.beginPin()
			}
		case "tab":
			m.filter = (m.filter + 1) % 7
			m.resetWorktreeTab()
			m.rebuildRows()
		case "shift+tab":
			m.filter = (m.filter + 6) % 7
			m.resetWorktreeTab()
			m.rebuildRows()
		}
		return m, m.queuePreview()
	}
	return m, nil
}

func (m *model) toggleSelected() {
	if len(m.rows) == 0 {
		return
	}
	// In the Worktree tab, space bulk-selects worktree paths for x (remove)
	// and p (pull) instead of toggling project selection.
	if m.filter == filterWorktrees && m.cursor >= 0 && m.cursor < len(m.worktreeFilterRows) {
		wt := m.worktreeFilterRows[m.cursor].worktree
		if wt.path == "" {
			return
		}
		if m.wtBulkSelected == nil {
			m.wtBulkSelected = map[string]bool{}
		}
		if m.wtBulkSelected[wt.path] {
			delete(m.wtBulkSelected, wt.path)
		} else {
			m.wtBulkSelected[wt.path] = true
		}
		m.err = nil
		return
	}
	r := m.rows[m.cursor]
	if r.tabIndex >= 0 || (m.entries[r.entryIndex].kind != "project" && m.entries[r.entryIndex].kind != "ssh") {
		m.err = fmt.Errorf("select a source project or SSH host, not a workspace, tab, or window")
		return
	}
	key := m.entries[r.entryIndex].key
	if m.selected == nil {
		m.selected = map[string]bool{}
	}
	if m.selected[key] {
		delete(m.selected, key)
	} else {
		m.selected[key] = true
	}
	m.err = nil
}

func (m model) selectedEntries() []entry {
	entries := make([]entry, 0, len(m.selected))
	for _, candidate := range m.entries {
		if m.selected[candidate.key] {
			entries = append(entries, candidate)
		}
	}
	return entries
}

// resetWorktreeTab clears the Worktree tab's project context and selection. The
// tab is only meaningful when a project is chosen with w; arriving by cycling
// Tab/Shift+tab carries no selection, so it must render empty with no folder
// name rather than reusing a stale (or default 0) entry index.
func (m *model) resetWorktreeTab() {
	m.worktreeFilterEntryIndex = -1
	m.worktreeFilterRows = nil
	m.wtBulkSelected = nil
}

// worktreeEntries resolves the projects a worktree action targets. In the
// Worktree tab it is the project whose tab is open; otherwise selection drives
// multi-project worktrees, and with nothing selected the project under the
// cursor is used so a single worktree needs no explicit selection.
func (m *model) worktreeEntries() []entry {
	if m.filter == filterWorktrees && m.worktreeFilterEntryIndex >= 0 && m.worktreeFilterEntryIndex < len(m.entries) {
		if e := m.entries[m.worktreeFilterEntryIndex]; e.kind == "project" && e.path != "" {
			return []entry{e}
		}
	}
	if len(m.selected) > 0 {
		return m.selectedEntries()
	}
	if len(m.rows) == 0 {
		return nil
	}
	e := m.entries[m.rows[m.cursor].entryIndex]
	if e.kind != "project" {
		return nil
	}
	return []entry{e}
}

func (m *model) beginClose() {
	if len(m.rows) == 0 {
		return
	}
	selected := m.rows[m.cursor]
	entry := m.entries[selected.entryIndex]
	if selected.section == "wt-head" {
		m.err = fmt.Errorf("select a worktree to delete")
		return
	}
	if selected.section != "wt-item" && selected.tabIndex < 0 && len(entry.tabs) == 0 && !(entry.saved && !entry.open) {
		m.err = fmt.Errorf("%s is not open", entry.name)
		return
	}
	m.closeRow = selected
	m.mode = modeCloseConfirm
	m.closeBusy = false
	m.worktreeForcePrompt = false
	m.mergedWorktrees = nil
	m.err = nil
}

func (m *model) refreshCloneDestination() {
	if m.cloneDestinationEdited {
		return
	}
	m.cloneDestination = displayPath(m.cloneRoot, os.Getenv("HOME"))
	if name, err := repositoryName(m.cloneRepository); err == nil {
		m.cloneDestination = displayPath(filepath.Join(m.cloneRoot, name), os.Getenv("HOME"))
	}
}

func (m *model) resetClone() {
	m.mode = modeNormal
	m.cloneBusy = false
	m.cloneDestinationFocus = false
	m.cloneDestinationEdited = false
	m.cloneRepository = ""
	m.cloneDestination = ""
	m.cloneRoot = ""
	m.err = nil
}

func (m *model) beginPin() {
	if len(m.rows) == 0 {
		return
	}
	selected := m.rows[m.cursor]
	m.pinEntry = selected.entryIndex
	m.mode = modePin
	m.confirmSlot = ""
	m.err = nil
}

func (m *model) assignPin(slot string) {
	selected := m.entries[m.pinEntry]
	if occupied, ok := m.pins[slot]; ok && occupied.Key != selected.key && m.confirmSlot != slot {
		m.confirmSlot = slot
		m.err = fmt.Errorf("slot %s is pinned to %s; press %s again to replace it", slot, occupied.Name, slot)
		return
	}
	updated := make(pinStore, len(m.pins)+1)
	for existingSlot, target := range m.pins {
		if target.Key != selected.key && existingSlot != slot {
			updated[existingSlot] = target
		}
	}
	target, err := pinTargetForEntry(selected)
	if err != nil {
		m.err = err
		return
	}
	updated[slot] = target
	if err := savePins(updated); err != nil {
		m.err = err
		return
	}
	if err := syncPinShortcuts(m.kitty, updated); err != nil {
		m.err = err
		return
	}
	m.pins = updated
	applyPins(m.entries, m.pins)
	m.mode = modeNormal
	m.confirmSlot = ""
	m.err = nil
}

func (m *model) unpinSelected() {
	selected := m.entries[m.pinEntry]
	updated := make(pinStore, len(m.pins))
	for slot, target := range m.pins {
		if target.Key != selected.key {
			updated[slot] = target
		}
	}
	if len(updated) == len(m.pins) {
		m.err = fmt.Errorf("%s is not pinned", selected.name)
		return
	}
	if err := savePins(updated); err != nil {
		m.err = err
		return
	}
	if err := syncPinShortcuts(m.kitty, updated); err != nil {
		m.err = err
		return
	}
	m.pins = updated
	applyPins(m.entries, m.pins)
	m.mode = modeNormal
	m.confirmSlot = ""
	m.err = nil
}

func (m *model) beginRename() {
	if len(m.rows) == 0 {
		return
	}
	selected := m.rows[m.cursor]
	entry := &m.entries[selected.entryIndex]
	if selected.windowIndex >= 0 {
		m.renameValue = entry.tabs[selected.tabIndex].windows[selected.windowIndex].title
		m.mode = modeRename
		m.err = nil
		return
	}
	if selected.tabIndex >= 0 {
		m.renameValue = entry.tabs[selected.tabIndex].title
		m.mode = modeRename
		m.err = nil
		return
	}
	if entry.kind == "project" {
		m.err = fmt.Errorf("rename an open workspace, not its source project")
		return
	}
	m.renameValue = entry.name
	m.mode = modeRename
	m.err = nil
}

func (m *model) expandOrDescend() {
	if len(m.rows) == 0 {
		return
	}
	r := m.rows[m.cursor]
	e := &m.entries[r.entryIndex]
	if r.windowIndex >= 0 {
		return
	}
	if r.tabIndex >= 0 {
		tab := &e.tabs[r.tabIndex]
		if len(tab.windows) == 0 {
			return
		}
		if !tab.expanded {
			tab.expanded = true
			m.rebuildRows()
			return
		}
		if m.cursor+1 < len(m.rows) && m.rows[m.cursor+1].tabIndex == r.tabIndex {
			m.cursor++
		}
		return
	}
	if len(e.tabs) == 0 {
		return
	}
	if !e.expanded {
		e.expanded = true
		m.rebuildRows()
		return
	}
	if m.cursor+1 < len(m.rows) && m.rows[m.cursor+1].entryIndex == r.entryIndex {
		m.cursor++
	}
}

func (m *model) ascendOrCollapse() {
	if len(m.rows) == 0 {
		return
	}
	r := m.rows[m.cursor]
	e := &m.entries[r.entryIndex]
	if r.windowIndex >= 0 {
		for m.cursor > 0 {
			m.cursor--
			if m.rows[m.cursor].tabIndex == r.tabIndex && m.rows[m.cursor].windowIndex < 0 {
				break
			}
		}
		return
	}
	if r.tabIndex >= 0 {
		if e.tabs[r.tabIndex].expanded {
			e.tabs[r.tabIndex].expanded = false
			m.rebuildRows()
			return
		}
		for m.cursor > 0 {
			m.cursor--
			if m.rows[m.cursor].tabIndex < 0 {
				break
			}
		}
		return
	}
	if e.expanded {
		e.expanded = false
		m.rebuildRows()
	}
}

// toggleExpandAtCursor expands or collapses the cursor's node in one keystroke,
// so the user does not have to walk the tree node by node:
//
//   - session row → toggle that session's whole subtree (the entry plus all its
//     tabs). Other sessions are not touched.
//   - tab or window row → toggle only that tab's windows.
//
// For the session scope, if the entry or any of its tabs is collapsed, expand
// the entry and all its tabs; otherwise collapse the entry. Entries without
// tabs (closed/saved projects, SSH hosts) are untouched.
func (m *model) toggleExpandAtCursor() {
	if len(m.rows) == 0 {
		return
	}
	r := m.rows[m.cursor]
	if r.entryIndex < 0 || r.entryIndex >= len(m.entries) {
		return
	}
	e := &m.entries[r.entryIndex]
	if r.tabIndex >= 0 {
		// On a tab/window row: toggle only that tab's windows. The session is
		// already expanded (otherwise the tab would not be visible).
		if r.tabIndex >= len(e.tabs) {
			return
		}
		e.tabs[r.tabIndex].expanded = !e.tabs[r.tabIndex].expanded
		m.rebuildRows()
		return
	}
	// Session row: toggle only this session's subtree.
	if len(e.tabs) == 0 {
		return
	}
	anyCollapsed := !e.expanded
	for t := range e.tabs {
		if !e.tabs[t].expanded {
			anyCollapsed = true
		}
	}
	if anyCollapsed {
		e.expanded = true
		for t := range e.tabs {
			e.tabs[t].expanded = true
		}
	} else {
		e.expanded = false
	}
	m.rebuildRows()
}

// preserveExpandedState keeps hierarchy navigation stable when an operation
// reloads Kitty's entries. IDs are supplied by Kitty and remain stable across a
// refresh, while the entry structs themselves are rebuilt from scratch.
func preserveExpandedState(previous, refreshed []entry) {
	oldEntries := make(map[string]entry, len(previous))
	for _, entry := range previous {
		oldEntries[entry.key] = entry
	}
	for i := range refreshed {
		old, ok := oldEntries[refreshed[i].key]
		if !ok {
			continue
		}
		refreshed[i].expanded = old.expanded
		oldTabs := make(map[int]bool, len(old.tabs))
		for _, tab := range old.tabs {
			oldTabs[tab.id] = tab.expanded
		}
		for tabIndex := range refreshed[i].tabs {
			if expanded, ok := oldTabs[refreshed[i].tabs[tabIndex].id]; ok {
				refreshed[i].tabs[tabIndex].expanded = expanded
			}
		}
	}
}

func (m *model) rebuildRows() {
	if m.filter == filterAgents {
		m.rebuildAgentRows()
		return
	}
	if m.filter == filterWorktrees {
		m.rebuildWorktreeRows()
		return
	}
	entryIndexes := make([]int, 0, len(m.entries))
	searchValues := make([]string, 0, len(m.entries))
	for i := range m.entries {
		e := m.entries[i]
		if !m.matchesFilter(e) {
			continue
		}
		entryIndexes = append(entryIndexes, i)
		searchValues = append(searchValues, e.name+" "+e.originalName+" "+e.detail)
	}
	if m.query != "" {
		matches := fuzzy.Find(m.query, searchValues)
		ranked := make([]int, 0, len(matches))
		for _, match := range matches {
			ranked = append(ranked, entryIndexes[match.Index])
		}
		// Fuzzy relevance determines the order within each group. Live
		// workspaces rank first, followed by restorable saved workspaces, then
		// source projects and SSH hosts.
		priority := func(e entry) int {
			if e.open {
				return 0
			}
			if e.saved {
				return 1
			}
			return 2
		}
		sort.SliceStable(ranked, func(i, j int) bool {
			return priority(m.entries[ranked[i]]) < priority(m.entries[ranked[j]])
		})
		entryIndexes = ranked
	}
	// Pins are shortcuts first, so give them a stable, visible home at the
	// top of every picker view. Non-pinned entries retain their normal order.
	sort.SliceStable(entryIndexes, func(i, j int) bool {
		left, right := m.entries[entryIndexes[i]], m.entries[entryIndexes[j]]
		if left.pin == "" || right.pin == "" {
			return left.pin != ""
		}
		return left.pin < right.pin
	})

	var rows []row
	for _, entryIndex := range entryIndexes {
		e := &m.entries[entryIndex]
		rows = append(rows, row{entryIndex: entryIndex, tabIndex: -1, windowIndex: -1})
		if e.worktreesOpen && e.worktreesLoaded && m.query == "" {
			rows = append(rows, row{entryIndex: entryIndex, tabIndex: -1, windowIndex: -1, section: "wt-head"})
			for wt := range e.worktrees {
				rows = append(rows, row{entryIndex: entryIndex, tabIndex: -1, windowIndex: -1, section: "wt-item", wt: wt})
			}
		}
		if e.expanded && m.query == "" {
			for tabIndex := range e.tabs {
				rows = append(rows, row{entryIndex: entryIndex, tabIndex: tabIndex, windowIndex: -1})
				if e.tabs[tabIndex].expanded {
					for windowIndex := range e.tabs[tabIndex].windows {
						rows = append(rows, row{entryIndex: entryIndex, tabIndex: tabIndex, windowIndex: windowIndex})
						w := e.tabs[tabIndex].windows[windowIndex]
						if w.worktreesOpen && w.worktreesLoaded {
							rows = append(rows, row{entryIndex: entryIndex, tabIndex: tabIndex, windowIndex: windowIndex, section: "wt-head"})
							for wt := range w.worktrees {
								rows = append(rows, row{entryIndex: entryIndex, tabIndex: tabIndex, windowIndex: windowIndex, section: "wt-item", wt: wt})
							}
						}
					}
				}
			}
		}
	}
	m.rows = rows
	if m.cursor >= len(rows) {
		m.cursor = max(0, len(rows)-1)
	}
}

func (m model) selectedDetailPath() string {
	if len(m.rows) == 0 || m.cursor < 0 || m.cursor >= len(m.rows) {
		return ""
	}
	selected := m.rows[m.cursor]
	entry := m.entries[selected.entryIndex]
	if selected.section == "wt-item" {
		worktrees := m.worktreesForRow(selected)
		if selected.wt >= 0 && selected.wt < len(worktrees) {
			return worktrees[selected.wt].path
		}
	}
	if selected.windowIndex >= 0 {
		return entry.tabs[selected.tabIndex].windows[selected.windowIndex].cwd
	}
	if selected.tabIndex >= 0 {
		for _, window := range entry.tabs[selected.tabIndex].windows {
			if window.cwd != "" {
				return window.cwd
			}
		}
	}
	return entry.path
}

func (m *model) queuePathPR() tea.Cmd {
	path := m.selectedDetailPath()
	if path == "" {
		return nil
	}
	if m.pathPRChecked == nil {
		m.pathPRChecked = map[string]bool{}
	}
	if m.pathPRChecked[path] {
		return nil
	}
	m.pathPRChecked[path] = true
	return func() tea.Msg {
		return pathPRMsg{path: path, info: cachedPathPR(path)}
	}
}

func (m model) hasSelectedAgentWindow() bool {
	if len(m.rows) == 0 || m.cursor < 0 || m.cursor >= len(m.rows) {
		return false
	}
	r := m.rows[m.cursor]
	return r.windowIndex >= 0 && m.entries[r.entryIndex].tabs[r.tabIndex].windows[r.windowIndex].agent != ""
}

func (m model) shouldRefreshPreview(windowID int) bool {
	if !m.showPreview || !m.hasSelectedAgentWindow() || m.previewID != windowID {
		return false
	}
	r := m.rows[m.cursor]
	return m.entries[r.entryIndex].tabs[r.tabIndex].windows[r.windowIndex].id == windowID
}

func (m model) queuePreviewRefresh(windowID int) tea.Cmd {
	if !m.shouldRefreshPreview(windowID) {
		return nil
	}
	return tea.Tick(previewRefreshInterval, func(time.Time) tea.Msg {
		return previewRefreshMsg{windowID: windowID}
	})
}

func (m *model) queuePreview() tea.Cmd {
	commands := []tea.Cmd{m.queuePathPR()}
	if !m.showPreview || !m.hasSelectedAgentWindow() {
		if len(m.rows) == 0 {
			m.previewID = 0
			m.preview = ""
			m.previewErr = nil
			m.previewBusy = false
		}
		return tea.Batch(commands...)
	}
	r := m.rows[m.cursor]
	windowID := m.entries[r.entryIndex].tabs[r.tabIndex].windows[r.windowIndex].id
	if windowID == m.previewID {
		return tea.Batch(commands...)
	}
	m.previewID = windowID
	m.preview = ""
	m.previewErr = nil
	m.previewBusy = true
	commands = append(commands, fetchPreview(m.kitty, windowID))
	return tea.Batch(commands...)
}

func fetchPreview(kitty string, windowID int) tea.Cmd {
	return func() tea.Msg {
		if kitty == "" {
			return previewMsg{windowID: windowID, err: fmt.Errorf("kitty was not found")}
		}
		output, err := exec.Command(
			kitty, "@", "get-text", "--match", "id:"+strconv.Itoa(windowID), "--extent", "screen", "--ansi",
		).CombinedOutput()
		content := cleanPreview(string(output))
		if err != nil {
			message := strings.TrimSpace(ansiPattern.ReplaceAllString(content, ""))
			if message != "" {
				err = fmt.Errorf("%s: %s", err, message)
			}
		}
		return previewMsg{windowID: windowID, content: content, err: err}
	}
}

func cleanPreview(content string) string {
	content = backgroundSGR.ReplaceAllString(content, "")
	lines := strings.Split(content, "\n")
	for len(lines) > 0 && strings.TrimSpace(ansiPattern.ReplaceAllString(lines[len(lines)-1], "")) == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}

func (m *model) rebuildAgentRows() {
	rows := make([]row, 0)
	searchValues := make([]string, 0)
	seen := map[int]bool{}
	for entryIndex := range m.entries {
		e := m.entries[entryIndex]
		for tabIndex := range e.tabs {
			tab := e.tabs[tabIndex]
			for windowIndex := range tab.windows {
				window := tab.windows[windowIndex]
				if window.agent == "" || seen[window.id] {
					continue
				}
				seen[window.id] = true
				rows = append(rows, row{entryIndex: entryIndex, tabIndex: tabIndex, windowIndex: windowIndex})
				searchValues = append(searchValues, strings.Join([]string{
					window.agent, e.name, e.originalName, e.detail, tab.title, window.title, window.command, window.detail,
				}, " "))
			}
		}
	}
	if m.query != "" {
		matches := fuzzy.Find(m.query, searchValues)
		ranked := make([]row, 0, len(matches))
		for _, match := range matches {
			ranked = append(ranked, rows[match.Index])
		}
		rows = ranked
	} else {
		sort.SliceStable(rows, func(i, j int) bool {
			a := m.entries[rows[i].entryIndex].tabs[rows[i].tabIndex].windows[rows[i].windowIndex]
			b := m.entries[rows[j].entryIndex].tabs[rows[j].tabIndex].windows[rows[j].windowIndex]
			return a.lastFocused > b.lastFocused
		})
	}
	m.rows = rows
	if m.cursor >= len(rows) {
		m.cursor = max(0, len(rows)-1)
	}
}

func (m *model) rebuildWorktreeRows() {
	focused := m.focusedWorktreePath()
	entryIndex := m.worktreeFilterEntryIndex
	if entryIndex < 0 || entryIndex >= len(m.entries) {
		m.worktreeFilterRows = []worktreeFilterRow{}
		m.rows = []row{}
		m.cursor = 0
		return
	}

	e := m.entries[entryIndex]
	rows := []worktreeFilterRow{}
	searchValues := []string{}

	// Get worktrees from entry level if available
	if e.worktreesLoaded {
		for _, wt := range e.worktrees {
			rows = append(rows, worktreeFilterRow{
				worktree:  wt,
				entryName: e.name,
				entryPath: e.path,
			})
			searchValues = append(searchValues, wt.branch+" "+wt.path+" "+wt.head)
		}
	} else {
		// Need to fetch worktrees - return early and let the fetch happen elsewhere
		return
	}

	if m.query != "" {
		matches := fuzzy.Find(m.query, searchValues)
		filtered := make([]worktreeFilterRow, 0, len(matches))
		for _, match := range matches {
			filtered = append(filtered, rows[match.Index])
		}
		rows = filtered
	} else {
		// Sort: current first, then by branch name
		sort.SliceStable(rows, func(i, j int) bool {
			a, b := rows[i].worktree, rows[j].worktree
			if a.current != b.current {
				return a.current // current worktree first
			}
			return worktreePriority(a) < worktreePriority(b)
		})
	}

	m.worktreeFilterRows = rows

	// Drop bulk selections for worktrees that no longer exist (removed or
	// pruned since selection) so the header count and bulk actions stay honest.
	if len(m.wtBulkSelected) > 0 {
		live := map[string]bool{}
		for _, wt := range m.entries[entryIndex].worktrees {
			live[wt.path] = true
		}
		for path := range m.wtBulkSelected {
			if !live[path] {
				delete(m.wtBulkSelected, path)
			}
		}
		if len(m.wtBulkSelected) == 0 {
			m.wtBulkSelected = nil
		}
	}

	// Build regular rows for cursor tracking. wt indexes worktreeFilterRows so
	// each rendered row maps to its own worktree, not the focused one.
	m.rows = make([]row, len(rows))
	for i := range rows {
		m.rows[i] = row{entryIndex: entryIndex, tabIndex: -1, windowIndex: -1, section: "wt-filter", wt: i}
	}
	if focused != "" {
		m.restoreFocusedWorktree(focused)
	} else if m.cursor >= len(m.rows) {
		m.cursor = max(0, len(m.rows)-1)
	}
}

// listPageSize mirrors the visible-row count computed in View() so jump/page
// keys move by roughly what is on screen.
func (m model) listPageSize() int {
	available := max(1, max(5, m.height-6)-3)
	return max(1, available/2)
}

func (m model) matchesFilter(e entry) bool {
	switch m.filter {
	case filterOpen:
		return e.open
	case filterProjects:
		return e.kind == "project"
	case filterSSH:
		return e.kind == "ssh"
	case filterSaved:
		return e.saved
	default:
		return true
	}
}

func (m model) View() string {
	outerWidth := max(40, m.width-4)
	workspaceWidth := min(140, outerWidth)
	_, hasSelectedWorktree := m.selectedWorktree()
	selectedPRURL, _ := m.selectedPullRequest()
	hasSelectedPR := selectedPRURL != ""
	compactDetail := workspaceWidth < 64 || m.height < 18
	showSideDetail := workspaceWidth >= 84 || m.height < 14
	bodyHeight := max(5, m.height-6)
	showAgentPreview := m.showPreview && m.hasSelectedAgentWindow() && bodyHeight >= 14
	listWidth, detailWidth := workspaceWidth, workspaceWidth
	listHeight, detailHeight := bodyHeight, bodyHeight
	if showAgentPreview && m.filter == filterAgents {
		// Agent browsing benefits from a compact chooser and a wide live screen.
		listWidth = max(38, min(48, workspaceWidth*35/100))
		detailWidth = workspaceWidth - listWidth - 2
		showSideDetail = true
	} else if showAgentPreview {
		// The hierarchy stays readable above a full-width terminal preview.
		listHeight = max(5, bodyHeight*40/100)
		detailHeight = max(6, bodyHeight-listHeight-1)
		showSideDetail = false
	} else if showSideDetail {
		detailWidth = max(20, min(42, workspaceWidth*28/100))
		listWidth = workspaceWidth - detailWidth - 2
		if listWidth < 18 {
			listWidth = 18
			detailWidth = max(12, workspaceWidth-listWidth-2)
		}
	} else {
		detailHeight = 8
		if compactDetail {
			detailHeight = 5
		}
		listHeight = max(5, bodyHeight-detailHeight-1)
	}

	tabs := []string{"All", "Agents", "Open", "Projects", "SSH", "Saved", "Worktrees"}
	for i := range tabs {
		if i == m.filter {
			tabs[i] = accentStyle.Render("[" + tabs[i] + "]")
		} else {
			tabs[i] = dimStyle.Render(" " + tabs[i] + " ")
		}
	}
	promptValue := dimStyle.Render("/ to search")
	if m.query != "" {
		promptValue = m.query
	}
	if m.mode == modeSearch {
		promptValue = accentStyle.Render(m.query+"█") + "  " + dimStyle.Render("SEARCH")
	}
	header := accentStyle.Render("Kesh") + "  " + strings.Join(tabs, " ")
	if m.filter == filterWorktrees && m.worktreeFilterEntryIndex >= 0 && m.worktreeFilterEntryIndex < len(m.entries) {
		entry := m.entries[m.worktreeFilterEntryIndex]
		name := truncate(entry.name, max(12, workspaceWidth-lipgloss.Width(header)-2))
		label := projectStyle.Render("󰉋 " + name)
		if owner, repo := m.getRepoOwner(entry.path); repo != "" && repo != filepath.Base(entry.path) {
			label += "  " + dimStyle.Render(owner+"/"+repo)
		}
		header += "  " + label
		if n := len(m.wtBulkSelected); n > 0 {
			header += "  " + accentStyle.Render(fmt.Sprintf("Selected (%d)", n))
		}
	}
	if len(m.selected) > 0 {
		names := make([]string, 0, len(m.selected))
		for _, entry := range m.entries {
			if m.selected[entry.key] {
				names = append(names, entry.name)
			}
		}
		summary := fmt.Sprintf("Selected (%d): %s", len(names), strings.Join(names, ", "))
		header += "  " + accentStyle.Render(truncate(summary, max(12, workspaceWidth-lipgloss.Width(header)-2)))
	}

	available := max(1, listHeight-3)
	start := 0
	if m.cursor >= available {
		start = m.cursor - available + 1
	}
	end := min(len(m.rows), start+available)
	listLines := []string{accentStyle.Render(fmt.Sprintf("List (%d)", len(m.rows)))}
	for i := start; i < end; i++ {
		row := m.rows[i]
		focused := i == m.cursor
		line := m.renderRow(row, max(8, listWidth-4), focused)
		if focused {
			if row.tabIndex < 0 && m.entries[row.entryIndex].open {
				line = accentStyle.Render("▌") + " " + line
			} else {
				line = accentStyle.Render("▌") + " " + focusStyle.Render(ansi.Strip(line))
			}
		} else {
			line = "  " + line
		}
		listLines = append(listLines, line)
	}
	if len(m.rows) == 0 {
		empty := "  No matching sessions"
		if m.filter == filterWorktrees {
			// The Worktree tab is empty when no project is selected (e.g. arrived
			// by cycling Tab). Tell the user how to populate it rather than
			// implying their search matched nothing.
			empty = "  No project selected — open a project and press w"
		}
		listLines = append(listLines, dimStyle.Render(empty))
	}
	listPanel := renderListPanel(listLines, listWidth, listHeight)
	detailPanel := m.detailPanelView(detailWidth, detailHeight, compactDetail)
	if showAgentPreview {
		detailPanel = m.previewView(detailWidth, detailHeight)
	}
	body := listPanel + "\n" + detailPanel
	if showSideDetail {
		body = lipgloss.JoinHorizontal(lipgloss.Top, listPanel, "  ", detailPanel)
	}

	footer := "j/k move  space select  n new  c clone  w worktrees  X remove merged  D delete closed  g PR  h/l expand  e expand  enter open  s/S save  p pin  r rename  x close  / search  tab filter  ctrl+d/u g/G  q quit"
	if showAgentPreview || (m.filter == filterAgents && m.hasSelectedAgentWindow()) {
		footer = "j/k move  enter focus  p preview  r rename  x close  / search  tab filter  q quit"
	} else if m.filter == filterWorktrees {
		footer = "j/k move  space select  enter focus  n create  p pull  r refresh  g PR  x remove  X merged  G end  esc back  / search  q quit"
	} else if hasSelectedWorktree {
		footer = "j/k move  enter open  g PR  x remove  X merged  D closed  q quit"
	} else if workspaceWidth < 100 {
		footer = "j/k move  enter open  h/l expand  x close  / search  q quit"
		if hasSelectedPR {
			footer = "j/k move  enter open  g PR  h/l expand  x close  / search  q quit"
		}
	}
	if workspaceWidth < 64 {
		footer = "j/k move  enter open  q quit"
		if hasSelectedPR {
			footer = "j/k move  enter open  g PR  q quit"
		}
	}
	if m.mode == modeSearch {
		footer = "type to filter  ctrl+j/k move  backspace delete  ctrl+u clear  enter/esc normal mode"
	}
	if m.saving {
		footer = "Saving workspace…"
	}
	if m.zoxidePending && m.mode != modeSearch {
		footer += "  · loading projects…"
	}
	if m.err != nil && m.mode != modeRename && m.mode != modeCreateSession && m.mode != modeClone && m.mode != modeSaveConfirm && m.mode != modePin && m.mode != modeCloseConfirm && m.mode != modeWorktreeCreate && !m.mergedWorktreeBusy && !m.worktreePullBusy {
		footer = errorStyle.Render("Error: " + m.err.Error())
	} else {
		footer = dimStyle.Render(footer)
	}

	content := strings.Join([]string{
		ansi.Truncate(header, workspaceWidth, "…"),
		ansi.Truncate(fmt.Sprintf("%-6s  %s", "Search", promptValue), workspaceWidth, "…"),
		strings.Repeat("─", workspaceWidth),
		body,
		ansi.Truncate(footer, workspaceWidth, "…"),
	}, "\n")
	if popup := m.popupView(workspaceWidth); popup != "" {
		content = strings.Join(overlayPopup(strings.Split(content, "\n"), popup, workspaceWidth), "\n")
	}
	content = lipgloss.PlaceHorizontal(outerWidth, lipgloss.Center, content)
	// Keep the alternate-screen frame at a stable height. Some detail values
	// wrap differently (notably the worktree summary), and an extra rendered
	// line makes the terminal scroll the entire view upward.
	if m.height > 3 {
		// The two vertical padding rows are outside this fixed content frame.
		frameHeight := m.height - 2
		lines := strings.Split(content, "\n")
		if len(lines) > frameHeight {
			lines = lines[:frameHeight]
		}
		for len(lines) < frameHeight {
			lines = append(lines, "")
		}
		content = strings.Join(lines, "\n")
	}
	return lipgloss.NewStyle().Padding(1, 2).Render(content)
}

func renderListPanel(lines []string, width, height int) string {
	panelWidth := max(12, width-2)
	contentHeight := max(1, height-2)
	if len(lines) > contentHeight {
		lines = lines[:contentHeight]
	}
	for len(lines) < contentHeight {
		lines = append(lines, "")
	}
	for index := range lines {
		lines[index] = ansi.Truncate(lines[index], panelWidth, "…")
	}
	return lipgloss.NewStyle().
		Width(panelWidth).
		Height(contentHeight).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("241")).
		Render(strings.Join(lines, "\n"))
}

func (m model) previewView(width, height int) string {
	content := m.preview
	switch {
	case m.previewBusy:
		content = dimStyle.Render("Loading preview…")
	case m.previewErr != nil:
		content = errorStyle.Render("Preview unavailable: " + m.previewErr.Error())
	case content == "":
		content = dimStyle.Render("No terminal content")
	}
	header := accentStyle.Render("Agent screen")
	body := lipgloss.NewStyle().Width(width).MaxWidth(width).MaxHeight(height - 2).Render(content)
	return lipgloss.NewStyle().Width(width).Height(height).Render(header + "\n" + strings.Repeat("─", width) + "\n" + body)
}

func middleTruncate(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(value) <= width {
		return value
	}
	if width <= 3 {
		return strings.Repeat(".", width)
	}
	runes := []rune(value)
	left := (width - 1) / 2
	right := width - 1 - left
	if left+right >= len(runes) {
		return value
	}
	return string(runes[:left]) + "…" + string(runes[len(runes)-right:])
}

type detailField struct {
	label  string
	value  string
	middle bool
}

// worktreeSyncBadge renders a worktree's divergence from its upstream and any
// uncommitted changes as a compact inline marker: ↑N ahead, ↓M behind, ✱ dirty.
func worktreeSyncBadge(worktree worktreeItem) string {
	dirtyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true)
	var segments []string
	if worktree.ahead > 0 {
		segments = append(segments, dimStyle.Render("↑"+strconv.Itoa(worktree.ahead)))
	}
	if worktree.behind > 0 {
		segments = append(segments, dimStyle.Render("↓"+strconv.Itoa(worktree.behind)))
	}
	if worktree.dirty {
		segments = append(segments, dirtyStyle.Render("✱"))
	}
	return strings.Join(segments, " ")
}

// worktreeSyncDetail renders a worktree's working-tree and upstream state for
// the detail panel (e.g. "clean · ↑2 ahead · ↓1 behind").
func worktreeSyncDetail(worktree worktreeItem) string {
	var parts []string
	if worktree.dirty {
		parts = append(parts, "uncommitted changes")
	} else {
		parts = append(parts, "clean")
	}
	if worktree.ahead > 0 {
		parts = append(parts, fmt.Sprintf("↑%d ahead", worktree.ahead))
	}
	if worktree.behind > 0 {
		parts = append(parts, fmt.Sprintf("↓%d behind", worktree.behind))
	}
	if worktree.ahead == 0 && worktree.behind == 0 {
		parts = append(parts, "up to date")
	}
	return strings.Join(parts, " · ")
}

func worktreePRSummary(worktree worktreeItem) string {
	if worktree.prNumber == 0 {
		return "—"
	}
	summary := strings.TrimSpace(prStatusIcon(worktree.prStatus) + " #" + strconv.Itoa(worktree.prNumber))
	if !worktree.prExact {
		summary += " · local HEAD differs"
	}
	return summary
}

func pathPRSummary(info pathPRInfo) string {
	pullRequest := info.PullRequest
	if pullRequest.Number == 0 {
		return ""
	}
	summary := strings.TrimSpace(prStatusIcon(pullRequest.Status) + " #" + strconv.Itoa(pullRequest.Number))
	if !info.Exact {
		summary += " · local HEAD differs"
	}
	return summary
}

func renderDetailPanel(title string, fields []detailField, action string, extra []string, width, height int, compact bool) string {
	panelWidth := max(12, width-2)
	contentHeight := max(1, height-2)
	valueWidth := max(4, panelWidth-8)
	lines := make([]string, 0, contentHeight)
	if !compact {
		lines = append(lines, accentStyle.Render(title))
	}
	fieldLimit := len(fields)
	if compact {
		fieldLimit = min(fieldLimit, 3)
	}
	for _, field := range fields[:fieldLimit] {
		value := field.value
		if field.label == "" {
			if compact {
				lines = append(lines, ansi.Truncate(value, panelWidth, "…"))
			} else {
				lines = append(lines, strings.Split(ansi.Wrap(value, panelWidth, " /_·"), "\n")...)
			}
			continue
		}
		if compact {
			parts := strings.Split(value, "\n")
			if len(parts) > 1 {
				value = fmt.Sprintf("%s (+%d more)", parts[0], len(parts)-1)
			}
			if field.middle {
				value = middleTruncate(value, valueWidth)
			} else {
				value = ansi.Truncate(value, valueWidth, "…")
			}
			lines = append(lines, mutedStyle.Render(fmt.Sprintf("%-8s", field.label))+value)
			continue
		}
		wrapped := strings.Split(ansi.Wrap(value, valueWidth, " /_·"), "\n")
		for index, line := range wrapped {
			label := strings.Repeat(" ", 8)
			if index == 0 {
				label = fmt.Sprintf("%-8s", field.label)
			}
			lines = append(lines, mutedStyle.Render(label)+line)
		}
	}
	if !compact && action != "" {
		lines = append(lines, "", dimStyle.Render(action))
	}
	if !compact && len(extra) > 0 {
		if len(lines)+2 < contentHeight {
			lines = append(lines, "")
		}
		lines = append(lines, accentStyle.Render("Screen"))
		for _, line := range extra {
			lines = append(lines, ansi.Truncate(line, max(8, panelWidth-2), "…"))
		}
	}
	if len(lines) > contentHeight {
		lines = lines[:contentHeight]
	}
	for len(lines) < contentHeight {
		lines = append(lines, "")
	}
	return lipgloss.NewStyle().
		Width(panelWidth).
		Height(contentHeight).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("241")).
		Render(strings.Join(lines, "\n"))
}

func worktreeInfoView(worktree worktreeItem, width int, compact bool) string {
	height := 8
	if compact {
		height = 5
	}
	action := "No matching pull request"
	if worktree.prURL != "" {
		action = "g Open PR"
	}
	return renderDetailPanel("Worktree", []detailField{
		{label: "Branch", value: worktree.branch, middle: true},
		{label: "Path", value: displayPath(worktree.path, os.Getenv("HOME")), middle: true},
		{label: "PR", value: worktreePRSummary(worktree)},
	}, action, nil, width, height, compact)
}

func entryDirectoryField(entry entry) detailField {
	seen := map[string]bool{}
	directories := make([]string, 0)
	for _, tab := range entry.tabs {
		for _, window := range tab.windows {
			if window.cwd == "" {
				continue
			}
			directory := displayPath(window.cwd, os.Getenv("HOME"))
			if !seen[directory] {
				seen[directory] = true
				directories = append(directories, directory)
			}
		}
	}
	if len(directories) == 0 {
		directory := entry.path
		if directory == "" {
			directory = entry.detail
		} else {
			directory = displayPath(directory, os.Getenv("HOME"))
		}
		return detailField{label: "Path", value: directory, middle: true}
	}
	if len(directories) == 1 {
		return detailField{label: "Path", value: directories[0], middle: true}
	}
	visible := directories
	if len(visible) > 3 {
		visible = append([]string{}, visible[:3]...)
		visible = append(visible, fmt.Sprintf("…and %d more", len(directories)-3))
	}
	return detailField{label: "Paths", value: strings.Join(visible, "\n")}
}

func (m model) detailPanelView(width, height int, compact bool) string {
	if len(m.rows) == 0 || m.cursor < 0 || m.cursor >= len(m.rows) {
		return renderDetailPanel("Info", []detailField{{value: "No matching rows — change or clear the filter."}}, "", nil, width, height, compact)
	}
	selected := m.rows[m.cursor]
	entry := m.entries[selected.entryIndex]
	if selected.section == "wt-item" {
		worktrees := m.worktreesForRow(selected)
		if selected.wt >= 0 && selected.wt < len(worktrees) {
			worktree := worktrees[selected.wt]
			action := "No matching pull request"
			if worktree.prURL != "" {
				action = "g Open PR"
			}
			return renderDetailPanel("Worktree", []detailField{
				{label: "Branch", value: worktree.branch, middle: true},
				{label: "Path", value: displayPath(worktree.path, os.Getenv("HOME")), middle: true},
				{label: "PR", value: worktreePRSummary(worktree)},
			}, action, nil, width, height, compact)
		}
	}
	if selected.section == "wt-head" {
		worktrees := m.worktreesForRow(selected)
		return renderDetailPanel("Worktrees", []detailField{
			{label: "Project", value: entry.name},
			{label: "Path", value: displayPath(m.worktreeDirectory(selected), os.Getenv("HOME")), middle: true},
			{label: "Count", value: strconv.Itoa(len(worktrees))},
		}, "Select a branch for actions", nil, width, height, compact)
	}
	if selected.section == "wt-filter" && m.filter == filterWorktrees {
		if m.cursor < 0 || m.cursor >= len(m.worktreeFilterRows) {
			return renderDetailPanel("Worktree", []detailField{{value: "No worktrees"}}, "", nil, width, height, compact)
		}
		wtRow := m.worktreeFilterRows[m.cursor]
		wt := wtRow.worktree

		action := "No matching pull request"
		if wt.prURL != "" {
			action = "g Open PR"
		}

		fields := []detailField{
			{label: "Branch", value: wt.branch, middle: true},
			{label: "Path", value: displayPath(wt.path, os.Getenv("HOME")), middle: true},
			{label: "PR", value: worktreePRSummary(wt)},
			{label: "Commit", value: wt.head},
			{label: "Sync", value: worktreeSyncDetail(wt)},
		}
		if wt.dirty && len(wt.changes) > 0 {
			shown := wt.changes
			if len(shown) > 6 {
				shown = append(append([]string{}, shown[:6]...), fmt.Sprintf("…and %d more", len(wt.changes)-6))
			}
			fields = append(fields, detailField{label: "Changes", value: strings.Join(shown, "\n")})
		}

		if wt.current {
			fields = append([]detailField{{label: "Status", value: accentStyle.Render("★ Current worktree")}}, fields...)
		}

		return renderDetailPanel("Worktree", fields, action, nil, width, height, compact)
	}
	if selected.windowIndex >= 0 {
		window := entry.tabs[selected.tabIndex].windows[selected.windowIndex]
		fields := []detailField{
			{label: "Name", value: window.title},
			{label: "Project", value: entry.name},
			{label: "Path", value: displayPath(window.cwd, os.Getenv("HOME")), middle: true},
		}
		if window.pathPR.PullRequest.Number > 0 {
			fields = []detailField{
				{label: "Name", value: window.title},
				{label: "Path", value: displayPath(window.cwd, os.Getenv("HOME")), middle: true},
				{label: "PR", value: pathPRSummary(window.pathPR)},
				{label: "Branch", value: window.pathPR.Branch, middle: true},
			}
		}
		if m.filter == filterAgents {
			fields = []detailField{
				{label: "Agent", value: window.agent},
				{label: "Project", value: entry.name},
				{label: "Last active", value: relativeLastActive(window.lastFocused, m.lastFocusedReference())},
				{label: "Path", value: displayPath(window.cwd, os.Getenv("HOME")), middle: true},
			}
			if window.pathPR.PullRequest.Number > 0 {
				fields[2] = detailField{label: "PR", value: pathPRSummary(window.pathPR)}
			}
		} else {
			if window.command != "" {
				fields = append(fields, detailField{label: "Command", value: window.command})
			}
			if window.agent != "" {
				fields = append(fields, detailField{label: "Agent", value: window.agent})
			}
		}
		var screen []string
		title := "Window"
		if m.filter == filterAgents {
			title = "Agent screen"
		}
		if m.filter == filterAgents && m.showPreview {
			screen = strings.Split(m.preview, "\n")
			if m.previewBusy {
				screen = []string{"Loading preview…"}
			} else if m.previewErr != nil {
				screen = []string{"Preview unavailable: " + m.previewErr.Error()}
			} else if m.preview == "" {
				screen = []string{"No terminal content"}
			}
		}
		return renderDetailPanel(title, fields, "", screen, width, height, compact)
	}
	if selected.tabIndex >= 0 {
		tab := entry.tabs[selected.tabIndex]
		fields := []detailField{
			{label: "Name", value: tab.title},
			{label: "Project", value: entry.name},
			{label: "Windows", value: strconv.Itoa(len(tab.windows))},
		}
		for _, window := range tab.windows {
			if window.pathPR.PullRequest.Number > 0 {
				fields = append(fields, detailField{label: "PR", value: pathPRSummary(window.pathPR)})
				break
			}
		}
		return renderDetailPanel("Tab", fields, "Enter focus · r rename", nil, width, height, compact)
	}
	directoryField := entryDirectoryField(entry)
	title := "Project"
	if entry.kind == "workspace" {
		title = "Workspace"
	} else if entry.kind == "ssh" {
		title = "SSH"
	}
	fields := []detailField{
		{label: "Name", value: entry.name},
		directoryField,
	}
	if entry.pathPR.PullRequest.Number > 0 {
		fields = []detailField{
			{label: "Name", value: entry.name},
			directoryField,
			{label: "PR", value: pathPRSummary(entry.pathPR)},
			{label: "Branch", value: entry.pathPR.Branch, middle: true},
		}
	}
	return renderDetailPanel(title, fields, "Enter open · w worktrees", nil, width, height, compact)
}

// prCheckoutPreview renders the dim summary block under the PR input. Once gh
// resolves the PR head, it shows the exact worktree path that checkout uses. A
// bare PR number has no owner/repo to resolve until its selected project is
// inspected, so only that project is noted.
func prCheckoutPreview(value, branch, selectedRepoPath, checkoutRoot, cloneRoot, worktreeRoot string, fieldWidth int) string {
	owner, repo, _, useSelected, err := parsePullRequestInput(value)
	if err != nil {
		return ""
	}
	var lines []string
	if useSelected || owner == "" {
		if selectedRepoPath == "" {
			lines = append(lines, "Root repo path: select a project")
		} else {
			lines = append(lines, "Root repo path: "+displayPath(selectedRepoPath, os.Getenv("HOME")))
		}
		return renderPreviewLines(lines, fieldWidth)
	}
	repoPath := filepath.Join(checkoutRoot, owner, repo)
	newClone := !dirExists(repoPath)
	if newClone {
		repoPath = filepath.Join(cloneRoot, owner, repo)
	}
	rootNote := ""
	if newClone {
		rootNote = " (new clone)"
	}
	lines = append(lines, "Root repo path: "+displayPath(repoPath, os.Getenv("HOME"))+rootNote)
	// Worktrees land under <worktreeRoot>/<owner>/<repo>/<branch>; fall back to
	// the clone root when the worktree root is unconfigured so the path is still
	// informative.
	root := worktreeRoot
	if root == "" {
		root = cloneRoot
	}
	if branch == "" {
		lines = append(lines, "Worktree path: resolving PRbranch…")
		return renderPreviewLines(lines, fieldWidth)
	}
	worktreePath := displayPath(filepath.Join(root, owner, repo, worktreeDirectoryName(branch)), os.Getenv("HOME"))
	lines = append(lines, "Worktree path: "+worktreePath+rootNote)
	return renderPreviewLines(lines, fieldWidth)
}

func wktreeSessionPreview(recipe *wktreeRecipe, repoPath, branch string) string {
	template := recipe.Terminal.SessionName
	if template == "" {
		template = "${repo}/${branch}"
	}
	repo := filepath.Base(repoPath)
	branch = strings.NewReplacer("/", "-", " ", "-").Replace(branch)
	return strings.ReplaceAll(strings.ReplaceAll(template, "${repo}", repo), "${branch}", branch)
}

func wktreePaneLabel(pane struct {
	Command    string   `yaml:"command"`
	Commands   []string `yaml:"commands"`
	Split      string   `yaml:"split"`
	Focus      bool     `yaml:"focus"`
	Percentage int      `yaml:"percentage"`
}) string {
	label := pane.Command
	if label == "" && len(pane.Commands) > 0 {
		label = strings.Join(pane.Commands, " && ")
	}
	if label == "" {
		label = "shell"
	}
	if pane.Focus {
		label += " *"
	}
	return truncate(label, 26)
}

type wktreePaneNode struct {
	label         string
	vertical      bool
	percentage    int
	first, second *wktreePaneNode
}

// wktreePaneDiagram simulates wktree's successive Kitty splits in a small
// terminal-cell canvas. It is intentionally a preview: Kitty makes final pixel
// sizing decisions, but the pane relationships and configured bias are exact.
func wktreePaneDiagram(panes []struct {
	Command    string   `yaml:"command"`
	Commands   []string `yaml:"commands"`
	Split      string   `yaml:"split"`
	Focus      bool     `yaml:"focus"`
	Percentage int      `yaml:"percentage"`
}, width int) []string {
	if len(panes) == 0 {
		panes = append(panes, struct {
			Command    string   `yaml:"command"`
			Commands   []string `yaml:"commands"`
			Split      string   `yaml:"split"`
			Focus      bool     `yaml:"focus"`
			Percentage int      `yaml:"percentage"`
		}{Command: "shell"})
	}
	root := &wktreePaneNode{label: wktreePaneLabel(panes[0])}
	active := root
	for _, pane := range panes[1:] {
		old := *active
		next := &wktreePaneNode{label: wktreePaneLabel(pane)}
		*active = wktreePaneNode{vertical: pane.Split == "vertical", percentage: pane.Percentage, first: &old, second: next}
		active = next
	}
	canvasWidth := min(54, max(20, width-4))
	canvasHeight := 7
	canvas := make([][]rune, canvasHeight)
	for y := range canvas {
		canvas[y] = []rune(strings.Repeat(" ", canvasWidth))
	}
	put := func(x, y int, value string) {
		if y >= 0 && y < canvasHeight {
			for i, r := range []rune(value) {
				if x+i >= 0 && x+i < canvasWidth {
					canvas[y][x+i] = r
				}
			}
		}
	}
	var draw func(*wktreePaneNode, int, int, int, int)
	draw = func(node *wktreePaneNode, x, y, w, h int) {
		if w < 4 || h < 3 {
			return
		}
		put(x, y, "┌"+strings.Repeat("─", w-2)+"┐")
		put(x, y+h-1, "└"+strings.Repeat("─", w-2)+"┘")
		for row := y + 1; row < y+h-1; row++ {
			put(x, row, "│")
			put(x+w-1, row, "│")
		}
		if node.first == nil {
			put(x+1, y+1, truncate(node.label, w-3))
			return
		}
		percent := node.percentage
		if percent <= 0 {
			percent = 50
		}
		if percent < 25 {
			percent = 25
		}
		if percent > 75 {
			percent = 75
		}
		if node.vertical {
			split := max(3, h*percent/100)
			draw(node.first, x, y, w, split)
			draw(node.second, x, y+split-1, w, h-split+1)
		} else {
			split := max(4, w*percent/100)
			draw(node.first, x, y, split, h)
			draw(node.second, x+split-1, y, w-split+1, h)
		}
	}
	draw(root, 0, 0, canvasWidth, canvasHeight)
	lines := make([]string, canvasHeight)
	for i := range canvas {
		lines[i] = strings.TrimRight(string(canvas[i]), " ")
	}
	return lines
}

func wktreeWorkspaceRepoPath(workspace struct {
	Name  string `yaml:"name"`
	Repo  string `yaml:"repo"`
	Panes []struct {
		Command    string   `yaml:"command"`
		Commands   []string `yaml:"commands"`
		Split      string   `yaml:"split"`
		Focus      bool     `yaml:"focus"`
		Percentage int      `yaml:"percentage"`
	} `yaml:"panes"`
}, recipePath string) string {
	repo := workspace.Repo
	if repo == "" {
		repo = "."
	}
	if expanded, err := expandHomePath(repo); err == nil {
		repo = expanded
	}
	if !filepath.IsAbs(repo) {
		repo = filepath.Join(filepath.Dir(recipePath), repo)
	}
	return displayPath(filepath.Clean(repo), os.Getenv("HOME"))
}

func wktreeLayoutPreview(recipe *wktreeRecipe, recipePath, mode string, width int, selected []bool) []string {
	all := recipe.Workspaces
	var indices []int
	switch mode {
	case "single":
		if len(all) > 0 {
			indices = []int{0}
		}
	case "selected":
		for i := range all {
			if i < len(selected) && selected[i] {
				indices = append(indices, i)
			}
		}
	default: // "all" and others show every workspace
		for i := range all {
			indices = append(indices, i)
		}
	}
	var lines []string
	for display, i := range indices {
		workspace := all[i]
		connector := "├─"
		if display == len(indices)-1 {
			connector = "└─"
		}
		lines = append(lines, connector+" "+workspace.Name+"  "+wktreeWorkspaceRepoPath(workspace, recipePath))
		for _, line := range wktreePaneDiagram(workspace.Panes, width) {
			lines = append(lines, "   "+line)
		}
	}
	return lines
}

// renderWorktreeChecklist renders the workspace selection shared by the
// read-only template preview and the editable Workspaces mode.
func (m *model) renderWorktreeChecklist(selected []bool, interactive bool) string {
	var rows []string
	for i, workspace := range m.worktreeRecipe.Workspaces {
		mark := "☐"
		if i < len(selected) && selected[i] {
			mark = "☑"
		}
		label := mark + " " + workspace.Name + "  " + wktreeWorkspaceRepoPath(workspace, m.worktreeRecipePath)
		if interactive && i == m.worktreeWorkspaceCursor {
			rows = append(rows, focusStyle.Render("› "+label))
		} else {
			rows = append(rows, dimStyle.Render("  "+label))
		}
	}
	return strings.Join(rows, "\n") + "\n"
}

func (m model) worktreePreviewSelection() []bool {
	if m.worktreeCustomWorkspaces || m.worktreeRecipeMode == "selected" {
		return m.worktreeSelected
	}
	selected := make([]bool, len(m.worktreeRecipe.Workspaces))
	if m.worktreeRecipeMode == "single" && len(selected) > 0 {
		selected[0] = true
	} else if m.worktreeRecipeMode == "all" {
		for i := range selected {
			selected[i] = true
		}
	}
	return selected
}

func renderPreviewLines(lines []string, fieldWidth int) string {
	wrapped := lipgloss.NewStyle().Width(fieldWidth).Render(strings.Join(lines, "\n"))
	return "\n\n" + dimStyle.Render(wrapped)
}

// worktreeModeMenuView makes the available creation layouts visible while the
// worktree form is open. Tab still cycles these choices so branch entry remains
// the primary keyboard focus.
func (m model) worktreeModeMenuView(width int) string {
	active := "template"
	if m.worktreeRecipeMode == "none" {
		active = "native"
	} else if m.worktreeCustomWorkspaces {
		active = "workspaces"
	}
	modes := []struct {
		value string
		label string
	}{
		{"native", "Simple"},
		{"template", "Template"},
	}
	if len(m.worktreeRecipe.Workspaces) > 1 {
		modes = append(modes, struct {
			value string
			label string
		}{"workspaces", "Workspaces"})
	}

	lines := []string{dimStyle.Render("MODE")}
	for _, mode := range modes {
		line := "  " + mode.label
		if mode.value == active {
			line = focusStyle.Render("› " + mode.label)
		} else {
			line = dimStyle.Render(line)
		}
		lines = append(lines, ansi.Truncate(line, width, "…"))
	}
	return lipgloss.NewStyle().Width(width).Render(strings.Join(lines, "\n"))
}

func (m model) popupView(width int) string {
	if m.mode != modeRename && m.mode != modeCreateSession && m.mode != modeClone && m.mode != modeSaveConfirm && m.mode != modePin && m.mode != modeCloseConfirm && m.mode != modeWorktreeCreate && m.mode != modeCheckoutPR && !m.mergedWorktreeBusy && !m.worktreePullBusy {
		return ""
	}
	popupWidth := min(50, max(28, width-10))
	if m.mode == modeClone || m.saveForeground || m.mode == modeWorktreeCreate || (m.mode == modeCloseConfirm && m.closeRow.section == "wt-item") {
		popupWidth = min(72, max(36, width-6))
	}
	// PR URLs and worktree paths are often long. Let this form use the available
	// terminal width instead of leaving an artificial empty right-hand column.
	if m.mode == modeCheckoutPR {
		popupWidth = min(100, max(36, width-6))
	}
	var title, field, help string
	if m.worktreePullBusy {
		title = "Syncing worktree"
		field = "Running git pull --rebase…"
		help = "Updates the worktree's branch from its upstream"
	} else if m.mergedWorktreeBusy {
		title = "Checking merged worktrees"
		field = "Querying GitHub for current PR status…"
		help = "This always uses live data before removal"
	} else if m.mode == modeSaveConfirm {
		entry := m.entries[m.saveEntry]
		if m.saveForeground {
			title = "Save with running commands"
			lines := []string{fmt.Sprintf("Save %q and rerun foreground commands when restored?", entry.name)}
			commands := workspaceForegroundCommands(entry)
			if len(commands) == 0 {
				lines = append(lines, "", dimStyle.Render("No non-shell foreground commands detected."))
			} else {
				lines = append(lines, "", "Restoring will rerun:")
				for index, command := range commands {
					if index == 4 {
						lines = append(lines, fmt.Sprintf("  • …and %d more", len(commands)-index))
						break
					}
					lines = append(lines, "  • "+truncate(command, popupWidth-12))
				}
			}
			field = lipgloss.NewStyle().Width(popupWidth - 6).Render(strings.Join(lines, "\n"))
		} else if entry.saved {
			title = "Update saved workspace"
			field = lipgloss.NewStyle().Width(popupWidth - 6).Render(fmt.Sprintf("Update the saved snapshot for %q?", entry.name))
		} else {
			title = "Save workspace"
			field = lipgloss.NewStyle().Width(popupWidth - 6).Render(fmt.Sprintf("Save %q for later restoration?", entry.name))
		}
		help = "Press y to confirm  •  Esc cancel"
	} else if m.mode == modeClone {
		title = "Clone repository"
		repositoryCursor := ""
		destinationCursor := ""
		if !m.cloneBusy {
			if m.cloneDestinationFocus {
				destinationCursor = "█"
			} else {
				repositoryCursor = "█"
			}
		}
		repositoryValueStyle := focusStyle
		destinationValueStyle := focusStyle
		if !m.cloneBusy && !m.cloneDestinationFocus {
			repositoryValueStyle = selectedTextStyle
		}
		if !m.cloneBusy && m.cloneDestinationFocus {
			destinationValueStyle = selectedTextStyle
		}
		fieldWidth := popupWidth - 6
		repositoryField := lipgloss.NewStyle().Width(fieldWidth).Render(
			dimStyle.Render("Repository: ") + repositoryValueStyle.Render(m.cloneRepository+repositoryCursor),
		)
		destinationField := lipgloss.NewStyle().Width(fieldWidth).Render(
			dimStyle.Render("Clone into: ") + destinationValueStyle.Render(m.cloneDestination+destinationCursor),
		)
		field = repositoryField + "\n\n" + destinationField
		if m.cloneBusy {
			help = "Cloning…"
		} else {
			help = "Tab switch field  •  Enter clone  •  Esc cancel"
		}
	} else if m.mode == modeCreateSession {
		title = fmt.Sprintf("Create session (%d tabs)", len(m.selected))
		field = selectedStyle.Width(popupWidth - 6).Render(m.createValue + "█")
		help = "Enter create  •  Esc cancel"
	} else if m.mode == modeRename {
		title = "Rename"
		field = selectedStyle.Width(popupWidth - 6).Render(m.renameValue + "█")
		help = "Enter save  •  Esc cancel"
	} else if m.mode == modeWorktreeCreate {
		title = "Create worktree"
		cursor := "█"
		if m.worktreeBranch != "" && !m.worktreeBusy {
			cursor = ""
		}
		branchField := dimStyle.Render("Branch: ") + focusStyle.Render(m.worktreeBranch+cursor)
		fieldWidth := popupWidth - 6

		var pathsField string
		if m.worktreeRecipe != nil {
			menuWidth := 18
			previewWidth := fieldWidth - menuWidth - 2
			if previewWidth < 18 {
				menuWidth = 14
				previewWidth = fieldWidth - menuWidth - 2
			}
			repoPath := ""
			if entries := m.worktreeEntries(); len(entries) == 1 {
				repoPath = entries[0].path
			}
			preview := ""
			if m.worktreeRecipeMode == "none" {
				preview = dimStyle.Render("Creates a simple Kesh worktree.")
			} else {
				preview = dimStyle.Render("Template: "+displayPath(m.worktreeRecipePath, os.Getenv("HOME"))) + "\n" +
					dimStyle.Render("Layout: "+wktreeSessionPreview(m.worktreeRecipe, repoPath, m.worktreeBranch)) + "\n"
				selected := m.worktreePreviewSelection()
				preview += m.renderWorktreeChecklist(selected, m.worktreeCustomWorkspaces)
				layout := wktreeLayoutPreview(m.worktreeRecipe, m.worktreeRecipePath, m.worktreeRecipeMode, previewWidth, selected)
				preview += "\n" + dimStyle.Render("Layout preview") + "\n" + dimStyle.Render(strings.Join(layout, "\n"))
			}
			pathsField = "\n\n" + lipgloss.JoinHorizontal(lipgloss.Top,
				m.worktreeModeMenuView(menuWidth), "  ",
				lipgloss.NewStyle().Width(previewWidth).Render(preview),
			)
		} else if len(m.worktreePaths) > 0 {
			if m.worktreeRecipe != nil {
				pathsField = "\n\n" + dimStyle.Render("Mode: none (native Kesh worktree)")
			}
			label := "Preview"
			if len(m.worktreePaths) > 1 {
				label = fmt.Sprintf("Preview (%d)", len(m.worktreePaths))
			}
			pathsField = "\n\n" + dimStyle.Render(label+":") + "\n"
			prefix := "  󰉋 "
			hanging := strings.Repeat(" ", lipgloss.Width(prefix))
			wrapWidth := fieldWidth - lipgloss.Width(prefix)
			for i, path := range m.worktreePaths {
				if i >= 3 {
					pathsField += "\n  " + dimStyle.Render(fmt.Sprintf("…and %d more", len(m.worktreePaths)-i))
					break
				}
				wrapped := lipgloss.NewStyle().Width(wrapWidth).Render(path)
				wrapped = strings.ReplaceAll(wrapped, "\n", "\n"+hanging)
				pathsField += "\n" + mutedStyle.Render(prefix+wrapped)
			}
		}

		field = lipgloss.NewStyle().Width(fieldWidth).Render(branchField + pathsField)
		if m.worktreeBusy {
			help = "Creating…"
		} else if m.worktreeRecipe != nil && m.worktreeCustomWorkspaces {
			help = "↑↓/Ctrl+J/K choose workspace  •  space toggle  •  Tab/Shift+Tab mode  •  Enter create  •  Esc cancel"
		} else if m.worktreeRecipe != nil {
			help = "Tab/Shift+Tab mode  •  Enter create  •  Esc cancel"
		} else {
			help = "Enter create  •  Esc cancel"
		}
	} else if m.mode == modeCheckoutPR {
		title = "Checkout pull request"
		fieldWidth := popupWidth - 6
		cursor := "█"
		if m.prCheckoutBusy {
			cursor = ""
		}
		inputField := lipgloss.NewStyle().Width(fieldWidth).Render(
			dimStyle.Render("PR: ") + focusStyle.Render(m.prCheckoutValue+cursor),
		)
		previewField := ""
		if !m.prCheckoutBusy {
			selectedRepoPath := ""
			if m.cursor >= 0 && m.cursor < len(m.rows) {
				entryIndex := m.rows[m.cursor].entryIndex
				if entryIndex >= 0 && entryIndex < len(m.entries) {
					selectedRepoPath = m.entries[entryIndex].path
				}
			}
			previewField = prCheckoutPreview(m.prCheckoutValue, m.prCheckoutBranch, selectedRepoPath, m.checkoutRoot, m.cloneRoot, m.worktreeRoot, fieldWidth)
		}
		field = inputField + previewField
		if m.prCheckoutBusy {
			help = "Fetching…"
		} else {
			help = "Enter checkout  •  Esc cancel"
		}
	} else if m.mode == modePin {
		title = "Pin " + m.entries[m.pinEntry].name
		slot := "█"
		if m.confirmSlot != "" {
			slot = m.confirmSlot
		}
		field = selectedStyle.Width(popupWidth - 6).Render("Slot: " + slot)
		if m.confirmSlot != "" {
			help = "Press " + m.confirmSlot + " again to replace  •  Esc cancel"
		} else {
			help = "0–9 assign  •  x unpin  •  Esc cancel"
		}
	} else {
		title = "Close"
		if m.destroyPlan != nil {
			title = "Destroy"
		} else if len(m.mergedWorktrees) > 0 {
			title = "Remove merged worktrees"
		} else if m.closeRow.section == "wt-bulk" {
			title = fmt.Sprintf("Remove %d worktree%s", len(m.bulkWorktrees), plural(len(m.bulkWorktrees)))
		}
		field = lipgloss.NewStyle().Width(popupWidth - 6).Render(m.closePrompt())
		switch {
		case m.closeBusy:
			help = "Removing…"
		case m.worktreeForcePrompt:
			help = "Press f to force  •  Esc cancel"
		case m.destroyPlan != nil:
			help = "y destroy  •  Esc cancel"
		default:
			if m.closeRow.section == "wt-item" {
				help = "y remove  •  f force  •  Esc cancel"
			} else {
				help = "Press y to confirm  •  Esc cancel"
			}
		}
	}
	body := accentStyle.Render(title) + "\n\n" + field + "\n\n" + dimStyle.Render(help)
	if m.err != nil {
		body += "\n" + errorStyle.Render(m.err.Error())
	}
	return lipgloss.NewStyle().
		Width(popupWidth).
		Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("205")).
		Render(body)
}

// destroyPrompt renders the layer list for a unified Destroy confirmation,
// showing only the layers that apply to the plan.
func destroyPrompt(plan destroyPlan) string {
	lines := []string{fmt.Sprintf("Destroy %q?", plan.entryName)}
	if plan.closeSession {
		lines = append(lines, "  • Close kitty session ("+strconv.Itoa(plan.tabCount)+" tab"+plural(plan.tabCount)+")")
	}
	if plan.worktreePath != "" {
		lines = append(lines, "  • Remove worktree  "+displayPath(plan.worktreePath, os.Getenv("HOME")))
	}
	if plan.branch != "" {
		lines = append(lines, "  • Delete branch  "+plan.branch)
	}
	if plan.saved {
		lines = append(lines, "  • Delete saved record")
	}
	return strings.Join(lines, "\n")
}

func (m model) closePrompt() string {
	selected := m.closeRow
	entry := m.entries[selected.entryIndex]
	if m.destroyPlan != nil {
		return destroyPrompt(*m.destroyPlan)
	}
	if len(m.mergedWorktrees) > 0 {
		lines := []string{fmt.Sprintf("Delete %d merged worktree%s?", len(m.mergedWorktrees), plural(len(m.mergedWorktrees)))}
		for i, worktree := range m.mergedWorktrees {
			if i == 4 {
				lines = append(lines, fmt.Sprintf("  …and %d more", len(m.mergedWorktrees)-i))
				break
			}
			lines = append(lines, "  "+worktree.branch)
		}
		return strings.Join(lines, "\n")
	}
	if selected.section == "wt-bulk" {
		targets := m.bulkWorktrees
		lines := []string{fmt.Sprintf("Delete %d worktree%s?", len(targets), plural(len(targets)))}
		for i, worktree := range targets {
			if i == 4 {
				lines = append(lines, fmt.Sprintf("  …and %d more", len(targets)-i))
				break
			}
			lines = append(lines, "  "+worktree.branch)
		}
		return strings.Join(lines, "\n")
	}
	if selected.section == "wt-item" {
		wt, ok := m.worktreeForRow(selected)
		if !ok {
			return "Worktree is no longer available"
		}
		prefix := "Delete"
		if m.worktreeForcePrompt {
			prefix = "Force-delete"
		}
		return fmt.Sprintf("%s worktree?\n\nBranch: %s\nPath:   %s", prefix, wt.branch, displayPath(wt.path, os.Getenv("HOME")))
	}
	if entry.saved && !entry.open {
		return fmt.Sprintf("Delete saved workspace %q?", entry.name)
	}
	if selected.windowIndex >= 0 {
		window := entry.tabs[selected.tabIndex].windows[selected.windowIndex]
		return fmt.Sprintf("Close window %q?", window.title)
	}
	if selected.tabIndex >= 0 {
		tab := entry.tabs[selected.tabIndex]
		return fmt.Sprintf("Close tab %q and its %d window%s?", tab.title, len(tab.windows), plural(len(tab.windows)))
	}
	return fmt.Sprintf("Close workspace %q and its %d tab%s?", entry.name, len(entry.tabs), plural(len(entry.tabs)))
}

func overlayPopup(lines []string, popup string, width int) []string {
	popupLines := strings.Split(popup, "\n")
	start := max(3, (len(lines)-len(popupLines))/2)
	if start+len(popupLines) > len(lines) {
		start = max(3, len(lines)-len(popupLines))
	}
	for index, popupLine := range popupLines {
		lineIndex := start + index
		if lineIndex >= len(lines) {
			break
		}
		popupWidth := min(width, lipgloss.Width(popupLine))
		left := max(0, (width-popupWidth)/2)
		right := left + popupWidth
		background := ansi.Truncate(lines[lineIndex], width, "")
		if padding := width - lipgloss.Width(background); padding > 0 {
			background += strings.Repeat(" ", padding)
		}
		lines[lineIndex] = ansi.Cut(background, 0, left) + ansi.Truncate(popupLine, popupWidth, "") + ansi.Cut(background, right, width)
	}
	return lines
}

func (m model) renderRow(r row, width int, focused bool) string {
	e := m.entries[r.entryIndex]
	switch r.section {
	case "wt-head":
		worktrees := m.worktreesForRow(r)
		label := "worktrees"
		if len(worktrees) != 1 {
			label = fmt.Sprintf("worktrees (%d)", len(worktrees))
		}
		indent := "            "
		if r.windowIndex >= 0 {
			indent = "               "
		}
		return indent + dimStyle.Render("└─ "+label)
	case "wt-item":
		worktrees := m.worktreesForRow(r)
		if r.wt < 0 || r.wt >= len(worktrees) {
			return ""
		}
		wt := worktrees[r.wt]
		indent := "                "
		if r.windowIndex >= 0 {
			indent = "                   "
		}
		connector := "├─"
		if r.wt == len(worktrees)-1 {
			connector = "└─"
		}
		indicator := prStatusIcon(wt.prStatus)
		if wt.prNumber > 0 {
			indicator += " " + dimStyle.Render("#"+strconv.Itoa(wt.prNumber))
		}
		if indicator != "" {
			indicator += "  "
		}
		left := indent + connector + " " + indicator + truncate(wt.branch, max(6, width-lipgloss.Width(indent)-lipgloss.Width(indicator)-6))
		right := mutedStyle.Render(displayPath(wt.path, os.Getenv("HOME")))
		if wt.current {
			right = dimStyle.Render("← here")
		}
		if focused {
			left = focusStyle.Render(ansi.Strip(left))
			right = focusStyle.Render(ansi.Strip(right))
		}
		if width >= 64 {
			return padColumns(left, right, width)
		}
		return ansi.Truncate(left, width, "…")
	case "wt-filter":
		// Worktree tab — flat list of the open project's worktrees.
		if r.wt < 0 || r.wt >= len(m.worktreeFilterRows) {
			return ""
		}
		wt := m.worktreeFilterRows[r.wt].worktree

		selected := " "
		if m.wtBulkSelected[wt.path] {
			selected = accentStyle.Render("✓")
		}
		current := " "
		if wt.current {
			current = accentStyle.Render("★")
		}

		prStatus := ""
		if wt.prNumber > 0 {
			prStatus = prStatusIcon(wt.prStatus) + " " + dimStyle.Render("#"+strconv.Itoa(wt.prNumber))
		}
		sync := worktreeSyncBadge(wt)

		branchWidth := max(12, width*40/100)
		branchPart := selected + current + " " + truncate(wt.branch, branchWidth-lipgloss.Width(selected)-lipgloss.Width(current)-1)

		statusWidth := max(8, width*20/100)
		statusPart := ""
		if prStatus != "" {
			segment := ansi.Truncate(prStatus, statusWidth, "…")
			if focused {
				segment = focusStyle.Render(ansi.Strip(segment))
			}
			statusPart += " " + segment
		}
		if sync != "" {
			segment := sync
			if focused {
				segment = focusStyle.Render(ansi.Strip(segment))
			}
			statusPart += " " + segment
		}

		pathPart := mutedStyle.Render(displayPath(wt.path, os.Getenv("HOME")))
		if wt.current {
			pathPart = dimStyle.Render("← here")
		}
		if focused {
			branchPart = focusStyle.Render(ansi.Strip(branchPart))
		}
		left := branchPart + statusPart
		right := pathPart
		if width >= 64 {
			return padColumns(left, right, width)
		}
		return ansi.Truncate(left+"  "+right, width, "…")
	}
	if r.windowIndex >= 0 {
		window := e.tabs[r.tabIndex].windows[r.windowIndex]
		if m.filter == filterAgents {
			return m.renderAgentRow(e, e.tabs[r.tabIndex], window, width)
		}
		branch := "├─"
		if r.windowIndex == len(e.tabs[r.tabIndex].windows)-1 {
			branch = "└─"
		}
		nameWidth := max(8, width*45/100-17)
		left := "           " + branch + " " + windowIcon(window) + " " + truncate(window.title, nameWidth)
		detail := window.detail
		if width >= 52 {
			return padColumns(left, dimStyle.Render(detail), width)
		}
		return ansi.Truncate(left, width, "…")
	}
	if r.tabIndex >= 0 {
		tab := e.tabs[r.tabIndex]
		branch := "├─"
		if r.tabIndex == len(e.tabs)-1 {
			branch = "└─"
		}
		arrow := " "
		if len(tab.windows) > 0 {
			arrow = "▸"
			if tab.expanded {
				arrow = "▾"
			}
		}
		windowCount := dimStyle.Render(fmt.Sprintf("%d window%s", len(tab.windows), plural(len(tab.windows))))
		nameWidth := max(8, width*45/100-17)
		left := fmt.Sprintf("       %s %s %s %s", branch, arrow, projectStyle.Render("󱂬"), truncate(tab.title, nameWidth))
		if width >= 52 {
			return padColumns(left, windowCount, width)
		}
		return ansi.Truncate(left+"  "+windowCount, width, "…")
	}
	selection := " "
	if m.selected[e.key] {
		selection = accentStyle.Render("✓")
	}
	arrow := " "
	if len(e.tabs) > 0 {
		arrow = "▸"
		if e.expanded {
			arrow = "▾"
		}
	}
	nameWidth := max(8, width*45/100-18)
	iconGlyph := ""
	if e.kind == "workspace" {
		iconGlyph = ""
	}
	icon := projectStyle.Render(iconGlyph)
	if e.kind == "ssh" {
		iconGlyph = ""
		icon = sshStyle.Render(iconGlyph)
	}
	name := truncate(e.name, nameWidth)
	if e.open {
		icon = openStyle.Render(iconGlyph)
		name = openStyle.Bold(true).Render(name)
	}
	pin := "   "
	if e.pin != "" {
		pin = accentStyle.Render("[" + e.pin + "]")
	}
	if focused && e.open {
		selection = focusStyle.Render(ansi.Strip(selection))
		pin = focusStyle.Render(ansi.Strip(pin))
		arrow = focusStyle.Render(arrow)
	}
	left := fmt.Sprintf("%s   %s %s %s %s", selection, pin, arrow, icon, name)
	detail := dimStyle.Render(e.detail)
	if focused && e.open {
		detail = focusStyle.Render(ansi.Strip(detail))
	}
	if width >= 52 {
		return padColumns(left, detail, width)
	}
	return ansi.Truncate(left, width, "…")
}

func prStatusIcon(status string) string {
	switch status {
	case "open":
		return prOpenStyle.Render("")
	case "merged":
		return prMergedStyle.Render("")
	case "closed":
		return prClosedStyle.Render("×")
	default:
		return ""
	}
}

func (m model) renderAgentRow(e entry, tab tabItem, window windowItem, width int) string {
	agent := agentLabel(window.agent)
	prefix := windowIcon(window) + " " + agent + "  "
	lastActive := relativeLastActive(window.lastFocused, m.lastFocusedReference())
	nameWidth := max(8, width*45/100-lipgloss.Width(prefix))
	if m.showPreview {
		nameWidth = max(8, width-lipgloss.Width(prefix)-lipgloss.Width(lastActive)-2)
	}

	// A project name identifies the agent more reliably than its tab title.
	// Only show the latter when it fits in full; a dangling " / foo…" hides
	// useful information while still failing to identify the tab.
	context := e.name
	if tab.title != "" && tab.title != e.name {
		candidate := context + " / " + tab.title
		if lipgloss.Width(candidate) <= nameWidth {
			context = candidate
		}
	}
	left := prefix + middleTruncate(context, nameWidth)
	if m.showPreview {
		return ansi.Truncate(left+"  "+dimStyle.Render(lastActive), width, "…")
	}
	detail := window.detail
	if width >= 52 {
		return padColumns(left, dimStyle.Render(detail+" · "+lastActive), width)
	}
	return ansi.Truncate(left, width, "…")
}

// Kitty reports last_focused_at as seconds since Kitty started, not a Unix
// timestamp. The most recently focused window is therefore our best available
// reference point when the picker opens.
func (m model) lastFocusedReference() float64 {
	var latest float64
	for _, entry := range m.entries {
		for _, tab := range entry.tabs {
			for _, window := range tab.windows {
				latest = max(latest, window.lastFocused)
			}
		}
	}
	return latest
}

func relativeLastActive(lastFocused, reference float64) string {
	if lastFocused <= 0 || reference <= 0 {
		return "unknown"
	}
	elapsed := time.Duration(max(0, reference-lastFocused) * float64(time.Second))
	if elapsed < time.Minute {
		return "just now"
	}
	if elapsed < time.Hour {
		return fmt.Sprintf("%dm ago", int(elapsed/time.Minute))
	}
	if elapsed < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(elapsed/time.Hour))
	}
	return fmt.Sprintf("%dd ago", int(elapsed/(24*time.Hour)))
}

func agentLabel(agent string) string {
	switch agent {
	case "pi":
		return "pi"
	case "codex":
		return "Codex"
	case "pi,codex":
		return "pi+Codex"
	default:
		return agent
	}
}

const shellIcon = ""

func windowIcon(window windowItem) string {
	if icon := agentIcon(window.agent); icon != "" {
		return icon
	}
	if icon := processIcon(window.command); icon != "" {
		return icon
	}
	return shellIcon
}

func agentIcon(agent string) string {
	switch agent {
	case "pi":
		return "π"
	case "codex":
		return "󰚩"
	case "pi,codex":
		return "π󰚩"
	default:
		return ""
	}
}

func processIcon(command string) string {
	switch strings.TrimPrefix(command, "-") {
	case "nvim", "vim":
		return ""
	case "zsh", "bash", "sh", "fish":
		return shellIcon
	default:
		return ""
	}
}

func padColumns(left, right string, width int) string {
	space := width*45/100 - lipgloss.Width(left)
	if space < 2 {
		space = 2
	}
	return ansi.Truncate(left+strings.Repeat(" ", space)+right, width, "…")
}

func truncate(value string, width int) string {
	if width <= 1 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= width {
		return value
	}
	return string(runes[:width-1]) + "…"
}

func commandOutput(name string, args ...string) <-chan commandResult {
	result := make(chan commandResult, 1)
	go func() {
		output, err := exec.Command(name, args...).Output()
		result <- commandResult{output: output, err: err}
	}()
	return result
}

// zoxideMergeContext carries what fetchZoxideEntries needs to dedup and name
// zoxide-sourced projects against the entries already built from live Kitty
// state, saved sessions, and SSH hosts.
type zoxideMergeContext struct {
	livePaths    map[string]bool
	merged       map[string]bool
	sessionNames map[string]bool
	home         string
}

// entryCategoryRank fixes the ordering of non-open entries: saved sessions,
// then source projects (zoxide), then SSH hosts. It lets zoxide projects —
// which now arrive asynchronously — slot into their original position relative
// to SSH hosts without depending on a global discovery counter.
func entryCategoryRank(e entry) int {
	if e.saved {
		return 0
	}
	if e.kind == "ssh" {
		return 2
	}
	return 1
}

// sortEntries orders entries: open sessions first (most recently focused), then
// by category (saved → source projects → SSH), stable within each group by
// discovery order. Used after the initial load and again when zoxide projects
// arrive asynchronously.
func sortEntries(entries []entry) {
	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.open != b.open {
			return a.open
		}
		if a.open && a.lastFocused != b.lastFocused {
			return a.lastFocused > b.lastFocused
		}
		if !a.open {
			ra, rb := entryCategoryRank(a), entryCategoryRank(b)
			if ra != rb {
				return ra < rb
			}
		}
		return a.order < b.order
	})
}

// buildZoxideEntries turns `zoxide query -l` output into source-project entries,
// skipping paths already represented (open/saved session) and including live
// window paths zoxide does not track. Order mirrors zoxide's frecency ranking.
func buildZoxideEntries(output []byte, ctx zoxideMergeContext) []entry {
	paths := strings.FieldsFunc(string(output), func(r rune) bool { return r == '\n' || r == '\r' })
	known := map[string]bool{}
	for _, path := range paths {
		known[path] = true
	}
	for path := range ctx.livePaths {
		if !known[path] {
			paths = append(paths, path)
		}
	}
	var entries []entry
	order := 0
	for _, path := range paths {
		if path == "" || path == "/" || ctx.merged[path] {
			continue
		}
		name := filepath.Base(path)
		entries = append(entries, entry{
			key: path, name: name, originalName: name, detail: displayPath(path, ctx.home),
			kind: "project", path: path, nameTaken: ctx.sessionNames[safeName(name)], order: order,
		})
		order++
	}
	return entries
}

// loadEntriesFast builds entries from live Kitty state, saved sessions, and SSH
// hosts only — everything obtainable without the (sometimes slow) zoxide query.
// Zoxide-sourced source projects are returned separately via the async
// fetchZoxideEntries command so the picker can paint before they arrive.
func loadEntriesFast(kitty string) ([]entry, zoxideMergeContext, error) {
	if kitty == "" {
		return nil, zoxideMergeContext{}, fmt.Errorf("kitty was not found")
	}
	savedStore, err := loadSavedSessions()
	if err != nil {
		return nil, zoxideMergeContext{}, err
	}
	kittyOutput := <-commandOutput(kitty, "@", "ls")
	if kittyOutput.err != nil {
		return nil, zoxideMergeContext{}, fmt.Errorf("kitty @ ls: %w", kittyOutput.err)
	}
	var state kittyState
	if err := json.Unmarshal(kittyOutput.output, &state); err != nil {
		return nil, zoxideMergeContext{}, fmt.Errorf("decode kitty state: %w", err)
	}
	selfID, _ := strconv.Atoi(os.Getenv("KITTY_WINDOW_ID"))
	type openSession struct {
		path    string
		focused float64
		tabs    []tabItem
	}
	sessions := map[string]*openSession{}
	sessionNames := map[string]bool{}
	livePaths := map[string]bool{}
	unscopedTabs := map[string][]tabItem{}
	unscopedFocus := map[string]float64{}
	openSSH := map[string]float64{}

	for _, osWindow := range state {
		for _, tab := range osWindow.Tabs {
			if isKeshTab(tab.Windows) {
				continue
			}
			sessionName := ""
			canonicalPath := ""
			var windows []windowItem
			focused := float64(0)
			for _, window := range tab.Windows {
				if window.ID == selfID {
					continue
				}
				path := windowPath(window)
				if path != "" {
					livePaths[path] = true
				}
				if canonicalPath == "" {
					canonicalPath = path
				}
				if sessionName == "" && window.SessionName != "" {
					sessionName = window.SessionName
					canonicalPath = path
				}
				focused = max(focused, window.LastFocusedAt)
				windows = append(windows, windowItemFromKitty(window))
			}
			if len(windows) > 0 {
				agent := mergedAgents(windows)
				title := cleanAgentTitle(tab.Title, agent)
				if title == "" {
					title = "tab " + strconv.Itoa(tab.ID)
				}
				item := tabItem{
					id: tab.ID, title: title,
					detail:  fmt.Sprintf("%d window%s", len(windows), plural(len(windows))),
					agent:   agent,
					windows: windows,
				}
				if sessionName == "" && canonicalPath != "" {
					unscopedTabs[canonicalPath] = append(unscopedTabs[canonicalPath], item)
					unscopedFocus[canonicalPath] = max(unscopedFocus[canonicalPath], focused)
				} else if sessionName != "" {
					sessionNames[sessionName] = true
					s := sessions[sessionName]
					if s == nil {
						s = &openSession{path: canonicalPath}
						sessions[sessionName] = s
					}
					s.focused = max(s.focused, focused)
					s.tabs = append(s.tabs, item)
				}
			}
			for _, window := range tab.Windows {
				if window.ID == selfID {
					continue
				}
				if host := sshHost(window); host != "" {
					openSSH[host] = max(openSSH[host], window.LastFocusedAt)
				}
			}
		}
	}

	var entries []entry
	order := 0
	home := os.Getenv("HOME")

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
		if session.path != "" {
			name = filepath.Base(session.path)
		}
		_, composed := composedSessionName(sessionName)
		if composedName, ok := composedSessionName(sessionName); ok {
			name = composedName
		}
		record, saved := savedSessionForName(savedStore, sessionName)
		sessionFile := ""
		if saved {
			name = record.Name
			sessionFile = record.SessionFile
			seenSavedSessions[record.SessionFile] = true
			composed = composed || len(record.Projects) > 1
		}
		detail := displayPath(session.path, home)
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
		entries = append(entries, entry{
			key: key, name: name, originalName: name, detail: detail,
			kind: kind, path: session.path, session: sessionName, sessionFile: sessionFile, saved: saved,
			open: true, lastFocused: session.focused,
			agent: mergedTabAgents(session.tabs), tabs: session.tabs, order: order,
		})
		order++
	}

	unscopedPaths := make([]string, 0, len(unscopedTabs))
	for path := range unscopedTabs {
		unscopedPaths = append(unscopedPaths, path)
	}
	sort.Strings(unscopedPaths)
	for _, path := range unscopedPaths {
		tabs := unscopedTabs[path]
		if mergedProjects[path] {
			continue
		}
		name := filepath.Base(path)
		entries = append(entries, entry{
			key: path, name: name, originalName: name, detail: displayPath(path, home),
			kind: "project", path: path, open: true, lastFocused: unscopedFocus[path],
			agent: mergedTabAgents(tabs), tabs: tabs, order: order,
		})
		mergedProjects[path] = true
		order++
	}

	savedFiles := make([]string, 0, len(savedStore.Sessions))
	for file := range savedStore.Sessions {
		savedFiles = append(savedFiles, file)
	}
	sort.Strings(savedFiles)
	for _, file := range savedFiles {
		if seenSavedSessions[file] {
			continue
		}
		record := savedStore.Sessions[file]
		path := ""
		if len(record.Projects) > 0 {
			path = record.Projects[0]
		}
		detail := "saved session"
		if path != "" {
			detail = displayPath(path, home)
		}
		_, composed := composedSessionName(record.SessionName)
		composed = composed || len(record.Projects) > 1 || path == ""
		kind := "workspace"
		key := "workspace:" + record.SessionName
		if !composed && !mergedProjects[path] {
			kind = "project"
			key = path
			mergedProjects[path] = true
		}
		entries = append(entries, entry{
			key: key, name: record.Name, originalName: record.Name, detail: detail,
			kind: kind, path: path, session: record.SessionName, sessionFile: record.SessionFile,
			saved: true, order: order,
		})
		sessionNames[record.SessionName] = true
		order++
	}

	for _, host := range readSSHHosts(filepath.Join(home, ".ssh", "config")) {
		var tabs []tabItem
		session := ""
		if _, ok := openSSH[host.name]; ok {
			session = "ssh-" + safeName(host.name)
			if s := sessions[session]; s != nil {
				tabs = s.tabs
			}
		}
		entries = append(entries, entry{
			key: "ssh://" + host.name, name: host.name, originalName: host.name, detail: host.target, kind: "ssh",
			session: session, open: session != "", lastFocused: openSSH[host.name],
			agent: mergedTabAgents(tabs), tabs: tabs, order: order,
		})
		order++
	}
	sortEntries(entries)
	return entries, zoxideMergeContext{
		livePaths: livePaths, merged: mergedProjects, sessionNames: sessionNames, home: home,
	}, nil
}

// loadEntries is the synchronous full load (live Kitty + saved + SSH + zoxide),
// used by one-shot CLI paths such as pin switching where blocking on zoxide is
// acceptable. The interactive picker uses loadEntriesFast + fetchZoxideEntries
// instead so first paint is not gated on zoxide.
func loadEntries(kitty, zoxide string) ([]entry, error) {
	entries, ctx, err := loadEntriesFast(kitty)
	if err != nil {
		return nil, err
	}
	if zoxide != "" {
		output, zerr := exec.Command(zoxide, "query", "-l").Output()
		if zerr != nil {
			return nil, fmt.Errorf("zoxide query: %w", zerr)
		}
		entries = append(entries, buildZoxideEntries(output, ctx)...)
		sortEntries(entries)
	}
	return entries, nil
}

// fetchZoxideEntries runs `zoxide query -l` off the main startup path and
// returns the zoxide-sourced source-project entries to merge once they arrive.
type zoxideEntriesMsg struct {
	entries []entry
	err     error
}

func fetchZoxideEntries(zoxide string, ctx zoxideMergeContext) tea.Cmd {
	return func() tea.Msg {
		if zoxide == "" {
			return zoxideEntriesMsg{err: fmt.Errorf("zoxide was not found")}
		}
		output, err := exec.Command(zoxide, "query", "-l").Output()
		if err != nil {
			return zoxideEntriesMsg{err: fmt.Errorf("zoxide query: %w", err)}
		}
		return zoxideEntriesMsg{entries: buildZoxideEntries(output, ctx)}
	}
}

func isKeshTab(windows []kittyWindow) bool {
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

func foregroundCmdlines(window kittyWindow) [][]string {
	commands := make([][]string, 0, len(window.ForegroundProcesses))
	for _, process := range window.ForegroundProcesses {
		commands = append(commands, process.Cmdline)
	}
	return commands
}

func cachedPathPR(path string) pathPRInfo {
	if path == "" {
		return pathPRInfo{}
	}
	output, err := exec.Command("git", "-C", path, "rev-parse", "--show-toplevel", "HEAD", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return pathPRInfo{}
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) < 3 {
		return pathPRInfo{}
	}
	repoKey := repositoryCacheKey(path)
	pullRequests, _ := loadPRStatusCache(repoKey)
	pullRequest, exact := matchPullRequest(pullRequests, lines[2], lines[1])
	return pathPRInfo{Branch: lines[2], Head: lines[1], RepoKey: repoKey, PullRequest: pullRequest, Exact: exact}
}

func windowPath(window kittyWindow) string {
	if path := window.Env["PWD"]; path != "" {
		return path
	}
	return window.CWD
}

func windowItemFromKitty(window kittyWindow) windowItem {
	command := ""
	fullCommand := ""
	detail := windowPath(window)
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
	agent := agentFromWindow(window)
	title := cleanAgentTitle(window.Title, agent)
	if title == "" {
		title = command
	}
	if title == "" {
		title = "window " + strconv.Itoa(window.ID)
	}
	return windowItem{
		id: window.ID, title: title, detail: displayPath(detail, os.Getenv("HOME")), command: command,
		fullCommand: fullCommand, agent: agent, lastFocused: window.LastFocusedAt, cwd: detail,
	}
}

func cleanAgentTitle(title, agent string) string {
	if strings.Contains(agent, "pi") {
		// Pi owns its terminal title (including an animated prefix while working).
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

func agentFromWindow(window kittyWindow) string {
	pi, codex := false, false
	for _, process := range window.ForegroundProcesses {
		command := " " + strings.ToLower(strings.Join(process.Cmdline, " ")) + " "
		pi = pi || strings.Contains(command, " pi ") || strings.Contains(command, "/pi ")
		codex = codex || strings.Contains(command, " codex ") || strings.Contains(command, "/codex ")
	}
	if pi && codex {
		return "pi,codex"
	}
	if pi {
		return "pi"
	}
	if codex {
		return "codex"
	}
	return ""
}

func mergedAgents(windows []windowItem) string {
	pi, codex := false, false
	for _, window := range windows {
		pi = pi || strings.Contains(window.agent, "pi")
		codex = codex || strings.Contains(window.agent, "codex")
	}
	if pi && codex {
		return "pi,codex"
	}
	if pi {
		return "pi"
	}
	if codex {
		return "codex"
	}
	return ""
}

func mergedTabAgents(tabs []tabItem) string {
	pi, codex := false, false
	for _, tab := range tabs {
		pi = pi || strings.Contains(tab.agent, "pi")
		codex = codex || strings.Contains(tab.agent, "codex")
	}
	if pi && codex {
		return "pi,codex"
	}
	if pi {
		return "pi"
	}
	if codex {
		return "codex"
	}
	return ""
}

func plural(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}

func sshHost(window kittyWindow) string {
	for _, process := range window.ForegroundProcesses {
		if len(process.Cmdline) > 1 && filepath.Base(process.Cmdline[0]) == "ssh" {
			return process.Cmdline[1]
		}
	}
	return ""
}

type sshConfigHost struct{ name, target string }

func readSSHHosts(path string) []sshConfigHost {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	wildcard := regexp.MustCompile(`[*?!]`)
	options := map[string]map[string]string{}
	var current []string
	for _, raw := range strings.Split(string(content), "\n") {
		line := strings.TrimSpace(strings.SplitN(raw, "#", 2)[0])
		parts := strings.Fields(line)
		if len(parts) == 0 {
			continue
		}
		switch strings.ToLower(parts[0]) {
		case "host":
			current = nil
			for _, host := range parts[1:] {
				if !wildcard.MatchString(host) {
					current = append(current, host)
					if options[host] == nil {
						options[host] = map[string]string{}
					}
				}
			}
		case "user", "hostname", "port":
			if len(parts) < 2 {
				continue
			}
			key := strings.ToLower(parts[0])
			for _, host := range current {
				if options[host][key] == "" {
					options[host][key] = parts[1]
				}
			}
		}
	}
	names := make([]string, 0, len(options))
	for name := range options {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]sshConfigHost, 0, len(names))
	for _, name := range names {
		hostname := strings.ReplaceAll(options[name]["hostname"], "%h", name)
		if hostname == "" {
			hostname = name
		}
		user := options[name]["user"]
		if user == "" {
			user = os.Getenv("USER")
		}
		port := options[name]["port"]
		if port == "" {
			port = "22"
		}
		target := hostname + ":" + port
		if user != "" {
			target = user + "@" + target
		}
		result = append(result, sshConfigHost{name: name, target: target})
	}
	return result
}

func runRename(kitty string, e entry, selected row, title string, names nameStore) tea.Cmd {
	return func() tea.Msg {
		var err error
		if selected.windowIndex >= 0 {
			window := e.tabs[selected.tabIndex].windows[selected.windowIndex]
			err = run(kitty, "@", "set-window-title", "--match", "id:"+strconv.Itoa(window.id), title)
		} else if selected.tabIndex >= 0 {
			tab := e.tabs[selected.tabIndex]
			err = run(kitty, "@", "set-tab-title", "--match", "id:"+strconv.Itoa(tab.id), title)
		} else {
			title = strings.TrimSpace(title)
			updated := make(nameStore, len(names)+1)
			for key, name := range names {
				updated[key] = name
			}
			if title == "" {
				delete(updated, e.key)
			} else {
				updated[e.key] = title
			}
			err = saveNames(updated)
			names = updated
		}
		return renameMsg{selected: selected, title: title, names: names, err: err}
	}
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
				err = run(kitty, args...)
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
	name := strings.TrimPrefix(session, "kesh-")
	return name, name != session && name != ""
}

func composedSessionContent(name string, entries []entry) string {
	var content strings.Builder
	content.WriteString("os_window_title ")
	content.WriteString(name)
	content.WriteString("\nlayout splits\n")
	for _, entry := range entries {
		title := entry.name
		// Kitty treats the rest of a new_tab line as its title, including any
		// quote characters, so do not shell-quote it.
		content.WriteString("new_tab ")
		content.WriteString(title)
		content.WriteByte('\n')
		if entry.kind == "ssh" {
			host := strings.TrimPrefix(entry.key, "ssh://")
			content.WriteString("cd ")
			content.WriteString(os.Getenv("HOME"))
			content.WriteString("\nlaunch --title ")
			content.WriteString(strconv.Quote("ssh: " + host))
			content.WriteString(" ssh ")
			content.WriteString(strconv.Quote(host))
			content.WriteByte('\n')
			continue
		}
		content.WriteString("cd ")
		content.WriteString(entry.key)
		content.WriteString("\nlaunch --title ")
		content.WriteString(strconv.Quote(title))
		content.WriteByte('\n')
	}
	content.WriteString("focus\nfocus_os_window\n")
	return content.String()
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

func runSaveSession(kitty string, e entry, entryIndex int, foregroundCommands bool) tea.Cmd {
	return func() tea.Msg {
		sessionName := e.session
		if sessionName == "" {
			return saveSessionMsg{entryIndex: entryIndex, err: fmt.Errorf("workspace has no Kitty session name")}
		}
		file := e.sessionFile
		if file == "" {
			file = savedSessionFilePath(sessionName)
		}
		if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
			return saveSessionMsg{entryIndex: entryIndex, err: fmt.Errorf("create saved session directory: %w", err)}
		}
		match := "session:^" + regexp.QuoteMeta(sessionName) + "$"
		args := []string{"@", "action", "save_as_session", "--save-only"}
		if foregroundCommands {
			args = append(args, "--use-foreground-process")
		}
		args = append(args, "--match="+match, file)
		if err := run(kitty, args...); err != nil {
			return saveSessionMsg{entryIndex: entryIndex, err: fmt.Errorf("save Kitty session: %w", err)}
		}
		if err := os.Chmod(file, 0o600); err != nil {
			return saveSessionMsg{entryIndex: entryIndex, err: fmt.Errorf("secure saved session: %w", err)}
		}
		store, err := loadSavedSessions()
		if err != nil {
			return saveSessionMsg{entryIndex: entryIndex, err: err}
		}
		for existingFile, record := range store.Sessions {
			if record.SessionName == sessionName && existingFile != file {
				delete(store.Sessions, existingFile)
			}
		}
		record := savedSessionRecord{
			Name: e.name, SessionName: sessionName, SessionFile: filepath.Clean(file),
			Projects: workspaceProjects(e), ForegroundCommands: foregroundCommands,
			SavedAt: time.Now().UTC().Format(time.RFC3339),
		}
		store.Sessions[record.SessionFile] = record
		if err := saveSavedSessions(store); err != nil {
			return saveSessionMsg{entryIndex: entryIndex, err: err}
		}
		return saveSessionMsg{entryIndex: entryIndex, record: record}
	}
}

func repositoryName(repository string) (string, error) {
	repository = strings.TrimSpace(strings.TrimRight(repository, "/"))
	if strings.ContainsAny(repository, "\r\n") {
		return "", fmt.Errorf("repository cannot contain a line break")
	}
	if repository == "" {
		return "", fmt.Errorf("repository is required")
	}
	if strings.HasPrefix(repository, "-") {
		return "", fmt.Errorf("repository cannot start with a dash")
	}
	source := repository
	if strings.Contains(repository, "://") {
		parsed, err := url.Parse(repository)
		if err != nil || parsed.Host == "" || strings.Trim(parsed.Path, "/") == "" {
			return "", fmt.Errorf("could not determine a repository name from %q", repository)
		}
		source = strings.Trim(parsed.Path, "/")
	}
	separator := max(strings.LastIndex(source, "/"), strings.LastIndex(source, ":"))
	name := strings.TrimSuffix(source[separator+1:], ".git")
	if name == "" || name == "." || name == ".." {
		return "", fmt.Errorf("could not determine a repository name from %q", repository)
	}
	if strings.ContainsAny(name, `/\\`) {
		return "", fmt.Errorf("invalid repository name %q", name)
	}
	return name, nil
}

// parsePullRequestInput accepts a pull request reference and returns the GitHub
// owner, repository, and PR number. It recognises three shapes:
//
//   - a full URL: https://github.com/owner/repo/pull/123 (optionally with a
//     trailing path such as /files); the host may differ for self-hosted GitHub
//   - owner/repo#123
//   - a bare number (123), in which case useSelected is true and the caller
//     resolves owner/repo from the project under the cursor
//
// An empty value, a non-numeric PR, or an SSH git URL is an error.
func parsePullRequestInput(value string) (owner, repo string, number int, useSelected bool, err error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", "", 0, false, fmt.Errorf("enter a pull request URL or number")
	}
	if strings.ContainsAny(value, "\r\n") {
		return "", "", 0, false, fmt.Errorf("pull request reference cannot contain a line break")
	}

	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		parsed, parseErr := url.Parse(value)
		if parseErr != nil || parsed.Host == "" {
			return "", "", 0, false, fmt.Errorf("could not parse pull request URL %q", value)
		}
		segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
		// Expect owner/repo/pull/<number> [...]; "pulls" is GitHub's plural alias.
		pullIndex := -1
		for i, segment := range segments {
			if segment == "pull" || segment == "pulls" {
				pullIndex = i
				break
			}
		}
		if pullIndex < 0 || pullIndex < 2 {
			return "", "", 0, false, fmt.Errorf("could not find owner/repo/pull/<number> in %q", value)
		}
		owner = segments[pullIndex-2]
		repo = strings.TrimSuffix(segments[pullIndex-1], ".git")
		number, err = parsePRNumber(segments[pullIndex+1:])
		if err != nil {
			return "", "", 0, false, err
		}
		return owner, repo, number, false, nil
	}

	if strings.HasPrefix(value, "git@") || strings.Contains(value, "://") {
		return "", "", 0, false, fmt.Errorf("paste the pull request's web URL, not a git URL")
	}

	if hash := strings.Index(value, "#"); hash >= 0 {
		left := strings.TrimSpace(value[:hash])
		number, err = parsePRNumber([]string{strings.TrimSpace(value[hash+1:])})
		if err != nil {
			return "", "", 0, false, err
		}
		parts := strings.Split(strings.Trim(left, "/"), "/")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return "", "", 0, false, fmt.Errorf("owner/repo#<number> expected, got %q", value)
		}
		return parts[0], strings.TrimSuffix(parts[1], ".git"), number, false, nil
	}

	// Bare number: the caller resolves owner/repo from the selected project.
	number, err = parsePRNumber([]string{value})
	if err != nil {
		return "", "", 0, false, err
	}
	return "", "", number, true, nil
}

// parsePRNumber reads the leading digits of the first non-empty segment and
// rejects anything that is not a positive PR number.
func parsePRNumber(segments []string) (int, error) {
	for _, segment := range segments {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			continue
		}
		digits := segment
		for i, r := range segment {
			if r < '0' || r > '9' {
				digits = segment[:i]
				break
			}
		}
		number, convErr := strconv.Atoi(digits)
		if convErr != nil || number <= 0 {
			return 0, fmt.Errorf("invalid pull request number %q", segment)
		}
		return number, nil
	}
	return 0, fmt.Errorf("could not find a pull request number")
}

func resolveCloneDestination(value, root string) (string, error) {
	destination, err := expandHomePath(value)
	if err != nil {
		return "", fmt.Errorf("invalid clone destination: %w", err)
	}
	if !filepath.IsAbs(destination) {
		destination = filepath.Join(root, destination)
	}
	return filepath.Clean(destination), nil
}

func runClone(kitty, zoxide, repository, destination string) tea.Cmd {
	return func() tea.Msg {
		repository = strings.TrimSpace(repository)
		if _, err := repositoryName(repository); err != nil {
			return cloneMsg{err: err}
		}
		if _, err := os.Stat(destination); err == nil {
			return cloneMsg{err: fmt.Errorf("clone destination already exists: %s", destination)}
		} else if !os.IsNotExist(err) {
			return cloneMsg{err: fmt.Errorf("check clone destination: %w", err)}
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return cloneMsg{err: fmt.Errorf("create clone directory: %w", err)}
		}
		if err := run("git", "clone", "--", repository, destination); err != nil {
			return cloneMsg{err: fmt.Errorf("clone repository: %w", err)}
		}
		if err := openProjectSession(kitty, zoxide, destination, false); err != nil {
			return cloneMsg{err: err}
		}
		return cloneMsg{}
	}
}

// runCheckoutPR turns a GitHub pull request into an open workspace. It resolves
// an existing local clone (the project under the cursor, or a candidate under
// the checkout root), cloning first when none exists; fetches the PR head into a
// local branch named after the PR's head ref; creates a worktree for it; and
// opens the workspace. If a worktree on that branch already exists it is focused
// instead, so re-checking out the same PR is a no-op.
// resolvePRPreview fetches only the head branch for the input preview. Its
// value is carried with the result so Update can ignore stale lookups.
func resolvePRPreview(value, owner, repo string, number int) tea.Cmd {
	return func() tea.Msg {
		branch, _ := lookupPRHeadBranch(owner, repo, number, "")
		return prPreviewMsg{value: value, branch: branch}
	}
}

func lookupPRHeadBranch(owner, repo string, number int, dir string) (string, error) {
	gh := findCommand("gh",
		filepath.Join(os.Getenv("HOME"), ".local", "share", "mise", "shims", "gh"),
		"/opt/homebrew/bin/gh",
		"/usr/local/bin/gh",
	)
	if gh == "" {
		return "", fmt.Errorf("gh was not found")
	}
	view := exec.Command(gh, "pr", "view", strconv.Itoa(number), "--repo", owner+"/"+repo, "--json", "headRefName")
	view.Dir = dir
	output, err := view.CombinedOutput()
	if err != nil {
		return "", commandError("gh pr view", output, err)
	}
	var pr struct {
		HeadRefName string `json:"headRefName"`
	}
	if err := json.Unmarshal(output, &pr); err != nil {
		return "", fmt.Errorf("parse pull request: %w", err)
	}
	branch := strings.TrimSpace(pr.HeadRefName)
	if branch == "" {
		return "", fmt.Errorf("pull request #%d has no head branch", number)
	}
	return branch, nil
}

func runCheckoutPR(kitty, zoxide, owner, repo string, number int, selectedRepoPath, checkoutRoot, cloneRoot string) tea.Cmd {
	cloneURL := "https://github.com/" + owner + "/" + repo + ".git"
	return func() tea.Msg {
		var probe model // gives access to getRepoOwner without model state
		matchesRepo := func(path string) bool {
			remoteOwner, remoteRepo := probe.getRepoOwner(path)
			return strings.EqualFold(remoteOwner, owner) && strings.EqualFold(remoteRepo, repo)
		}

		// 1. Resolve an existing local clone, preferring the selected project.
		repoPath := ""
		if selectedRepoPath != "" && matchesRepo(selectedRepoPath) {
			repoPath = selectedRepoPath
		}
		if repoPath == "" {
			if candidate := filepath.Join(checkoutRoot, owner, repo); dirExists(candidate) && matchesRepo(candidate) {
				repoPath = candidate
			}
		}

		// 2. Clone when no local clone is found.
		if repoPath == "" {
			destination := filepath.Join(cloneRoot, owner, repo)
			if _, err := os.Stat(destination); err == nil {
				return prCheckoutMsg{err: fmt.Errorf("clone destination already exists: %s", displayPath(destination, os.Getenv("HOME")))}
			} else if !os.IsNotExist(err) {
				return prCheckoutMsg{err: fmt.Errorf("check clone destination: %w", err)}
			}
			if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
				return prCheckoutMsg{err: fmt.Errorf("create clone directory: %w", err)}
			}
			if err := run("git", "clone", "--", cloneURL, destination); err != nil {
				return prCheckoutMsg{err: fmt.Errorf("clone repository: %w", err)}
			}
			repoPath = destination
		}

		// 3. Resolve the PR's head branch via gh.
		branch, err := lookupPRHeadBranch(owner, repo, number, repoPath)
		if err != nil {
			return prCheckoutMsg{err: err}
		}
		return checkoutPRBranch(kitty, zoxide, owner, repo, number, branch, repoPath, cloneURL, matchesRepo)
	}
}

// checkoutPRBranch is the worktree-creating half of runCheckoutPR, split out so
// the head branch is the only PR detail it needs.
func checkoutPRBranch(kitty, zoxide, owner, repo string, number int, branch, repoPath, cloneURL string, matchesRepo func(string) bool) tea.Msg {
	// 4. Idempotency: a worktree on this branch already exists → focus it.
	listOutput, err := exec.Command("git", "-C", repoPath, "worktree", "list", "--porcelain").CombinedOutput()
	if err != nil {
		return prCheckoutMsg{err: fmt.Errorf("git worktree list: %w", err)}
	}
	for _, wt := range parseWorktreePorcelain(string(listOutput)) {
		if wt.branch == branch {
			_ = run(zoxide, "add", "--", wt.path)
			return prCheckoutMsg{err: openProjectSession(kitty, zoxide, wt.path, false)}
		}
	}

	// 5. Fetch the PR head into a local branch and create a worktree for it.
	worktreeRoot, err := loadWorktreeRoot()
	if err != nil {
		return prCheckoutMsg{err: err}
	}
	wtPath := filepath.Join(worktreeRoot, owner, repo, worktreeDirectoryName(branch))
	if _, err := os.Stat(wtPath); err == nil {
		return prCheckoutMsg{err: fmt.Errorf("worktree already exists at %s", displayPath(wtPath, os.Getenv("HOME")))}
	} else if !os.IsNotExist(err) {
		return prCheckoutMsg{err: fmt.Errorf("check worktree path: %w", err)}
	}
	if err := os.MkdirAll(filepath.Dir(wtPath), 0o755); err != nil {
		return prCheckoutMsg{err: fmt.Errorf("create worktree directory: %w", err)}
	}
	// Fork-origin checkouts still resolve the PR via the canonical GitHub URL.
	fetchSource := "origin"
	if !matchesRepo(repoPath) {
		fetchSource = cloneURL
	}
	// The local PR branch may have been fetched previously and rewritten since;
	// PR refs are not guaranteed to fast-forward, so update this managed ref
	// explicitly rather than rejecting a valid re-checkout.
	fetch := exec.Command("git", "-C", repoPath, "fetch", "--", fetchSource,
		fmt.Sprintf("+refs/pull/%d/head:refs/heads/%s", number, branch))
	if fetchOutput, ferr := fetch.CombinedOutput(); ferr != nil {
		return prCheckoutMsg{err: commandError("git fetch", fetchOutput, ferr)}
	}
	add := exec.Command("git", "-C", repoPath, "worktree", "add", wtPath, branch)
	if addOutput, aerr := add.CombinedOutput(); aerr != nil {
		return prCheckoutMsg{err: commandError("git worktree add", addOutput, aerr)}
	}

	// 6. Open the workspace.
	_ = run(zoxide, "add", "--", wtPath)
	return prCheckoutMsg{err: openProjectSession(kitty, zoxide, wtPath, false)}
}

// worktreeDirectoryName keeps a PR branch in one directory. Git branch names
// commonly contain slashes (for example fix/widget), which should not create
// an accidental nested directory tree beneath the worktree root.
func worktreeDirectoryName(branch string) string {
	return strings.ReplaceAll(branch, "/", "-")
}

// dirExists reports whether path is an existing directory.
func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// runWktreeNew delegates recipe-driven creation and Kitty layout setup to
// wktree; Kesh only owns the interactive branch prompt and refresh afterward.
func runWktreeNew(recipePath, mode, branch string, selected []string) tea.Cmd {
	return func() tea.Msg {
		wktree := findCommand("wktree", filepath.Join(os.Getenv("HOME"), ".local", "bin", "wktree"), "/opt/homebrew/bin/wktree")
		if wktree == "" {
			return worktreeMsg{err: fmt.Errorf("wktree was not found")}
		}
		args := []string{"new"}
		switch mode {
		case "all":
			args = append(args, "--workspaces")
		case "selected":
			for _, name := range selected {
				args = append(args, "--workspace", name)
			}
		}
		args = append(args, branch)
		command := exec.Command(wktree, args...)
		command.Dir = filepath.Dir(recipePath)
		if output, err := command.CombinedOutput(); err != nil {
			return worktreeMsg{err: commandError("wktree new", output, err)}
		}
		return worktreeMsg{}
	}
}

func runCreateSession(kitty string, entries []entry, name string) tea.Cmd {
	return func() tea.Msg {
		if len(entries) == 0 {
			return createMsg{err: fmt.Errorf("select at least one project or SSH host")}
		}
		path := composedSessionPath(name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return createMsg{err: fmt.Errorf("create session directory: %w", err)}
		}
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			if os.IsExist(err) {
				return createMsg{err: fmt.Errorf("a session named %q already exists", name)}
			}
			return createMsg{err: fmt.Errorf("create session file: %w", err)}
		}
		defer os.Remove(path)
		if _, err := file.WriteString(composedSessionContent(name, entries)); err != nil {
			file.Close()
			return createMsg{err: fmt.Errorf("write session file: %w", err)}
		}
		if err := file.Close(); err != nil {
			return createMsg{err: fmt.Errorf("close session file: %w", err)}
		}
		if err := run(kitty, "@", "action", "goto_session", path); err != nil {
			return createMsg{err: err}
		}
		return createMsg{}
	}
}

func runAction(kitty, zoxide string, e entry, selected row) tea.Cmd {
	return func() tea.Msg {
		// The Worktree tab addresses a worktree by path (the tab list is a
		// filtered, re-sorted subset of the entry's worktrees, so a wt index
		// would target the wrong tree). Open it directly instead of falling
		// through to the project's main checkout.
		if selected.worktreePath != "" {
			return actionMsg{err: openProjectSession(kitty, zoxide, selected.worktreePath, false)}
		}
		if selected.section == "wt-item" && selected.wt >= 0 {
			if selected.tabIndex < 0 && selected.wt < len(e.worktrees) {
				return actionMsg{err: openProjectSession(kitty, zoxide, e.worktrees[selected.wt].path, false)}
			}
			if selected.tabIndex >= 0 && selected.tabIndex < len(e.tabs) && selected.windowIndex >= 0 &&
				selected.windowIndex < len(e.tabs[selected.tabIndex].windows) &&
				selected.wt < len(e.tabs[selected.tabIndex].windows[selected.windowIndex].worktrees) {
				wt := e.tabs[selected.tabIndex].windows[selected.windowIndex].worktrees[selected.wt]
				return actionMsg{err: openProjectSession(kitty, zoxide, wt.path, false)}
			}
		}
		if selected.windowIndex >= 0 {
			window := e.tabs[selected.tabIndex].windows[selected.windowIndex]
			return actionMsg{err: run(kitty, "@", "focus-window", "--match", "id:"+strconv.Itoa(window.id))}
		}
		if selected.tabIndex >= 0 {
			return actionMsg{err: run(kitty, "@", "focus-tab", "--match", "id:"+strconv.Itoa(e.tabs[selected.tabIndex].id))}
		}
		if e.sessionFile != "" {
			return actionMsg{err: run(kitty, "@", "action", "goto_session", e.sessionFile)}
		}
		if e.session != "" {
			return actionMsg{err: run(kitty, "@", "action", "goto_session", e.session)}
		}
		if len(e.tabs) > 0 {
			return actionMsg{err: run(kitty, "@", "focus-tab", "--match", "id:"+strconv.Itoa(e.tabs[0].id))}
		}
		if e.kind == "ssh" {
			sessionDir := filepath.Join(os.TempDir(), "kitty-zoxide-sessions")
			if err := os.MkdirAll(sessionDir, 0o755); err != nil {
				return actionMsg{err: err}
			}
			host := strings.TrimPrefix(e.key, "ssh://")
			file := filepath.Join(sessionDir, "ssh-"+safeName(host)+".kitty-session")
			content := fmt.Sprintf("layout splits\ncd %s\nlaunch --title \"ssh: %s\" ssh \"%s\"\nfocus\nfocus_os_window\n", os.Getenv("HOME"), host, host)
			if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
				return actionMsg{err: err}
			}
			return actionMsg{err: run(kitty, "@", "action", "goto_session", file)}
		}
		return actionMsg{err: openProjectSession(kitty, zoxide, e.key, e.nameTaken)}
	}
}

func openProjectSession(kitty, zoxide, project string, nameTaken bool) error {
	sessionDir := filepath.Join(os.TempDir(), "kitty-zoxide-sessions")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		return err
	}
	name := safeName(filepath.Base(project))
	if nameTaken {
		name += "-" + shortHash(project)
	}
	file := filepath.Join(sessionDir, name+".kitty-session")
	content := fmt.Sprintf("layout splits\ncd %s\nlaunch --title %s\nfocus\nfocus_os_window\n", project, strconv.Quote(filepath.Base(project)))
	if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
		return err
	}
	_ = run(zoxide, "add", "--", project)
	if err := run(kitty, "@", "action", "goto_session", file); err != nil {
		return err
	}
	return nil
}

// fetchOriginThenReload updates remote-tracking refs before reloading a
// project's worktrees. Local state still reloads if fetch fails, while the
// returned error makes a stale remote status visible to the user.
func fetchOriginThenReload(dir string, entryIndex int) tea.Cmd {
	return func() tea.Msg {
		output, err := exec.Command("git", "-C", dir, "fetch", "--prune").CombinedOutput()
		if err != nil {
			err = commandError("git fetch", output, err)
		}
		return worktreeFetchedMsg{dir: dir, entryIndex: entryIndex, err: err}
	}
}

// pullWorktree fast-forwards or rebases a single worktree's branch onto its
// upstream. The reload afterwards recomputes the worktree's ahead/behind.
func pullWorktree(path, dir string, entryIndex int) tea.Cmd {
	return func() tea.Msg {
		output, err := exec.Command("git", "-C", path, "pull", "--rebase").CombinedOutput()
		if err != nil {
			return worktreePullMsg{dir: dir, entryIndex: entryIndex, err: commandError("git pull", output, err)}
		}
		return worktreePullMsg{dir: dir, entryIndex: entryIndex}
	}
}

// pullWorktrees pulls every target worktree in one command, collecting per-
// target failures so one bad branch does not abort the rest. Returns a single
// summary that the worktreePullMsg handler surfaces and then reloads.
func pullWorktrees(targets []worktreeItem, dir string, entryIndex int) tea.Cmd {
	return func() tea.Msg {
		var failures []string
		for _, target := range targets {
			if target.path == "" {
				continue
			}
			output, err := exec.Command("git", "-C", target.path, "pull", "--rebase").CombinedOutput()
			if err != nil {
				failures = append(failures, commandError(target.branch, output, err).Error())
			}
		}
		if len(failures) > 0 {
			return worktreePullMsg{dir: dir, entryIndex: entryIndex, err: fmt.Errorf("some worktrees did not pull: %s", strings.Join(failures, "; "))}
		}
		return worktreePullMsg{dir: dir, entryIndex: entryIndex}
	}
}

func run(name string, args ...string) error {
	if name == "" {
		return fmt.Errorf("required command was not found")
	}
	output, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message != "" {
			return fmt.Errorf("%s: %s", err, message)
		}
	}
	return err
}

// openerCommand resolves the platform's default URL opener so PR links open in
// the browser on every OS: macOS uses "open", Linux/BSDs use "xdg-open". It
// scans PATH so a missing opener is reported cleanly instead of shelling out to
// a binary that does not exist. Returns "" when no opener is installed.
func openerCommand() string {
	candidates := []string{"xdg-open", "open"}
	if runtime.GOOS == "darwin" {
		candidates = []string{"open", "xdg-open"}
	}
	for _, name := range candidates {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	return ""
}

// openURL opens target in the user's default browser via the platform opener.
// Both the Worktree tab and the main-mode PR opener route through here so the
// platform choice lives in one place.
func openURL(target string) error {
	opener := openerCommand()
	if opener == "" {
		return fmt.Errorf("no URL opener found (install xdg-open or open)")
	}
	return run(opener, target)
}

func safeName(value string) string {
	return regexp.MustCompile(`[^A-Za-z0-9._-]+`).ReplaceAllString(value, "_")
}

func shortHash(value string) string {
	// FNV-1a is sufficient here; the hash only disambiguates equal basenames.
	var hash uint32 = 2166136261
	for i := 0; i < len(value); i++ {
		hash ^= uint32(value[i])
		hash *= 16777619
	}
	return fmt.Sprintf("%06x", hash)[:6]
}

func displayPath(path, home string) string {
	if path == home {
		return "~"
	}
	if strings.HasPrefix(path, home+string(os.PathSeparator)) {
		return "~" + strings.TrimPrefix(path, home)
	}
	return path
}

func (m *model) calculateWorktreePaths() []string {
	entries := m.worktreeEntries()
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.kind != "project" {
			continue
		}
		owner, repo := m.getRepoOwner(entry.path)
		// filepath.Join drops a trailing empty segment, so with no branch typed
		// yet the preview resolves to the repo directory and fills in as the
		// branch is entered — keeping the popup layout stable from the start.
		worktreePath := filepath.Join(m.worktreeRoot, owner, repo, m.worktreeBranch)
		paths = append(paths, displayPath(worktreePath, os.Getenv("HOME")))
	}
	return paths
}

func (m *model) validateWorktreeBranch() tea.Cmd {
	if m.worktreeBranch == "" {
		return nil
	}
	entries := m.worktreeEntries()
	if len(entries) == 0 {
		return nil
	}

	return func() tea.Msg {
		// Validate branch exists on origin for all selected projects
		for _, entry := range entries {
			if entry.kind != "project" {
				continue
			}
			cmd := exec.Command("git", "-C", entry.path, "ls-remote", "--heads", "origin", m.worktreeBranch)
			output, err := cmd.CombinedOutput()
			if err != nil {
				return worktreeMsg{err: fmt.Errorf("branch %q not found in origin for %s: %w", m.worktreeBranch, entry.name, err)}
			}
			if strings.TrimSpace(string(output)) == "" {
				return worktreeMsg{err: fmt.Errorf("branch %q does not exist in origin for %s", m.worktreeBranch, entry.name)}
			}

			// Check if worktree path already exists
			owner, repo := m.getRepoOwner(entry.path)
			worktreePath := filepath.Join(m.worktreeRoot, owner, repo, m.worktreeBranch)
			if _, err := os.Stat(worktreePath); err == nil {
				return worktreeMsg{err: fmt.Errorf("worktree already exists at %s", displayPath(worktreePath, os.Getenv("HOME")))}
			}
		}
		// Validation successful - return nil error to indicate valid
		return worktreeMsg{err: nil}
	}

}
func (m *model) createWorktree() tea.Cmd {
	entries := m.worktreeEntries()
	return func() tea.Msg {
		for _, entry := range entries {
			if entry.kind != "project" {
				continue
			}
			owner, repo := m.getRepoOwner(entry.path)
			worktreePath := filepath.Join(m.worktreeRoot, owner, repo, m.worktreeBranch)

			// Create worktree using the branch from origin
			cmd := exec.Command("git", "-C", entry.path, "worktree", "add", worktreePath, "origin/"+m.worktreeBranch)
			output, err := cmd.CombinedOutput()
			if err != nil {
				return worktreeMsg{err: fmt.Errorf("failed to create worktree for %s: %w\n%s", entry.name, err, output)}
			}

			// Add to zoxide
			_ = run(m.zoxide, "add", "--", worktreePath)
		}

		// Return success - worktrees are created and added to zoxide
		return worktreeMsg{err: nil}
	}
}
func (m *model) getDefaultBranch(repoPath string) (string, error) {
	cmd := exec.Command("git", "-C", repoPath, "symbolic-ref", "refs/remotes/origin/HEAD")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to determine default branch: %w", err)
	}
	ref := strings.TrimSpace(string(output))
	if strings.HasPrefix(ref, "refs/remotes/origin/") {
		return strings.TrimPrefix(ref, "refs/remotes/origin/"), nil
	}
	return "main", nil
}

func (m *model) getRepoOwner(repoPath string) (owner, repo string) {
	cmd := exec.Command("git", "-C", repoPath, "remote", "get-url", "origin")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "user", filepath.Base(repoPath)
	}
	url := strings.TrimSpace(string(output))

	// Parse GitHub URL: git@github.com:owner/repo.git or https://github.com/owner/repo.git
	if strings.HasPrefix(url, "git@github.com:") {
		parts := strings.TrimPrefix(url, "git@github.com:")
		parts = strings.TrimSuffix(parts, ".git")
		slashes := strings.Split(parts, "/")
		if len(slashes) >= 2 {
			return slashes[0], slashes[1]
		}
	}
	if strings.HasPrefix(url, "https://github.com/") {
		parts := strings.TrimPrefix(url, "https://github.com/")
		parts = strings.TrimSuffix(parts, ".git")
		slashes := strings.Split(parts, "/")
		if len(slashes) >= 2 {
			return slashes[0], slashes[1]
		}
	}
	// Fallback
	return "user", filepath.Base(repoPath)
}

func (m model) selectedPullRequest() (string, string) {
	if len(m.rows) == 0 || m.cursor < 0 || m.cursor >= len(m.rows) {
		return "", ""
	}
	selected := m.rows[m.cursor]
	entry := m.entries[selected.entryIndex]
	pullRequestURL := ""
	branch := entry.pathPR.Branch
	switch {
	case selected.section == "wt-item":
		worktrees := m.worktreesForRow(selected)
		if selected.wt >= 0 && selected.wt < len(worktrees) {
			pullRequestURL = worktrees[selected.wt].prURL
			branch = worktrees[selected.wt].branch
		}
	case selected.windowIndex >= 0:
		info := entry.tabs[selected.tabIndex].windows[selected.windowIndex].pathPR
		pullRequestURL, branch = info.PullRequest.URL, info.Branch
	case selected.tabIndex >= 0:
		for _, window := range entry.tabs[selected.tabIndex].windows {
			if window.pathPR.PullRequest.URL != "" {
				pullRequestURL, branch = window.pathPR.PullRequest.URL, window.pathPR.Branch
				break
			}
		}
	default:
		pullRequestURL = entry.pathPR.PullRequest.URL
	}
	return pullRequestURL, branch
}

func (m *model) openWorktreePR() tea.Cmd {
	pullRequestURL, branch := m.selectedPullRequest()
	parsed, err := url.Parse(pullRequestURL)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
		if branch == "" {
			branch = "selected row"
		}
		m.err = fmt.Errorf("no matching pull request for %s", branch)
		return nil
	}
	m.err = nil
	return func() tea.Msg {
		return openPRMsg{err: openURL(pullRequestURL)}
	}
}

// beginWorktreeCreate opens the create-worktree form for the project whose
// Worktree tab is open. While the tab is active, worktreeEntries() resolves
// that project, so the paths, recipe, and validation pipeline target it
// unchanged. The form overlays the tab; cancelling returns there because the
// filter is left on filterWorktrees.
func (m *model) beginWorktreeCreate() tea.Cmd {
	if len(m.worktreeEntries()) == 0 {
		m.err = fmt.Errorf("no project in this worktree tab")
		return nil
	}
	m.mode = modeWorktreeCreate
	m.worktreeBranch = ""
	m.worktreePaths = m.calculateWorktreePaths()
	m.worktreeRecipe = nil
	m.worktreeRecipePath = ""
	m.worktreeRecipeMode = ""
	m.worktreeCustomWorkspaces = false
	entries := m.worktreeEntries()
	if len(entries) == 1 && entries[0].path != "" {
		recipe, recipePath, err := loadWktreeRecipe(entries[0].path)
		if err != nil {
			m.mode = modeNormal
			m.err = err
			return nil
		}
		m.worktreeRecipe, m.worktreeRecipePath = recipe, recipePath
		if recipe != nil {
			m.worktreeRecipeMode = recipe.WorkspaceMode
			m.ensureWorktreeSelection()
		}
	}
	m.err = nil
	return nil
}

func prStatusCachePath() string {
	cacheHome := os.Getenv("XDG_CACHE_HOME")
	if cacheHome == "" {
		cacheHome = filepath.Join(os.Getenv("HOME"), ".cache")
	}
	return filepath.Join(cacheHome, "kesh", "pr-status.json")
}

func prStatusKey(branch, head string) string {
	return branch + "\x00" + head
}

func matchPullRequest(pullRequests map[string]prInfo, branch, head string) (prInfo, bool) {
	if pullRequest, ok := pullRequests[prStatusKey(branch, head)]; ok {
		return pullRequest, true
	}
	var latest prInfo
	for key, pullRequest := range pullRequests {
		parts := strings.SplitN(key, "\x00", 2)
		if len(parts) == 2 && parts[0] == branch && pullRequest.Number > latest.Number {
			latest = pullRequest
		}
	}
	return latest, false
}

func repositoryCacheKey(dir string) string {
	output, err := exec.Command("git", "-C", dir, "remote", "get-url", "origin").Output()
	if err == nil && strings.TrimSpace(string(output)) != "" {
		remote := strings.TrimSpace(string(output))
		if parsed, parseErr := url.Parse(remote); parseErr == nil && parsed.Scheme != "" {
			parsed.User = nil
			remote = parsed.String()
		}
		return remote
	}
	output, err = exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func loadPRStatusCache(repoKey string) (map[string]prInfo, time.Time) {
	if repoKey == "" {
		return nil, time.Time{}
	}
	content, err := os.ReadFile(prStatusCachePath())
	if err != nil {
		return nil, time.Time{}
	}
	var store prStatusCacheStore
	if json.Unmarshal(content, &store) != nil || store.Version != prStatusCacheVersion {
		return nil, time.Time{}
	}
	repository, ok := store.Repositories[repoKey]
	if !ok {
		return nil, time.Time{}
	}
	fetchedAt, _ := time.Parse(time.RFC3339, repository.FetchedAt)
	pullRequests := map[string]prInfo{}
	for _, entry := range repository.Entries {
		pullRequests[prStatusKey(entry.Branch, entry.Head)] = prInfo{Status: entry.Status, URL: entry.URL, Number: entry.Number}
	}
	return pullRequests, fetchedAt
}

func savePRStatusCache(repoKey string, pullRequests map[string]prInfo) error {
	path := prStatusCachePath()
	store := prStatusCacheStore{Version: prStatusCacheVersion, Repositories: map[string]prStatusRepositoryCache{}}
	if content, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(content, &store)
		if store.Version != prStatusCacheVersion || store.Repositories == nil {
			store = prStatusCacheStore{Version: prStatusCacheVersion, Repositories: map[string]prStatusRepositoryCache{}}
		}
	}
	entries := make([]prStatusCacheEntry, 0, len(pullRequests))
	for key, pullRequest := range pullRequests {
		parts := strings.SplitN(key, "\x00", 2)
		if len(parts) == 2 {
			entries = append(entries, prStatusCacheEntry{
				Branch: parts[0], Head: parts[1], Status: pullRequest.Status,
				URL: pullRequest.URL, Number: pullRequest.Number,
			})
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Branch == entries[j].Branch {
			return entries[i].Head < entries[j].Head
		}
		return entries[i].Branch < entries[j].Branch
	})
	store.Repositories[repoKey] = prStatusRepositoryCache{FetchedAt: time.Now().UTC().Format(time.RFC3339), Entries: entries}
	content, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".pr-status-*.json")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryName, path)
}

func queryPRStatuses(dir string) (string, map[string]prInfo, error) {
	repoKey := repositoryCacheKey(dir)
	if repoKey == "" {
		return "", nil, fmt.Errorf("repository has no cache key")
	}
	gh := findCommand("gh",
		filepath.Join(os.Getenv("HOME"), ".local", "share", "mise", "shims", "gh"),
		"/opt/homebrew/bin/gh",
		"/usr/local/bin/gh",
	)
	if gh == "" {
		return repoKey, nil, fmt.Errorf("gh was not found")
	}
	command := exec.Command(gh, "pr", "list", "--state", "all", "--limit", "1000", "--json", "headRefName,headRefOid,state,mergedAt,number,url")
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		return repoKey, nil, commandError("gh pr list", output, err)
	}
	var pullRequests []struct {
		HeadRefName string  `json:"headRefName"`
		HeadRefOID  string  `json:"headRefOid"`
		State       string  `json:"state"`
		MergedAt    *string `json:"mergedAt"`
		Number      int     `json:"number"`
		URL         string  `json:"url"`
	}
	if err := json.Unmarshal(output, &pullRequests); err != nil {
		return repoKey, nil, err
	}
	statuses := map[string]prInfo{}
	priority := map[string]int{"closed": 1, "open": 2, "merged": 3}
	for _, pullRequest := range pullRequests {
		if pullRequest.HeadRefName == "" || pullRequest.HeadRefOID == "" {
			continue
		}
		status := strings.ToLower(pullRequest.State)
		if pullRequest.MergedAt != nil {
			status = "merged"
		}
		key := prStatusKey(pullRequest.HeadRefName, pullRequest.HeadRefOID)
		if priority[status] > priority[statuses[key].Status] {
			statuses[key] = prInfo{Status: status, URL: pullRequest.URL, Number: pullRequest.Number}
		}
	}
	_ = savePRStatusCache(repoKey, statuses)
	return repoKey, statuses, nil
}

func (m *model) refreshPRStatuses(dir string, force bool) tea.Cmd {
	repoKey := repositoryCacheKey(dir)
	if repoKey == "" {
		return nil
	}
	if m.prStatusPending == nil {
		m.prStatusPending = map[string]bool{}
	}
	if m.prStatusPending[repoKey] {
		return nil
	}
	if !force {
		cachedStatuses, cachedAt := loadPRStatusCache(repoKey)
		focusedWorktree := m.focusedWorktreePath()
		m.applyPRStatuses(repoKey, cachedStatuses)
		m.restoreFocusedWorktree(focusedWorktree)
		lastFetch := cachedAt
		if fetched := m.prStatusLastFetch[repoKey]; fetched.After(lastFetch) {
			lastFetch = fetched
		}
		if !lastFetch.IsZero() && time.Since(lastFetch) < prStatusThrottle {
			return nil
		}
	}
	m.prStatusPending[repoKey] = true
	return func() tea.Msg {
		key, pullRequests, err := queryPRStatuses(dir)
		if key == "" {
			key = repoKey
		}
		return prStatusMsg{repoKey: key, pullRequests: pullRequests, err: err}
	}
}

func worktreePriority(worktree worktreeItem) int {
	if worktree.isDefault {
		return 0
	}
	switch worktree.prStatus {
	case "open":
		return 1
	case "merged":
		return 2
	case "closed":
		return 3
	default:
		return 4
	}
}

func sortWorktreeItems(worktrees []worktreeItem) {
	sort.SliceStable(worktrees, func(i, j int) bool {
		left, right := worktreePriority(worktrees[i]), worktreePriority(worktrees[j])
		if left != right {
			return left < right
		}
		return strings.ToLower(worktrees[i].branch) < strings.ToLower(worktrees[j].branch)
	})
}

func (m *model) applyPRStatuses(repoKey string, pullRequests map[string]prInfo) {
	apply := func(worktrees []worktreeItem) {
		for index := range worktrees {
			if worktrees[index].prRepoKey == repoKey {
				pullRequest, exact := matchPullRequest(pullRequests, worktrees[index].branch, worktrees[index].head)
				worktrees[index].prStatus = pullRequest.Status
				worktrees[index].prURL = pullRequest.URL
				worktrees[index].prNumber = pullRequest.Number
				worktrees[index].prExact = exact
			}
		}
		sortWorktreeItems(worktrees)
	}
	applyPath := func(info *pathPRInfo) {
		if info.RepoKey == repoKey && info.Branch != "" {
			info.PullRequest, info.Exact = matchPullRequest(pullRequests, info.Branch, info.Head)
		}
	}
	for index := range m.entries {
		apply(m.entries[index].worktrees)
		applyPath(&m.entries[index].pathPR)
		for tabIndex := range m.entries[index].tabs {
			for windowIndex := range m.entries[index].tabs[tabIndex].windows {
				window := &m.entries[index].tabs[tabIndex].windows[windowIndex]
				apply(window.worktrees)
				applyPath(&window.pathPR)
			}
		}
	}
}

// findMergedWorktrees finds non-current worktrees whose branches are either
// ancestors of the repository's current HEAD or are the exact head of a merged
// GitHub pull request. Capital X confirms once; dirty worktrees are never forced.
func (m *model) findMergedWorktrees() tea.Cmd {
	if len(m.rows) == 0 {
		return nil
	}
	selected := m.rows[m.cursor]
	dir := m.worktreeDirectory(selected)
	if dir == "" {
		m.err = fmt.Errorf("place the cursor on a window or closed project")
		return nil
	}
	m.mergedWorktreeBusy = true
	m.err = nil
	return func() tea.Msg {
		worktreeOutput, err := exec.Command("git", "-C", dir, "worktree", "list", "--porcelain").CombinedOutput()
		if err != nil {
			return mergedWorktreeListMsg{selected: selected, dir: dir, err: commandError("git worktree list", worktreeOutput, err)}
		}
		mergedOutput, err := exec.Command("git", "-C", dir, "branch", "--merged", "HEAD", "--format=%(refname:short)").CombinedOutput()
		if err != nil {
			return mergedWorktreeListMsg{selected: selected, dir: dir, err: commandError("git branch --merged", mergedOutput, err)}
		}
		currentOutput, err := exec.Command("git", "-C", dir, "branch", "--show-current").CombinedOutput()
		if err != nil {
			return mergedWorktreeListMsg{selected: selected, dir: dir, err: commandError("git branch --show-current", currentOutput, err)}
		}
		return mergedWorktreeListMsg{
			selected: selected,
			dir:      dir,
			worktrees: mergedWorktreeItems(
				parseWorktreePorcelain(string(worktreeOutput)),
				string(mergedOutput),
				strings.TrimSpace(string(currentOutput)),
				mergedPullRequestHeads(dir),
			),
		}
	}
}

func mergedWorktreeItems(worktrees []worktreeItem, mergedOutput, currentBranch string, pullRequestHeads map[string]map[string]bool) []worktreeItem {
	merged := map[string]bool{}
	for _, branch := range strings.Fields(mergedOutput) {
		merged[branch] = true
	}
	var result []worktreeItem
	for index, worktree := range worktrees {
		// The first record is the repository's primary working tree. Never bulk
		// remove it, even when the command is run from another worktree.
		if index == 0 || worktree.branch == "" || worktree.branch == "(detached)" || worktree.branch == currentBranch {
			continue
		}
		mergedPullRequest := pullRequestHeads[worktree.branch][worktree.head]
		if !merged[worktree.branch] && !mergedPullRequest {
			continue
		}
		result = append(result, worktree)
	}
	return result
}

// mergedPullRequestHeads supplements Git's ancestry check for squash- and
// rebase-merged pull requests. A branch is accepted only when its current
// worktree HEAD exactly matches the head recorded by GitHub, preventing a reused
// branch with newer unmerged commits from being removed. Git remains the
// fallback when gh is unavailable, unauthenticated, or used outside GitHub.
func mergedPullRequestHeads(dir string) map[string]map[string]bool {
	_, statuses, err := queryPRStatuses(dir)
	if err != nil {
		return nil
	}
	heads := map[string]map[string]bool{}
	for key, pullRequest := range statuses {
		if pullRequest.Status != "merged" {
			continue
		}
		parts := strings.SplitN(key, "\x00", 2)
		if len(parts) != 2 {
			continue
		}
		if heads[parts[0]] == nil {
			heads[parts[0]] = map[string]bool{}
		}
		heads[parts[0]][parts[1]] = true
	}
	return heads
}

func commandError(action string, output []byte, err error) error {
	message := strings.TrimSpace(string(output))
	if message != "" {
		err = fmt.Errorf("%s: %s", err, message)
	}
	return fmt.Errorf("%s: %w", action, err)
}

func (m *model) runRemoveMergedWorktrees() tea.Cmd {
	selected := m.closeRow
	dir := m.worktreeDirectory(selected)
	targets := append([]worktreeItem(nil), m.mergedWorktrees...)
	return func() tea.Msg {
		var failures []string
		for _, target := range targets {
			output, err := exec.Command("git", "-C", dir, "worktree", "remove", target.path).CombinedOutput()
			if err != nil {
				failures = append(failures, commandError(target.branch, output, err).Error())
			}
		}
		if len(failures) > 0 {
			return mergedWorktreeRemoveMsg{selected: selected, dir: dir, err: fmt.Errorf("some merged worktrees were not removed: %s", strings.Join(failures, "; "))}
		}
		return mergedWorktreeRemoveMsg{selected: selected, dir: dir}
	}
}

// runRemoveWorktrees removes every bulk-selected worktree in the Worktree tab,
// mirroring runRemoveMergedWorktrees. Non-force: a target with open Kitty
// windows or uncommitted changes fails for that one and is named in the
// summary; the rest are still removed.
func (m *model) runRemoveWorktrees() tea.Cmd {
	selected := m.closeRow
	dir := m.worktreeDirectory(selected)
	targets := append([]worktreeItem(nil), m.bulkWorktrees...)
	entryIndex := selected.entryIndex
	return func() tea.Msg {
		var failures []string
		for _, target := range targets {
			output, err := exec.Command("git", "-C", dir, "worktree", "remove", target.path).CombinedOutput()
			if err != nil {
				failures = append(failures, commandError(target.branch, output, err).Error())
			}
		}
		if len(failures) > 0 {
			return bulkWorktreeRemoveMsg{entryIndex: entryIndex, dir: dir, err: fmt.Errorf("some worktrees were not removed: %s", strings.Join(failures, "; "))}
		}
		return bulkWorktreeRemoveMsg{entryIndex: entryIndex, dir: dir}
	}
}

func (m *model) worktreeDirectory(r row) string {
	if m.filter == filterWorktrees && r.entryIndex >= 0 && r.entryIndex < len(m.entries) {
		// The tab lists one project's worktrees regardless of whether it is
		// open; closedEntryAt skips open entries, so resolve the repo from the
		// tab's project directly.
		return m.entries[r.entryIndex].path
	}
	if w := m.windowAt(r.entryIndex, r.tabIndex, r.windowIndex); w != nil {
		return w.cwd
	}
	if e := m.closedEntryAt(r.entryIndex, r.tabIndex, r.windowIndex); e != nil {
		return e.path
	}
	return ""
}

func (m *model) invalidateWorktrees(r row) {
	if w := m.windowAt(r.entryIndex, r.tabIndex, r.windowIndex); w != nil {
		w.worktreesLoaded = false
		w.worktreesOpen = false
		w.worktreesPending = false
		return
	}
	if e := m.closedEntryAt(r.entryIndex, r.tabIndex, r.windowIndex); e != nil {
		e.worktreesLoaded = false
		e.worktreesOpen = false
		e.worktreesPending = false
	}
}

// worktreeWindowIDs returns live Kitty windows rooted in target (including a
// child directory). Querying Kitty here, rather than relying on the picker
// snapshot, prevents deleting a worktree opened after Kesh started.
func worktreeWindowIDs(kitty, target string) ([]int, error) {
	output, err := exec.Command(kitty, "@", "ls").Output()
	if err != nil {
		return nil, fmt.Errorf("kitty @ ls: %w", err)
	}
	var state kittyState
	if err := json.Unmarshal(output, &state); err != nil {
		return nil, fmt.Errorf("decode kitty state: %w", err)
	}
	target = filepath.Clean(target)
	prefix := target + string(filepath.Separator)
	var ids []int
	for _, osWindow := range state {
		for _, tab := range osWindow.Tabs {
			for _, window := range tab.Windows {
				cwd := filepath.Clean(window.CWD)
				if cwd == target || strings.HasPrefix(cwd, prefix) {
					ids = append(ids, window.ID)
				}
			}
		}
	}
	return ids, nil
}

// destroyPlan describes the layers a unified Destroy (D) removes for one
// focused entry. Only non-empty/true layers are touched.
type destroyPlan struct {
	entryName    string
	closeSession bool // close the kitty session tabs
	tabCount     int
	worktreePath string // linked worktree dir to remove; "" leaves the folder alone
	branch       string // local branch to delete; "" deletes none
	saved        bool   // delete the saved-session record + snapshot
}

type destroyMsg struct {
	entries []entry
	err     error
}

// linkedWorktreeBranch reports whether dir is a git linked worktree (its .git
// is a file, not a directory) and returns the checked-out branch. The main
// checkout and plain directories return ok=false so Destroy never deletes them.
func linkedWorktreeBranch(dir string) (branch string, ok bool) {
	info, err := os.Stat(filepath.Join(dir, ".git"))
	if err != nil || info.IsDir() {
		return "", false
	}
	output, err := exec.Command("git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return "", true // linked worktree whose branch can't be resolved: drop dir, skip branch
	}
	branch = strings.TrimSpace(string(output))
	if branch == "" || branch == "HEAD" {
		return "", true // detached HEAD: remove dir, skip branch
	}
	return branch, true
}

// worktreeMainPath returns the main worktree path for the repo containing dir,
// so a worktree is never removed from within itself. It falls back to dir.
func worktreeMainPath(dir string) string {
	// A stale linked worktree may no longer exist. Walk its parents to find the
	// repository that still owns its metadata, then remove it from there.
	candidate := dir
	for {
		output, err := exec.Command("git", "-C", candidate, "worktree", "list", "--porcelain").Output()
		if err == nil {
			for _, raw := range strings.Split(string(output), "\n") {
				line := strings.TrimSpace(raw)
				if strings.HasPrefix(line, "worktree ") {
					return strings.TrimSpace(strings.TrimPrefix(line, "worktree "))
				}
			}
			return candidate
		}
		parent := filepath.Dir(candidate)
		if parent == candidate {
			return dir
		}
		candidate = parent
	}
}

// detectDestroyPlan builds the destroy plan for an entry. Folder and branch
// removal are restricted to single project entries that resolve to a linked
// worktree; composed workspaces and plain directories only close/release.
func detectDestroyPlan(e entry) destroyPlan {
	plan := destroyPlan{entryName: e.name, saved: e.saved}
	if e.open {
		plan.closeSession = len(e.tabs) > 0
		plan.tabCount = len(e.tabs)
	}
	if e.kind == "project" && e.path != "" {
		if branch, ok := linkedWorktreeBranch(e.path); ok {
			plan.worktreePath = e.path
			plan.branch = branch
		}
	}
	return plan
}

// runDestroy cascades a destroy plan: close the kitty session, close any
// windows still inside the worktree, force-removes the worktree (including
// local changes and untracked files), deletes the local branch, and deletes the
// saved record. Worktree removal runs from the repo's main worktree so a
// worktree is never removed from within itself.
func runDestroy(kitty, zoxide string, e entry, plan destroyPlan) tea.Cmd {
	return func() tea.Msg {
		if plan.closeSession {
			if args, err := closeArgs(e, row{tabIndex: -1, windowIndex: -1}); err == nil {
				if err := run(kitty, args...); err != nil {
					return destroyMsg{err: fmt.Errorf("close session: %w", err)}
				}
			}
		}
		if plan.worktreePath != "" {
			ids, _ := worktreeWindowIDs(kitty, plan.worktreePath)
			for _, id := range ids {
				if err := run(kitty, "@", "close-window", "--match", "id:"+strconv.Itoa(id)); err != nil {
					return destroyMsg{err: fmt.Errorf("close worktree window %d: %w", id, err)}
				}
			}
			repoDir := worktreeMainPath(plan.worktreePath)
			remove, rerr := exec.Command("git", "-C", repoDir, "worktree", "remove", "--force", plan.worktreePath).CombinedOutput()
			if rerr != nil {
				return destroyMsg{err: commandError("git worktree remove", remove, rerr)}
			}
			if plan.branch != "" {
				bout, berr := exec.Command("git", "-C", repoDir, "branch", "-D", "--", plan.branch).CombinedOutput()
				if berr != nil {
					return destroyMsg{err: commandError("git branch -D", bout, berr)}
				}
			}
		}
		if plan.saved {
			if err := deleteSavedSession(e); err != nil {
				return destroyMsg{err: err}
			}
		}
		entries, err := loadEntries(kitty, zoxide)
		return destroyMsg{entries: entries, err: err}
	}
}

// runRemoveWorktree deletes the selected worktree via git. It first protects
// live Kitty windows from being left in a deleted working directory; force
// removal closes those windows, then removes the worktree.
func (m *model) runRemoveWorktree(force bool) tea.Cmd {
	r := m.closeRow
	target, ok := m.worktreeForRow(r)
	if !ok {
		return func() tea.Msg {
			return worktreeRemoveMsg{forceTried: force, err: fmt.Errorf("worktree is no longer available")}
		}
	}
	targetPath := target.path
	// The parent entry can be a stale saved or pruned worktree. Resolve the
	// repository from the worktree being removed instead of running Git from
	// that parent path.
	repoDir := worktreeMainPath(targetPath)
	entryIndex, tabIndex, windowIndex := r.entryIndex, r.tabIndex, r.windowIndex
	return func() tea.Msg {
		windowIDs, windowErr := worktreeWindowIDs(m.kitty, targetPath)
		if windowErr != nil {
			return worktreeRemoveMsg{entryIndex: entryIndex, tabIndex: tabIndex, windowIndex: windowIndex, forceTried: force, err: windowErr}
		}
		if len(windowIDs) > 0 && !force {
			return worktreeRemoveMsg{entryIndex: entryIndex, tabIndex: tabIndex, windowIndex: windowIndex, err: fmt.Errorf("%d Kitty window(s) are open in this worktree; press f to close them and force-remove", len(windowIDs))}
		}
		if force {
			for _, id := range windowIDs {
				if err := run(m.kitty, "@", "close-window", "--match", "id:"+strconv.Itoa(id)); err != nil {
					return worktreeRemoveMsg{entryIndex: entryIndex, tabIndex: tabIndex, windowIndex: windowIndex, forceTried: true, err: fmt.Errorf("close Kitty window %d: %w", id, err)}
				}
			}
		}
		args := []string{"-C", repoDir, "worktree", "remove"}
		if force {
			args = append(args, "--force")
		}
		args = append(args, targetPath)
		output, err := exec.Command("git", args...).CombinedOutput()
		if err != nil {
			message := strings.TrimSpace(string(output))
			if message != "" {
				err = fmt.Errorf("%s: %s", err, message)
			}
			return worktreeRemoveMsg{entryIndex: entryIndex, tabIndex: tabIndex, windowIndex: windowIndex, forceTried: force, err: fmt.Errorf("git worktree remove: %w", err)}
		}
		return worktreeRemoveMsg{entryIndex: entryIndex, tabIndex: tabIndex, windowIndex: windowIndex, forceTried: force}
	}
}

// windowAt returns a pointer to the window at the given coordinates, or nil if
// any index is out of range or the coordinates do not address a window.
func (m *model) windowAt(entryIndex, tabIndex, windowIndex int) *windowItem {
	if entryIndex < 0 || entryIndex >= len(m.entries) {
		return nil
	}
	tabs := m.entries[entryIndex].tabs
	if tabIndex < 0 || tabIndex >= len(tabs) {
		return nil
	}
	windows := tabs[tabIndex].windows
	if windowIndex < 0 || windowIndex >= len(windows) {
		return nil
	}
	return &windows[windowIndex]
}

// closedEntryAt returns an entry-level worktree target only for a closed entry.
// Open entries must be inspected at window level to avoid guessing which repo a
// multi-tab session represents.
func (m *model) closedEntryAt(entryIndex, tabIndex, windowIndex int) *entry {
	if tabIndex >= 0 || windowIndex >= 0 || entryIndex < 0 || entryIndex >= len(m.entries) {
		return nil
	}
	e := &m.entries[entryIndex]
	if e.open {
		return nil
	}
	return e
}

func (m model) selectedWorktree() (worktreeItem, bool) {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return worktreeItem{}, false
	}
	selected := m.rows[m.cursor]
	if selected.section != "wt-item" {
		return worktreeItem{}, false
	}
	worktrees := m.worktreesForRow(selected)
	if selected.wt < 0 || selected.wt >= len(worktrees) {
		return worktreeItem{}, false
	}
	return worktrees[selected.wt], true
}

// selectedWorktreeItems resolves the bulk-selected worktree paths to their
// worktreeItems via the tab project's full worktree list, so a search filter
// does not silently drop selections from a bulk action. Stale paths (worktrees
// removed since selection) are skipped.
func (m model) selectedWorktreeItems() []worktreeItem {
	if len(m.wtBulkSelected) == 0 {
		return nil
	}
	if m.worktreeFilterEntryIndex < 0 || m.worktreeFilterEntryIndex >= len(m.entries) {
		return nil
	}
	var items []worktreeItem
	for _, wt := range m.entries[m.worktreeFilterEntryIndex].worktrees {
		if m.wtBulkSelected[wt.path] {
			items = append(items, wt)
		}
	}
	return items
}

func (m model) focusedWorktreePath() string {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return ""
	}
	selected := m.rows[m.cursor]
	if selected.section == "wt-filter" && selected.wt >= 0 && selected.wt < len(m.worktreeFilterRows) {
		return m.worktreeFilterRows[selected.wt].worktree.path
	}
	if selected.section != "wt-item" {
		return ""
	}
	worktrees := m.worktreesForRow(selected)
	if selected.wt < 0 || selected.wt >= len(worktrees) {
		return ""
	}
	return worktrees[selected.wt].path
}

func (m *model) restoreFocusedWorktree(path string) {
	if path == "" {
		return
	}
	for index, candidate := range m.rows {
		if candidate.section == "wt-filter" && candidate.wt >= 0 && candidate.wt < len(m.worktreeFilterRows) && m.worktreeFilterRows[candidate.wt].worktree.path == path {
			m.cursor = index
			return
		}
		if candidate.section != "wt-item" {
			continue
		}
		worktrees := m.worktreesForRow(candidate)
		if candidate.wt >= 0 && candidate.wt < len(worktrees) && worktrees[candidate.wt].path == path {
			m.cursor = index
			return
		}
	}
}

func (m model) worktreesForRow(r row) []worktreeItem {
	if r.tabIndex < 0 {
		if r.entryIndex >= 0 && r.entryIndex < len(m.entries) {
			return m.entries[r.entryIndex].worktrees
		}
		return nil
	}
	if r.entryIndex < 0 || r.entryIndex >= len(m.entries) || r.tabIndex >= len(m.entries[r.entryIndex].tabs) || r.windowIndex < 0 {
		return nil
	}
	windows := m.entries[r.entryIndex].tabs[r.tabIndex].windows
	if r.windowIndex >= len(windows) {
		return nil
	}
	return windows[r.windowIndex].worktrees
}

// worktreeForRow resolves the single worktree addressed by r. Rows built for the
// Worktree tab carry worktreePath directly: the tab list can be a filtered,
// re-sorted subset of the entry's worktrees, so indexing e.worktrees by r.wt
// would target the wrong tree. Inline wt-item rows resolve by index into the
// entry or window worktree list instead.
func (m model) worktreeForRow(r row) (worktreeItem, bool) {
	if r.worktreePath != "" {
		for _, wt := range m.worktreesForRow(r) {
			if wt.path == r.worktreePath {
				return wt, true
			}
		}
		// The list may have been refetched between the keypress and this call;
		// the path is still valid for removal, so synthesize a minimal item.
		return worktreeItem{path: r.worktreePath, branch: filepath.Base(r.worktreePath)}, true
	}
	worktrees := m.worktreesForRow(r)
	if r.wt < 0 || r.wt >= len(worktrees) {
		return worktreeItem{}, false
	}
	return worktrees[r.wt], true
}

func fetchWorktrees(dir string, entryIndex, tabIndex, windowIndex int) tea.Cmd {
	return func() tea.Msg {
		output, err := exec.Command("git", "-C", dir, "worktree", "list", "--porcelain").CombinedOutput()
		if err != nil {
			message := strings.TrimSpace(string(output))
			if message != "" {
				err = fmt.Errorf("%s: %s", err, message)
			}
			return worktreeListMsg{entryIndex: entryIndex, tabIndex: tabIndex, windowIndex: windowIndex, dir: dir, err: fmt.Errorf("git worktree list: %w", err)}
		}
		worktrees := parseWorktreePorcelain(string(output))
		defaultBranch := ""
		if defaultOutput, defaultErr := exec.Command("git", "-C", dir, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD").Output(); defaultErr == nil {
			defaultBranch = strings.TrimPrefix(strings.TrimSpace(string(defaultOutput)), "origin/")
		}
		if defaultBranch == "" && len(worktrees) > 0 {
			defaultBranch = worktrees[0].branch
		}
		repoKey := repositoryCacheKey(dir)
		cachedStatuses, _ := loadPRStatusCache(repoKey)
		for i := range worktrees {
			if worktrees[i].path == dir {
				worktrees[i].current = true
			}
			worktrees[i].isDefault = worktrees[i].branch == defaultBranch
			worktrees[i].prRepoKey = repoKey
			pullRequest, exact := matchPullRequest(cachedStatuses, worktrees[i].branch, worktrees[i].head)
			worktrees[i].prStatus = pullRequest.Status
			worktrees[i].prURL = pullRequest.URL
			worktrees[i].prNumber = pullRequest.Number
			worktrees[i].prExact = exact
			worktrees[i].dirty, worktrees[i].ahead, worktrees[i].behind, worktrees[i].changes = worktreeSyncStatus(worktrees[i].path)
		}
		sortWorktreeItems(worktrees)
		return worktreeListMsg{entryIndex: entryIndex, tabIndex: tabIndex, windowIndex: windowIndex, dir: dir, worktrees: worktrees}
	}
}

func parseWorktreePorcelain(output string) []worktreeItem {
	var items []worktreeItem
	var current *worktreeItem
	flush := func() {
		if current != nil {
			items = append(items, *current)
			current = nil
		}
	}
	for _, raw := range strings.Split(output, "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case strings.HasPrefix(line, "worktree "):
			flush()
			current = &worktreeItem{path: strings.TrimSpace(strings.TrimPrefix(line, "worktree "))}
		case current == nil:
			continue
		case strings.HasPrefix(line, "HEAD "):
			current.head = strings.TrimPrefix(line, "HEAD ")
		case strings.HasPrefix(line, "branch "):
			current.branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		case line == "detached":
			current.branch = "(detached)"
		case line == "" || strings.HasPrefix(line, "command ") || strings.HasPrefix(line, "locked") || strings.HasPrefix(line, "prune"):
			if line == "" {
				flush()
			}
		}
	}
	flush()
	return items
}

// worktreeSyncStatus reports whether a worktree has uncommitted changes and how
// far its branch has diverged from its upstream. A single `git status -sb
// --porcelain` call yields both: the tracking header carries ahead/behind, and
// any following line is an uncommitted change.
func worktreeSyncStatus(path string) (dirty bool, ahead, behind int, changes []string) {
	output, err := exec.Command("git", "-C", path, "status", "-sb", "--porcelain").Output()
	if err != nil {
		return false, 0, 0, nil
	}
	return parseWorktreeStatus(string(output))
}

// parseWorktreeStatus decodes `git status -sb --porcelain` output: the tracking
// header yields ahead/behind, and every following line is an uncommitted change
// (kept verbatim so its status column survives for the detail panel).
func parseWorktreeStatus(output string) (dirty bool, ahead, behind int, changes []string) {
	lines := strings.Split(output, "\n")
	if len(lines) == 0 {
		return false, 0, 0, nil
	}
	if header := strings.TrimSpace(lines[0]); strings.HasPrefix(header, "## ") {
		if start := strings.Index(header, "["); start >= 0 {
			ahead, behind = parseAheadBehind(header[start:])
		}
	}
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		dirty = true
		changes = append(changes, strings.TrimRight(line, "\r"))
	}
	return dirty, ahead, behind, changes
}

// parseAheadBehind decodes a "[ahead N, behind M]" tracking segment.
func parseAheadBehind(segment string) (ahead, behind int) {
	segment = strings.Trim(segment, "[]")
	for _, part := range strings.Split(segment, ",") {
		fields := strings.Fields(part)
		if len(fields) != 2 {
			continue
		}
		n, _ := strconv.Atoi(fields[1])
		switch fields[0] {
		case "ahead":
			ahead = n
		case "behind":
			behind = n
		}
	}
	return ahead, behind
}
