package app

import (
	"regexp"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/alienxp03/kesh/internal/domain"
	kittyx "github.com/alienxp03/kesh/internal/kitty"
	"github.com/alienxp03/kesh/internal/state"
	"github.com/alienxp03/kesh/internal/workspace"
)

type kittyWindow = kittyx.Window

type windowItem struct {
	id          int
	title       string
	detail      string
	cwd         string
	command     string
	fullCommand string
	agent       string
	agentStatus string
	lastFocused float64
	pathPR      pathPRInfo
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

func toDomainWorktree(item worktreeItem) domain.Worktree {
	return domain.Worktree{
		Path: item.path, Branch: item.branch, Head: item.head, Current: item.current,
		IsDefault: item.isDefault, PRStatus: item.prStatus, PRURL: item.prURL,
		PRNumber: item.prNumber, PRExact: item.prExact, PRRepoKey: item.prRepoKey,
		Dirty: item.dirty, Behind: item.behind, Ahead: item.ahead,
		Changes: append([]string(nil), item.changes...),
	}
}

func fromDomainWorktree(item domain.Worktree) worktreeItem {
	return worktreeItem{
		path: item.Path, branch: item.Branch, head: item.Head, current: item.Current,
		isDefault: item.IsDefault, prStatus: item.PRStatus, prURL: item.PRURL,
		prNumber: item.PRNumber, prExact: item.PRExact, prRepoKey: item.PRRepoKey,
		dirty: item.Dirty, behind: item.Behind, ahead: item.Ahead,
		changes: append([]string(nil), item.Changes...),
	}
}

func toDomainWorktrees(items []worktreeItem) []domain.Worktree {
	result := make([]domain.Worktree, len(items))
	for index, item := range items {
		result[index] = toDomainWorktree(item)
	}
	return result
}

func fromDomainWorktrees(items []domain.Worktree) []worktreeItem {
	result := make([]worktreeItem, len(items))
	for index, item := range items {
		result[index] = fromDomainWorktree(item)
	}
	return result
}

type worktreeFilterRow struct {
	worktree  worktreeItem
	entryName string
	entryPath string
}

type entry struct {
	key             string
	name            string
	originalName    string
	detail          string
	kind            string
	path            string
	session         string
	sessionFile     string
	saved           bool
	open            bool
	lastFocused     float64
	nameTaken       bool
	agent           string
	expanded        bool
	tabs            []tabItem
	order           int
	pin             string
	pathPR          pathPRInfo
	worktrees       []worktreeItem
	worktreesLoaded bool
}

func fromDomainEntries(entries []domain.Entry) []entry {
	result := make([]entry, len(entries))
	for index, source := range entries {
		tabs := make([]tabItem, len(source.Tabs))
		for tabIndex, sourceTab := range source.Tabs {
			windows := make([]windowItem, len(sourceTab.Windows))
			for windowIndex, sourceWindow := range sourceTab.Windows {
				windows[windowIndex] = windowItem{
					id: sourceWindow.ID, title: sourceWindow.Title, detail: sourceWindow.Detail,
					cwd: sourceWindow.CWD, command: sourceWindow.Command, fullCommand: sourceWindow.FullCommand,
					agent: sourceWindow.Agent, lastFocused: sourceWindow.LastFocused,
				}
			}
			tabs[tabIndex] = tabItem{
				id: sourceTab.ID, title: sourceTab.Title, detail: sourceTab.Detail,
				agent: sourceTab.Agent, windows: windows,
			}
		}
		result[index] = entry{
			key: source.Key, name: source.Name, originalName: source.OriginalName, detail: source.Detail,
			kind: source.Kind, path: source.Path, session: source.Session, sessionFile: source.SessionFile,
			saved: source.Saved, open: source.Open, lastFocused: source.LastFocused,
			nameTaken: source.NameTaken, agent: source.Agent, tabs: tabs, order: source.Order,
		}
	}
	return result
}

type row struct {
	entryIndex   int
	tabIndex     int
	windowIndex  int
	section      string // "" or "wt-filter"
	wt           int    // index into worktreeFilterRows for "wt-filter" rows
	worktreePath string // for worktree filter mode
}

type actionMsg struct{ err error }
type openPRMsg struct{ err error }

type worktreeOpenMsg struct {
	entryKey  string
	path      string
	windowIDs []int
	err       error
}

type closeMsg struct {
	entries         []entry
	deletedSavedKey string
	err             error
}

type previewMsg struct {
	windowID int
	request  uint64
	content  string
	err      error
}

type previewRefreshMsg struct {
	windowID int
	request  uint64
}

type agentStatusTickMsg struct{}
type agentSpinnerTickMsg struct{}

type agentLifecycleStatus struct {
	tool   string
	status string
}

type agentStatusMsg struct {
	statuses map[int]agentLifecycleStatus
	err      error
}

const (
	currentPinVersion          = state.CurrentPinVersion
	currentSavedSessionVersion = state.CurrentSavedSessionVersion
	previewRefreshInterval     = time.Second
	agentStatusRefreshInterval = time.Second
	agentSpinnerInterval       = 120 * time.Millisecond
)

type pinTarget = state.PinTarget
type pinStore = state.Pins
type savedSessionRecord = state.SavedSessionRecord
type savedSessionStore = state.SavedSessions
type nameStore = state.Names

type renameMsg struct {
	selected row
	target   renameTarget
	title    string
	names    nameStore
	err      error
}

type renameTarget struct {
	entryKey string
	tabID    int
	windowID int
}

type createMsg struct{ err error }

type cloneMsg struct{ err error }

type prCheckoutMsg struct{ err error }

type prCheckoutValidationMsg struct {
	owner    string
	repo     string
	number   int
	repoPath string
	err      error
}

type prPreviewMsg struct {
	value    string
	branch   string
	repoPath string
	newClone bool
}

type saveSessionMsg struct {
	entryIndex int
	entryKey   string
	record     savedSessionRecord
	err        error
}

type pinsPersistMsg struct {
	pins      pinStore
	finishPin bool
	err       error
}

type worktreeMsg struct {
	err        error
	validation bool
}

type worktreeRecipeMsg struct {
	projectPath  string
	recipe       *workspace.Config
	recipePath   string
	repositories map[string]repoIdentity
	err          error
}

type worktreeListMsg struct {
	entryIndex  int
	tabIndex    int
	windowIndex int
	dir         string
	worktrees   []worktreeItem
	err         error
}

type worktreeSyncState struct {
	dirty   bool
	ahead   int
	behind  int
	changes []string
}

type worktreeSyncMsg struct {
	dir      string
	statuses map[string]worktreeSyncState
}

type worktreeRemoveMsg struct {
	dir         string
	targetPath  string
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
	selected   row
	dir        string
	remaining  []worktreeItem
	forceTried bool
	err        error
}

type bulkWorktreeRemoveMsg struct {
	entryIndex int
	dir        string
	err        error
}

type prInfo = domain.PullRequest

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
	fetchedAt    time.Time
	err          error
}

