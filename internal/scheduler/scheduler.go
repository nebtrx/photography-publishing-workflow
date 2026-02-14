// Package scheduler manages a posting schedule queue, assigning approved
// posts to optimal time slots based on a configured schedule.
package scheduler

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"photography-publishing-workflow/internal/manifest"
)

// SlotConfig defines a recurring time slot.
type SlotConfig struct {
	Day  string `json:"day"`  // monday, tuesday, ...
	Time string `json:"time"` // HH:MM
}

// ScheduleConfig holds the schedule configuration.
type ScheduleConfig struct {
	Timezone    string       `json:"timezone"`
	Slots       []SlotConfig `json:"slots"`
	BatchSize   int          `json:"batch_size"`
	MinGapHours int          `json:"min_gap_hours"`
}

// QueueEntry represents a scheduled post in the queue.
type QueueEntry struct {
	PostID       string    `json:"post_id"`
	ManifestPath string    `json:"manifest_path"`
	ScheduledFor time.Time `json:"scheduled_for"`
	SlotID       string    `json:"slot_id"`
	BatchID      string    `json:"batch_id"`
	QueuedAt     time.Time `json:"queued_at"`
}

// Queue is the schedule queue persisted to disk.
type Queue struct {
	Entries []QueueEntry `json:"entries"`
}

// Scheduler manages post scheduling.
type Scheduler struct {
	config    ScheduleConfig
	queuePath string
	location  *time.Location
	logger    *log.Logger
	dryRun    bool
}

// Options configures the scheduler.
type Options struct {
	ConfigPath string // path to schedule config JSON
	QueuePath  string // path to schedule_queue.json
	DryRun     bool
}

// New creates a Scheduler from a config file.
func New(opts Options) (*Scheduler, error) {
	cfg, err := loadConfig(opts.ConfigPath)
	if err != nil {
		return nil, fmt.Errorf("load schedule config: %w", err)
	}

	loc, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		return nil, fmt.Errorf("invalid timezone %q: %w", cfg.Timezone, err)
	}

	if len(cfg.Slots) == 0 {
		return nil, fmt.Errorf("no slots configured")
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 3
	}

	return &Scheduler{
		config:    *cfg,
		queuePath: opts.QueuePath,
		location:  loc,
		logger:    log.New(os.Stderr, "[schedule] ", log.LstdFlags),
		dryRun:    opts.DryRun,
	}, nil
}

// Schedule assigns an approved post to the next available slot.
func (s *Scheduler) Schedule(m *manifest.Manifest, manifestPath string) (*QueueEntry, error) {
	if m.State != manifest.StateApproved {
		return nil, fmt.Errorf("manifest state is %q, expected %q", m.State, manifest.StateApproved)
	}

	queue, err := s.loadQueue()
	if err != nil {
		return nil, fmt.Errorf("load queue: %w", err)
	}

	// Check for duplicate
	for _, e := range queue.Entries {
		if e.PostID == m.ID {
			return nil, fmt.Errorf("post %q is already scheduled for %s", m.ID, e.ScheduledFor.In(s.location).Format("2006-01-02 15:04"))
		}
	}

	now := time.Now().In(s.location)
	slot, slotTime := s.findNextSlot(now, queue)
	if slot == nil {
		return nil, fmt.Errorf("no available slots found")
	}

	slotID := fmt.Sprintf("%s-%s", slot.Day, slot.Time)
	batchID := fmt.Sprintf("%s-%s", slotTime.Format("2006-01-02"), strings.ReplaceAll(slot.Time, ":", ""))

	entry := QueueEntry{
		PostID:       m.ID,
		ManifestPath: manifestPath,
		ScheduledFor: slotTime.UTC(),
		SlotID:       slotID,
		BatchID:      batchID,
		QueuedAt:     time.Now().UTC(),
	}

	if s.dryRun {
		s.logger.Printf("[DRY RUN] Would schedule %s for %s (slot: %s)",
			m.ID, slotTime.In(s.location).Format("2006-01-02 15:04 MST"), slotID)
		return &entry, nil
	}

	// Update manifest
	m.Scheduling = &manifest.Scheduling{
		ScheduledFor: &slotTime,
		SlotID:       slotID,
		BatchID:      batchID,
		QueuedAt:     func() *time.Time { t := entry.QueuedAt; return &t }(),
		Method:       "client_queue", // default; update if Instagram-side scheduling works
	}

	if err := m.Transition(manifest.StateScheduled); err != nil {
		return nil, fmt.Errorf("transition to scheduled: %w", err)
	}
	if err := m.Write(manifestPath); err != nil {
		return nil, fmt.Errorf("write manifest: %w", err)
	}

	// Add to queue and save
	queue.Entries = append(queue.Entries, entry)
	if err := s.saveQueue(queue); err != nil {
		return nil, fmt.Errorf("save queue: %w", err)
	}

	s.logger.Printf("Scheduled %s for %s (slot: %s, batch: %s)",
		m.ID, slotTime.In(s.location).Format("2006-01-02 15:04 MST"), slotID, batchID)

	return &entry, nil
}

// ListQueue returns all currently scheduled posts.
func (s *Scheduler) ListQueue() ([]QueueEntry, error) {
	queue, err := s.loadQueue()
	if err != nil {
		return nil, err
	}

	// Sort by scheduled time
	sort.Slice(queue.Entries, func(i, j int) bool {
		return queue.Entries[i].ScheduledFor.Before(queue.Entries[j].ScheduledFor)
	})

	return queue.Entries, nil
}

