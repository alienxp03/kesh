package state

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/alienxp03/kesh/internal/domain"
)

func fixture(t *testing.T, name string, replacements map[string]string) []byte {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	value := string(content)
	for old, replacement := range replacements {
		value = strings.ReplaceAll(value, old, replacement)
	}
	return []byte(value)
}

func writeFixture(t *testing.T, path, name string, replacements map[string]string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, fixture(t, name, replacements), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestAliasesFixtureAndRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "aliases.json")
	writeFixture(t, path, "aliases.json", nil)
	aliases, err := LoadNames(path)
	if err != nil {
		t.Fatal(err)
	}
	if aliases["workspace:kesh-dotfiles"] != "Dotfiles" || aliases["ssh://production"] != "Production" {
		t.Fatalf("loaded aliases = %#v", aliases)
	}
	if err := SaveNames(path, aliases); err != nil {
		t.Fatal(err)
	}
	assertPrivateFile(t, path)
}

func TestPinsV2FixtureAndLegacyVersion(t *testing.T) {
	directory := t.TempDir()
	sessions := filepath.Join(directory, "sessions")
	sessionFile := filepath.Join(sessions, "dotfiles.kitty-session")
	path := filepath.Join(directory, "pins.json")
	writeFixture(t, path, "pins-v2.json", map[string]string{"SESSION_FILE": sessionFile})
	pins, err := LoadPins(path, sessions)
	if err != nil {
		t.Fatal(err)
	}
	if pins["1"].Version != CurrentPinVersion || pins["1"].SessionFile != sessionFile {
		t.Fatalf("loaded pins = %#v", pins)
	}
	pins["2"] = PinTarget{Key: "/workspace/legacy", Name: "Legacy"}
	if err := SavePins(path, pins); err != nil {
		t.Fatal(err)
	}
	roundTrip, err := LoadPins(path, sessions)
	if err != nil || !reflect.DeepEqual(roundTrip, pins) {
		t.Fatalf("round trip = %#v, %v", roundTrip, err)
	}
	assertPrivateFile(t, path)
}

func TestSavedSessionsV1FixtureAndRoundTrip(t *testing.T) {
	directory := t.TempDir()
	sessions := filepath.Join(directory, "sessions")
	sessionFile := filepath.Join(sessions, "dotfiles.kitty-session")
	path := filepath.Join(directory, "saved-sessions.json")
	writeFixture(t, path, "saved-sessions-v1.json", map[string]string{"SESSION_FILE": sessionFile})
	store, err := LoadSavedSessions(path, sessions)
	if err != nil {
		t.Fatal(err)
	}
	if store.Version != CurrentSavedSessionVersion || store.Sessions[sessionFile].SessionName != "kesh-dotfiles" {
		t.Fatalf("loaded sessions = %#v", store)
	}
	if err := SaveSavedSessions(path, store); err != nil {
		t.Fatal(err)
	}
	roundTrip, err := LoadSavedSessions(path, sessions)
	if err != nil || !reflect.DeepEqual(roundTrip, store) {
		t.Fatalf("round trip = %#v, %v", roundTrip, err)
	}
	assertPrivateFile(t, path)
}

func TestPRCacheV2FixtureAndDeterministicWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pr-status.json")
	const repository = "git@github.com:alienxp03/dotfiles.git"
	writeFixture(t, path, "pr-status-v2.json", nil)
	pullRequests, fetchedAt := LoadPRCache(path, repository)
	if fetchedAt.Format(time.RFC3339) != "2026-07-22T15:00:00Z" {
		t.Fatalf("fetched at = %s", fetchedAt)
	}
	if pullRequests[domain.PullRequestKey("feature", "abc123")].Number != 1 {
		t.Fatalf("loaded pull requests = %#v", pullRequests)
	}
	pullRequests[domain.PullRequestKey("alpha", "def456")] = domain.PullRequest{Status: "MERGED", Number: 2}
	now := time.Date(2026, 7, 23, 9, 30, 0, 0, time.UTC)
	if err := SavePRCache(path, repository, pullRequests, now); err != nil {
		t.Fatal(err)
	}
	reloaded, timestamp := LoadPRCache(path, repository)
	if !reflect.DeepEqual(reloaded, pullRequests) || !timestamp.Equal(now) {
		t.Fatalf("round trip = %#v, %s", reloaded, timestamp)
	}
	assertPrivateFile(t, path)
}

func TestInvalidPersistedVersionsAreRejectedOrIgnored(t *testing.T) {
	directory := t.TempDir()
	sessions := filepath.Join(directory, "sessions")
	saved := filepath.Join(directory, "saved.json")
	if err := os.WriteFile(saved, []byte(`{"version":99,"sessions":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSavedSessions(saved, sessions); err == nil {
		t.Fatal("unsupported saved-session version was accepted")
	}
	cache := filepath.Join(directory, "cache.json")
	if err := os.WriteFile(cache, []byte(`{"version":99,"repositories":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if entries, fetchedAt := LoadPRCache(cache, "repo"); entries != nil || !fetchedAt.IsZero() {
		t.Fatalf("unsupported cache = %#v, %s", entries, fetchedAt)
	}
}

func assertPrivateFile(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("%s mode = %o, want 600", path, info.Mode().Perm())
	}
}