type prStatusCacheMsg struct {
	dir          string
	repoKey      string
	pullRequests map[string]prInfo
	cachedAt     time.Time
	now          time.Time
	force        bool
	err          error
}

type pathPRMsg struct {
	path string
	info pathPRInfo
}

type zoxideMergeContext struct {
	livePaths    map[string]bool
	merged       map[string]bool
	sessionNames map[string]bool
	home         string
	openTabs     map[string]domain.OpenTabState
}

type renameForm struct {
	renameValue string
}

type searchMode struct{}

type createSessionForm struct {
	createValue string
}

type cloneForm struct {
	cloneRoot              string
	cloneBusy              bool
	cloneDestinationFocus  bool
	cloneDestinationEdited bool
	cloneRepository        string
	cloneDestination       string
}

type checkoutForm struct {
	prCheckoutBusy       bool
	prCheckoutValue      string
	prCheckoutBranch     string
	prCheckoutPath       string
	prCheckoutPathFocus  bool
	prCheckoutPathEdited bool
	prCheckoutClone      bool
	checkoutRoot         string
	checkoutCloneRoot    string
}

type helpMode struct {
	helpQuery     string
	helpSearching bool
}

type pinMode struct {
	pinEntry    int
	confirmSlot string
	pinBusy     bool
}

type saveConfirmation struct {
	saveForeground bool
	saveEntry      int
	saveName       string
}

type closeConfirmation struct {
	closeBusy           bool
	unsave              bool
	closeRow            row
	destroyPlan         *destroyPlan
	worktreeForcePrompt bool
	mergedWorktrees     []worktreeItem
	bulkWorktrees       []worktreeItem
}

type worktreeCreateForm struct {
	worktreeBranch           string
	worktreePaths            []string
	worktreeBusy             bool
	worktreeRecipe           *workspace.Config
	worktreeRecipePath       string
	worktreeRecipeMode       string
	worktreeCustomWorkspaces bool
	worktreeSelected         []bool
	worktreeWorkspaceCursor  int
	worktreeRepositories     map[string]repoIdentity
	// launchOnFolder repurposes the form to launch the recipe layout on the
	// project's existing folders (no worktree). It hides the branch field and
	// redirects Enter to runLaunchLayout.
	launchOnFolder    bool
	launchProjectPath string
	// worktreeSessionName is the user-typed Kitty session name in launch mode.
	// Empty falls back to an auto-derived name.
	worktreeSessionName string
}