// NextSlot returns the next available slot and how many openings remain.
func (s *Scheduler) NextSlot() (slotTime time.Time, slotID string, openings int, err error) {
	queue, err := s.loadQueue()
	if err != nil {
		return time.Time{}, "", 0, err
	}

	now := time.Now().In(s.location)
	slot, t := s.findNextSlot(now, queue)
	if slot == nil {
		return time.Time{}, "", 0, fmt.Errorf("no available slots")
	}

	sid := fmt.Sprintf("%s-%s", slot.Day, slot.Time)
	count := s.countInSlot(queue, t, sid)

	return t, sid, s.config.BatchSize - count, nil
}

// DueEntries returns queue entries that are due for publishing (scheduled time <= now).
func (s *Scheduler) DueEntries() ([]QueueEntry, error) {
	queue, err := s.loadQueue()
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	var due []QueueEntry
	for _, e := range queue.Entries {
		if !e.ScheduledFor.After(now) {
			due = append(due, e)
		}
	}
	return due, nil
}

// RemoveEntry removes a post from the queue (after publishing or cancellation).
func (s *Scheduler) RemoveEntry(postID string) error {
	queue, err := s.loadQueue()
	if err != nil {
		return err
	}

	var filtered []QueueEntry
	for _, e := range queue.Entries {
		if e.PostID != postID {
			filtered = append(filtered, e)
		}
	}
	queue.Entries = filtered
	return s.saveQueue(queue)
}

// findNextSlot finds the next slot with available capacity.
func (s *Scheduler) findNextSlot(now time.Time, queue *Queue) (*SlotConfig, time.Time) {
	// Generate candidate slot times for the next 30 days
	type candidate struct {
		slot *SlotConfig
		time time.Time
	}

	var candidates []candidate
	for day := 0; day < 30; day++ {
		date := now.AddDate(0, 0, day)
		for i := range s.config.Slots {
			slot := &s.config.Slots[i]
			slotTime := s.slotTimeOnDate(date, slot)

			// Must be in the future
			if !slotTime.After(now) {
				continue
			}

			candidates = append(candidates, candidate{slot: slot, time: slotTime})
		}
	}

	// Sort by time
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].time.Before(candidates[j].time)
	})

	// Find first slot with capacity
	for _, c := range candidates {
		slotID := fmt.Sprintf("%s-%s", c.slot.Day, c.slot.Time)
		count := s.countInSlot(queue, c.time, slotID)
		if count < s.config.BatchSize {
			return c.slot, c.time
		}
	}

	return nil, time.Time{}
}

// slotTimeOnDate calculates the exact time for a slot on a given date.
func (s *Scheduler) slotTimeOnDate(date time.Time, slot *SlotConfig) time.Time {
	// Find the day of week offset
	targetDay := parseDayOfWeek(slot.Day)
	currentDay := date.Weekday()
	dayOffset := (int(targetDay) - int(currentDay) + 7) % 7

	slotDate := date.AddDate(0, 0, dayOffset)

	// Parse time
	var hour, minute int
	fmt.Sscanf(slot.Time, "%d:%d", &hour, &minute)

	return time.Date(slotDate.Year(), slotDate.Month(), slotDate.Day(),
		hour, minute, 0, 0, s.location)
}

// countInSlot counts how many queue entries are assigned to a specific slot time.
func (s *Scheduler) countInSlot(queue *Queue, slotTime time.Time, slotID string) int {
	count := 0
	for _, e := range queue.Entries {
		// Match by date and slot ID
		if e.SlotID == slotID && sameDate(e.ScheduledFor.In(s.location), slotTime) {
			count++
		}
	}
	return count
}

func sameDate(a, b time.Time) bool {
	return a.Year() == b.Year() && a.Month() == b.Month() && a.Day() == b.Day()
}

func parseDayOfWeek(day string) time.Weekday {
	switch strings.ToLower(day) {
	case "sunday":
		return time.Sunday
	case "monday":
		return time.Monday
	case "tuesday":
		return time.Tuesday
	case "wednesday":
		return time.Wednesday
	case "thursday":
		return time.Thursday
	case "friday":
		return time.Friday
	case "saturday":
		return time.Saturday
	default:
		return time.Sunday
	}
}

// loadQueue reads the queue from disk (or returns empty if doesn't exist).
func (s *Scheduler) loadQueue() (*Queue, error) {
	data, err := os.ReadFile(s.queuePath)
	if err != nil {
		if os.IsNotExist(err) {
			return &Queue{Entries: []QueueEntry{}}, nil
		}
		return nil, fmt.Errorf("read queue: %w", err)
	}

	var queue Queue
	if err := json.Unmarshal(data, &queue); err != nil {
		return nil, fmt.Errorf("parse queue: %w", err)
	}
	if queue.Entries == nil {
		queue.Entries = []QueueEntry{}
	}
	return &queue, nil
}

// saveQueue writes the queue atomically.
func (s *Scheduler) saveQueue(queue *Queue) error {
	dir := filepath.Dir(s.queuePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create queue dir: %w", err)
	}

	data, err := json.MarshalIndent(queue, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal queue: %w", err)
	}
	data = append(data, '\n')

	tmp := s.queuePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write temp queue: %w", err)
	}
	if err := os.Rename(tmp, s.queuePath); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename queue: %w", err)
	}
	return nil
}

func loadConfig(path string) (*ScheduleConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	var cfg ScheduleConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return &cfg, nil
}
