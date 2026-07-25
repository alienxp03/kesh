package domain

// EntryOrder contains only the domain facts needed to order catalog entries.
// Presentation fields and Bubble Tea state deliberately stay out of domain.
type EntryOrder struct {
	Open        bool
	LastFocused float64
	Saved       bool
	Kind        string
	Order       int
}

// Window, Tab, and Entry are the platform-neutral catalog entities assembled
// from Kitty, saved state, zoxide, and SSH configuration.
type Window struct {
	ID          int
	Title       string
	Detail      string
	CWD         string
	Command     string
	FullCommand string
	Agent       string
	LastFocused float64
}

type Tab struct {
	ID      int
	Title   string
	Detail  string
	Agent   string
	Windows []Window
}

type Entry struct {
	Key          string
	Name         string
	OriginalName string
	Detail       string
	Kind         string
	Path         string
	Session      string
	SessionFile  string
	Saved        bool
	Open         bool
	LastFocused  float64
	NameTaken    bool
	Agent        string
	Tabs         []Tab
	Order        int
}

// CatalogContext is carried into the asynchronous zoxide merge.
type CatalogContext struct {
	LivePaths    map[string]bool
	MergedPaths  map[string]bool
	SessionNames map[string]bool
	Home         string
}

// EntryCategoryRank orders closed entries as saved sessions, source projects,
// then SSH hosts.
func EntryCategoryRank(entry EntryOrder) int {
	if entry.Saved {
		return 0
	}
	if entry.Kind == "ssh" {
		return 2
	}
	return 1
}

// EntryLess reports whether a should appear before b.
func EntryLess(a, b EntryOrder) bool {
	if a.Open != b.Open {
		return a.Open
	}
	if a.Open && a.LastFocused != b.LastFocused {
		return a.LastFocused > b.LastFocused
	}
	if !a.Open {
		if aRank, bRank := EntryCategoryRank(a), EntryCategoryRank(b); aRank != bRank {
			return aRank < bRank
		}
	}
	return a.Order < b.Order
}