type repoIdentity struct {
	owner string
	repo  string
}

// modeState is the single owner of modal presentation state. Exactly one
// embedded payload pointer is non-nil for a non-normal mode.
type modeState struct {
	mode modeKind
	*helpMode
	*searchMode
	*renameForm
	*createSessionForm
	*cloneForm
	*checkoutForm
	*pinMode
	*saveConfirmation
	*closeConfirmation
	*worktreeCreateForm
}

func (m *model) cancelMode() {
	m.modeState = modeState{}
}

// activateMode is the only transition into a modal state. Resetting first
// guarantees that payload data from the previous mode cannot remain active.
func (m *model) activateMode(kind modeKind) {
	state := modeState{mode: kind}
	switch kind {
	case modeHelp:
		state.helpMode = &helpMode{}
	case modeSearch:
		state.searchMode = &searchMode{}
	case modeRename:
		state.renameForm = &renameForm{}
	case modeCreateSession:
		state.createSessionForm = &createSessionForm{}
	case modeClone:
		state.cloneForm = &cloneForm{}
	case modeCheckoutPR:
		state.checkoutForm = &checkoutForm{}
	case modePin:
		state.pinMode = &pinMode{}
	case modeSaveConfirm:
		state.saveConfirmation = &saveConfirmation{}
	case modeCloseConfirm:
		state.closeConfirmation = &closeConfirmation{}
	case modeWorktreeCreate:
		state.worktreeCreateForm = &worktreeCreateForm{}
	}
	m.modeState = state
}

type model struct {
	modeState
	entries                  []entry
	rows                     []row
	cursor                   int
	query                    string
	saving                   bool
	selected                 map[string]bool
	wtBulkSelected           map[string]bool // Worktree-tab bulk selection, keyed by worktree path
	pins                     pinStore
	names                    nameStore
	filter                   int
	previousFilter           int
	worktreeFilterEntryIndex int
	worktreeFilterRows       []worktreeFilterRow
	worktreeLoading          bool
	zoxideCtx                zoxideMergeContext
	zoxidePending            bool
	width                    int
	height                   int
	helpScroll               int
	pendingG                 bool
	err                      error
	kitty                    string
	zoxide                   string
	preview                  string
	previewErr               error
	previewID                int
	previewRequest           uint64
	previewBusy              bool
	showPreview              bool
	agentStatusDir           string
	agentSpinnerFrame        int
	agentSpinnerPending      bool
	cloneBaseRoot            string
	worktreeRoot             string
	mergedWorktreeBusy       bool
	worktreePullBusy         bool
	destroyPlanning          bool
	prStatusPending          map[string]bool
	prStatusDirPending       map[string]bool
	prStatusLastFetch        map[string]time.Time
	pathPRChecked            map[string]bool
	startupCmd               tea.Cmd
}

// modeKind makes modal combinations unrepresentable. Background busy state
// remains on the feature that owns it (for example cloneBusy).
type modeKind uint8

const (
	modeNormal modeKind = iota
	modeHelp
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
	filterSSH
	filterSaved
	filterWorktrees
)

// cycleFilters are the flat filters reachable by cycling Tab/Shift+tab.
// filterWorktrees is deliberately excluded: it is a project-scoped drill-in
// surface opened with w and closed with esc, not a flat list. Cycling into it
// without a selected project only ever rendered an empty list, so it is reached
// solely through w, which scopes it to a project and records the filter to
// return to.
var cycleFilters = []int{
	filterAll, filterAgents, filterSSH, filterSaved,
}

// cycleFilter advances current by step positions within cycleFilters. A current
// filter outside the cycle (such as filterWorktrees) has no defined neighbor
// there, so the caller gates cycling on being inside the cycle.
func cycleFilter(current, step int) int {
	for index, value := range cycleFilters {
		if value == current {
			next := (index + step) % len(cycleFilters)
			if next < 0 {
				next += len(cycleFilters)
			}
			return cycleFilters[next]
		}
	}
	return current
}

const (
	prStatusCacheVersion = state.CurrentPRCacheVersion
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
	errorStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	ansiPattern       = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)
	backgroundSGR     = regexp.MustCompile(`\x1b\[(48(:[0-9]*)+|48(;[0-9]*)+|49)m`)
)
