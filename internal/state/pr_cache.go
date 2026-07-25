package state

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/alienxp03/dotfiles/apps/kesh/internal/domain"
)

const CurrentPRCacheVersion = 2

type prCacheEntry struct {
	Branch string `json:"branch"`
	Head   string `json:"head"`
	Status string `json:"status"`
	URL    string `json:"url,omitempty"`
	Number int    `json:"number,omitempty"`
}

type prRepositoryCache struct {
	FetchedAt string         `json:"fetched_at"`
	Entries   []prCacheEntry `json:"entries"`
}

type prCache struct {
	Version      int                          `json:"version"`
	Repositories map[string]prRepositoryCache `json:"repositories"`
}

func LoadPRCache(path, repositoryKey string) (map[string]domain.PullRequest, time.Time) {
	if repositoryKey == "" {
		return nil, time.Time{}
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, time.Time{}
	}
	var store prCache
	if json.Unmarshal(content, &store) != nil || store.Version != CurrentPRCacheVersion {
		return nil, time.Time{}
	}
	repository, ok := store.Repositories[repositoryKey]
	if !ok {
		return nil, time.Time{}
	}
	fetchedAt, _ := time.Parse(time.RFC3339, repository.FetchedAt)
	pullRequests := map[string]domain.PullRequest{}
	for _, entry := range repository.Entries {
		pullRequests[domain.PullRequestKey(entry.Branch, entry.Head)] = domain.PullRequest{
			Status: entry.Status,
			URL:    entry.URL,
			Number: entry.Number,
		}
	}
	return pullRequests, fetchedAt
}

func SavePRCache(path, repositoryKey string, pullRequests map[string]domain.PullRequest, now time.Time) error {
	store := prCache{Version: CurrentPRCacheVersion, Repositories: map[string]prRepositoryCache{}}
	if content, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(content, &store)
		if store.Version != CurrentPRCacheVersion || store.Repositories == nil {
			store = prCache{Version: CurrentPRCacheVersion, Repositories: map[string]prRepositoryCache{}}
		}
	}
	entries := make([]prCacheEntry, 0, len(pullRequests))
	for key, pullRequest := range pullRequests {
		parts := strings.SplitN(key, "\x00", 2)
		if len(parts) == 2 {
			entries = append(entries, prCacheEntry{
				Branch: parts[0],
				Head:   parts[1],
				Status: pullRequest.Status,
				URL:    pullRequest.URL,
				Number: pullRequest.Number,
			})
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Branch == entries[j].Branch {
			return entries[i].Head < entries[j].Head
		}
		return entries[i].Branch < entries[j].Branch
	})
	store.Repositories[repositoryKey] = prRepositoryCache{
		FetchedAt: now.UTC().Format(time.RFC3339),
		Entries:   entries,
	}
	return atomicJSON(path, ".pr-status-*.json", store, false)
}
