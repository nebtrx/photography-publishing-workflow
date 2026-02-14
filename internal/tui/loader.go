package tui

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"photography-publishing-workflow/internal/manifest"
)

// loadData reloads all panel data from disk.
func (m *AppModel) loadData() {
	if m.cfg == nil {
		return
	}

	m.loadPendingAndQueue()
	m.loadPublishedLog()
}

// loadPendingAndQueue scans the watch directory for pending_review and approved manifests.
func (m *AppModel) loadPendingAndQueue() {
	dir := m.cfg.Watch.Dir
	if dir == "" {
		return
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	var pending []PostEntry
	var queue []PostEntry

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		mPath := filepath.Join(dir, e.Name(), "manifest.json")
		man, err := manifest.Read(mPath)
		if err != nil {
			continue
		}

		switch man.State {
		case manifest.StatePendingReview:
			pending = append(pending, PostEntry{Manifest: man, Path: mPath})
		case manifest.StateApproved, manifest.StateScheduled:
			queue = append(queue, PostEntry{Manifest: man, Path: mPath})
		}
	}

	// Sort by ID
	sort.Slice(pending, func(i, j int) bool {
		return pending[i].Manifest.ID < pending[j].Manifest.ID
	})
	sort.Slice(queue, func(i, j int) bool {
		return queue[i].Manifest.ID < queue[j].Manifest.ID
	})

	m.pendingPosts = pending
	m.queuePosts = queue
}

// loadPublishedLog reads the JSONL publish log and groups entries by month.
func (m *AppModel) loadPublishedLog() {
	logFile := m.cfg.Archive.LogFile
	if logFile == "" {
		return
	}

	f, err := os.Open(logFile)
	if err != nil {
		m.logGroups = nil
		return
	}
	defer f.Close()

	// Parse JSONL entries
	var entries []LogDisplayEntry
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var raw struct {
			ID              string `json:"id"`
			PublishedAt     string `json:"published_at"`
			InstagramPostID string `json:"instagram_post_id"`
			Permalink       string `json:"permalink"`
			Caption         string `json:"caption"`
			Location        string `json:"location"`
			ImageCount      int    `json:"image_count"`
			ArchivePath     string `json:"archive_path"`
			StoryPublished  bool   `json:"story_published"`
		}
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			continue
		}
		entries = append(entries, LogDisplayEntry{
			ID:              raw.ID,
			ImageCount:      raw.ImageCount,
			PublishedAt:     raw.PublishedAt,
			InstagramPostID: raw.InstagramPostID,
			Permalink:       raw.Permalink,
			Caption:         raw.Caption,
			Location:        raw.Location,
			StoryPublished:  raw.StoryPublished,
			ArchivePath:     raw.ArchivePath,
		})
	}

	// Sort by published_at descending (newest first)
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].PublishedAt > entries[j].PublishedAt
	})

	// Group by month
	groupMap := make(map[string][]LogDisplayEntry)
	var months []string
	for _, e := range entries {
		month := extractMonth(e.PublishedAt)
		if _, exists := groupMap[month]; !exists {
			months = append(months, month)
		}
		groupMap[month] = append(groupMap[month], e)
	}

	// Sort months descending
	sort.Sort(sort.Reverse(sort.StringSlice(months)))

	var groups []LogGroup
	for _, month := range months {
		groups = append(groups, LogGroup{
			Month:     month,
			Entries:   groupMap[month],
			Collapsed: false,
		})
	}

	m.logGroups = groups
}

func extractMonth(publishedAt string) string {
	t, err := time.Parse(time.RFC3339, publishedAt)
	if err != nil {
		// Try to extract YYYY-MM from the string
		if len(publishedAt) >= 7 {
			return publishedAt[:7]
		}
		return "unknown"
	}
	return t.Format("2006-01")
}
