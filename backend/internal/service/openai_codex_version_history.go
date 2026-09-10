package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	openAICodexHistoryCacheTTL  = 15 * time.Minute
	openAICodexHistoryScanPages = 3
	OpenAICodexHistoryMaxPage   = 1000
)

// Optional to keep lightweight release clients and existing update clients compatible.
type codexPaginatedGitHubReleaseClient interface {
	FetchReleasesPage(context.Context, string, int, int) ([]*GitHubRelease, error)
}

type OpenAICodexVersion struct {
	Version     string `json:"version"`
	TagName     string `json:"tag_name"`
	PublishedAt string `json:"published_at"`
	HTMLURL     string `json:"html_url"`
}

type OpenAICodexVersionHistory struct {
	Versions      []OpenAICodexVersion `json:"versions"`
	LatestVersion string               `json:"latest_version"`
	HasMore       bool                 `json:"has_more"`
	NextPage      *int                 `json:"next_page"`
}

type OpenAICodexVersionSyncDefaults struct {
	Originator    string `json:"originator"`
	UserAgent     string `json:"user_agent"`
	ClientVersion string `json:"client_version"`
}

type OpenAICodexVersionSyncResult struct {
	LatestVersion string                         `json:"latest_version"`
	SyncedVersion string                         `json:"synced_version"`
	Updated       bool                           `json:"updated"`
	Defaults      OpenAICodexVersionSyncDefaults `json:"defaults"`
}

type cachedCodexVersionHistory struct {
	value     OpenAICodexVersionHistory
	expiresAt time.Time
}

func (s *OpenAICodexVersionSyncService) cachedVersionHistory(page int) (*OpenAICodexVersionHistory, bool) {
	s.historyMu.Lock()
	defer s.historyMu.Unlock()
	cached, ok := s.historyCache[page]
	if !ok || time.Now().After(cached.expiresAt) {
		return nil, false
	}
	value := cached.value
	value.Versions = append([]OpenAICodexVersion{}, value.Versions...)
	return &value, true
}

// ListVersions reads only official stable Codex releases. A page number is the
// GitHub release-page cursor, not an offset in the filtered stable-version list.
// Empty prerelease pages are scanned forward up to a bounded limit; next_page
// always advances, allowing administrators to continue across dense alpha releases.
func (s *OpenAICodexVersionSyncService) ListVersions(ctx context.Context, page int) (*OpenAICodexVersionHistory, error) {
	if s == nil || s.githubClient == nil {
		return nil, errors.New("Codex version history service is not configured")
	}
	if page < 1 || page > OpenAICodexHistoryMaxPage {
		return nil, errors.New("Codex version page is out of range")
	}
	if value, ok := s.cachedVersionHistory(page); ok {
		return value, nil
	}
	s.historyMu.Lock()
	epoch := s.historyEpoch
	s.historyMu.Unlock()
	result := s.historySF.DoChan(fmt.Sprintf("%d:%d", epoch, page), func() (any, error) {
		if value, ok := s.cachedVersionHistory(page); ok {
			return value, nil
		}
		fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), openAICodexVersionSyncTimeout)
		defer cancel()
		value, err := s.fetchVersionHistory(fetchCtx, page)
		if err != nil {
			return nil, err
		}
		s.historyMu.Lock()
		if epoch == s.historyEpoch {
			if s.historyCache == nil || len(s.historyCache) >= 128 {
				s.historyCache = make(map[int]cachedCodexVersionHistory)
			}
			s.historyCache[page] = cachedCodexVersionHistory{value: *value, expiresAt: time.Now().Add(openAICodexHistoryCacheTTL)}
		}
		s.historyMu.Unlock()
		return value, nil
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-result:
		if result.Err != nil {
			return nil, result.Err
		}
		value := *result.Val.(*OpenAICodexVersionHistory)
		value.Versions = append([]OpenAICodexVersion{}, value.Versions...)
		return &value, nil
	}
}

func (s *OpenAICodexVersionSyncService) fetchVersionHistory(ctx context.Context, page int) (*OpenAICodexVersionHistory, error) {
	// The latest endpoint can point at another component. Do not let an empty
	// candidate prevent pagination through alpha-heavy pages to older stable releases.
	latestRelease, _ := s.githubClient.FetchLatestRelease(ctx, openAICodexVersionSyncRepo)
	latest := latestCodexStableReleaseVersion([]*GitHubRelease{latestRelease})
	result := &OpenAICodexVersionHistory{LatestVersion: latest, Versions: []OpenAICodexVersion{}}
	seen := make(map[string]bool)
	_, paginated := s.githubClient.(codexPaginatedGitHubReleaseClient)
	for scanned := 0; scanned < openAICodexHistoryScanPages && page <= OpenAICodexHistoryMaxPage; scanned++ {
		releases, err := s.fetchReleasePage(ctx, page)
		if err != nil {
			return nil, fmt.Errorf("fetch official Codex release history: %w", err)
		}
		for _, release := range releases {
			version := latestCodexStableReleaseVersion([]*GitHubRelease{release})
			if version == "" || seen[version] {
				continue
			}
			seen[version] = true
			result.Versions = append(result.Versions, OpenAICodexVersion{
				Version: version, TagName: strings.TrimSpace(release.TagName), PublishedAt: release.PublishedAt, HTMLURL: "https://github.com/" + openAICodexVersionSyncRepo + "/releases/tag/" + openAICodexVersionTagPrefix + version,
			})
			if CompareVersions(version, result.LatestVersion) > 0 {
				result.LatestVersion = version
			}
		}
		page++
		result.HasMore = paginated && len(releases) == openAICodexVersionSyncPerPage && page <= OpenAICodexHistoryMaxPage
		if !result.HasMore || len(result.Versions) > 0 {
			break
		}
	}
	if result.HasMore {
		result.NextPage = &page
	}
	sort.Slice(result.Versions, func(i, j int) bool {
		return CompareVersions(result.Versions[i].Version, result.Versions[j].Version) > 0
	})
	return result, nil
}

func (s *OpenAICodexVersionSyncService) fetchReleasePage(ctx context.Context, page int) ([]*GitHubRelease, error) {
	if client, ok := s.githubClient.(codexPaginatedGitHubReleaseClient); ok {
		return client.FetchReleasesPage(ctx, openAICodexVersionSyncRepo, page, openAICodexVersionSyncPerPage)
	}
	if page != 1 {
		return nil, errors.New("GitHub release client does not support pagination")
	}
	return s.githubClient.FetchRecentReleases(ctx, openAICodexVersionSyncRepo, openAICodexVersionSyncPerPage)
}
