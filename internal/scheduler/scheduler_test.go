package scheduler

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"photography-publishing-workflow/internal/manifest"
)

func writeTestConfig(t *testing.T, dir string) string {
	t.Helper()
	cfg := ScheduleConfig{
		Timezone:    "Europe/Amsterdam",
		Slots: []SlotConfig{
			{Day: "monday", Time: "08:00"},
			{Day: "monday", Time: "18:30"},
			{Day: "wednesday", Time: "12:00"},
			{Day: "friday", Time: "08:30"},
			{Day: "sunday", Time: "10:00"},
		},
		BatchSize:   2,
		MinGapHours: 4,
	}
	data, _ := json.MarshalIndent(cfg, "", "  ")
	path := filepath.Join(dir, "schedule.json")
	os.WriteFile(path, data, 0o644)
	return path
}

func approvedManifest(id string) *manifest.Manifest {
	m := manifest.New(id, "/tmp/"+id)
	m.State = manifest.StateApproved
	m.Review = &manifest.Review{
		Decision:    "approved",
		PublishMode: "queued",
		ReviewedAt:  time.Now().UTC(),
	}
	return m
}

func TestSchedule_AssignsSlot(t *testing.T) {
	dir := t.TempDir()
	configPath := writeTestConfig(t, dir)
	queuePath := filepath.Join(dir, "queue.json")
	manifestPath := filepath.Join(dir, "manifest.json")

	m := approvedManifest("test-post")
	m.Write(manifestPath)

	s, err := New(Options{
		ConfigPath: configPath,
		QueuePath:  queuePath,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	entry, err := s.Schedule(m, manifestPath)
	if err != nil {
		t.Fatalf("Schedule: %v", err)
	}

	if entry.PostID != "test-post" {
		t.Errorf("post_id = %q, want %q", entry.PostID, "test-post")
	}
	if entry.SlotID == "" {
		t.Error("slot_id should not be empty")
	}
	if entry.ScheduledFor.IsZero() {
		t.Error("scheduled_for should be set")
	}
	if !entry.ScheduledFor.After(time.Now()) {
		t.Error("scheduled_for should be in the future")
	}

	// Manifest should be updated
	if m.State != manifest.StateScheduled {
		t.Errorf("state = %q, want %q", m.State, manifest.StateScheduled)
	}
	if m.Scheduling == nil {
		t.Fatal("scheduling is nil")
	}
	if m.Scheduling.Method != "client_queue" {
		t.Errorf("method = %q, want %q", m.Scheduling.Method, "client_queue")
	}

	// Queue file should exist with 1 entry
	queue, _ := s.loadQueue()
	if len(queue.Entries) != 1 {
		t.Errorf("queue entries = %d, want 1", len(queue.Entries))
	}
}

func TestSchedule_BatchOverflow(t *testing.T) {
	dir := t.TempDir()
	configPath := writeTestConfig(t, dir)
	queuePath := filepath.Join(dir, "queue.json")

	s, err := New(Options{
		ConfigPath: configPath,
		QueuePath:  queuePath,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Schedule 3 posts — batch_size is 2, so the third should go to a different slot
	var entries []*QueueEntry
	for i := 0; i < 3; i++ {
		id := "post-" + string(rune('a'+i))
		m := approvedManifest(id)
		mPath := filepath.Join(dir, id+".json")
		m.Write(mPath)

		// Reset state for scheduling (each needs to be approved)
		m.State = manifest.StateApproved
		entry, err := s.Schedule(m, mPath)
		if err != nil {
			t.Fatalf("Schedule %d: %v", i, err)
		}
		entries = append(entries, entry)
	}

	// First two should share a slot, third should be different
	if entries[0].SlotID != entries[1].SlotID {
		t.Errorf("first two should share slot: %s vs %s", entries[0].SlotID, entries[1].SlotID)
	}
	if entries[2].SlotID == entries[0].SlotID && sameDate(entries[2].ScheduledFor, entries[0].ScheduledFor) {
		t.Error("third post should overflow to a different slot")
	}
}

func TestSchedule_DuplicatePrevented(t *testing.T) {
	dir := t.TempDir()
	configPath := writeTestConfig(t, dir)
	queuePath := filepath.Join(dir, "queue.json")

	s, _ := New(Options{ConfigPath: configPath, QueuePath: queuePath})

	m := approvedManifest("dup-post")
	mPath := filepath.Join(dir, "manifest.json")
	m.Write(mPath)

	if _, err := s.Schedule(m, mPath); err != nil {
		t.Fatalf("first schedule: %v", err)
	}

	// Reset state to try scheduling again
	m.State = manifest.StateApproved
	if _, err := s.Schedule(m, mPath); err == nil {
		t.Error("expected error for duplicate scheduling")
	}
}

func TestSchedule_WrongState(t *testing.T) {
	dir := t.TempDir()
	configPath := writeTestConfig(t, dir)

	s, _ := New(Options{ConfigPath: configPath, QueuePath: filepath.Join(dir, "q.json")})

	m := manifest.New("test", "/tmp")
	m.State = manifest.StateScanned

	if _, err := s.Schedule(m, "/tmp/manifest.json"); err == nil {
		t.Error("expected error for wrong state")
	}
}

func TestSchedule_DryRun(t *testing.T) {
	dir := t.TempDir()
	configPath := writeTestConfig(t, dir)
	queuePath := filepath.Join(dir, "queue.json")

	s, _ := New(Options{ConfigPath: configPath, QueuePath: queuePath, DryRun: true})

	m := approvedManifest("dry-post")
	mPath := filepath.Join(dir, "manifest.json")
	m.Write(mPath)

	entry, err := s.Schedule(m, mPath)
	if err != nil {
		t.Fatalf("Schedule dry run: %v", err)
	}

	if entry == nil {
		t.Fatal("entry should not be nil in dry run")
	}

	// State should NOT change in dry run
	if m.State != manifest.StateApproved {
		t.Errorf("state should remain approved in dry run, got %q", m.State)
	}

	// Queue should be empty
	if _, err := os.Stat(queuePath); !os.IsNotExist(err) {
		t.Error("queue file should not be created in dry run")
	}
}

func TestListQueue_Empty(t *testing.T) {
	dir := t.TempDir()
	configPath := writeTestConfig(t, dir)

	s, _ := New(Options{ConfigPath: configPath, QueuePath: filepath.Join(dir, "q.json")})

	entries, err := s.ListQueue()
	if err != nil {
		t.Fatalf("ListQueue: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("entries = %d, want 0", len(entries))
	}
}

func TestListQueue_Sorted(t *testing.T) {
	dir := t.TempDir()
	configPath := writeTestConfig(t, dir)
	queuePath := filepath.Join(dir, "queue.json")

	s, _ := New(Options{ConfigPath: configPath, QueuePath: queuePath})

	// Schedule two posts
	for _, id := range []string{"post-b", "post-a"} {
		m := approvedManifest(id)
		mPath := filepath.Join(dir, id+".json")
		m.Write(mPath)
		s.Schedule(m, mPath)
	}

	entries, _ := s.ListQueue()
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}

	// Should be sorted by time
	if entries[0].ScheduledFor.After(entries[1].ScheduledFor) {
		t.Error("entries should be sorted by scheduled time")
	}
}

func TestNextSlot(t *testing.T) {
	dir := t.TempDir()
	configPath := writeTestConfig(t, dir)

	s, _ := New(Options{ConfigPath: configPath, QueuePath: filepath.Join(dir, "q.json")})

	slotTime, slotID, openings, err := s.NextSlot()
	if err != nil {
		t.Fatalf("NextSlot: %v", err)
	}

	if slotTime.IsZero() {
		t.Error("slot time should not be zero")
	}
	if slotID == "" {
		t.Error("slot ID should not be empty")
	}
	if openings != 2 { // batch_size=2, empty queue
		t.Errorf("openings = %d, want 2", openings)
	}
	if !slotTime.After(time.Now()) {
		t.Error("next slot should be in the future")
	}
}

func TestRemoveEntry(t *testing.T) {
	dir := t.TempDir()
	configPath := writeTestConfig(t, dir)
	queuePath := filepath.Join(dir, "queue.json")

	s, _ := New(Options{ConfigPath: configPath, QueuePath: queuePath})

	m := approvedManifest("remove-me")
	mPath := filepath.Join(dir, "manifest.json")
	m.Write(mPath)
	s.Schedule(m, mPath)

	entries, _ := s.ListQueue()
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}

	if err := s.RemoveEntry("remove-me"); err != nil {
		t.Fatalf("RemoveEntry: %v", err)
	}

	entries, _ = s.ListQueue()
	if len(entries) != 0 {
		t.Errorf("entries after remove = %d, want 0", len(entries))
	}
}

func TestDueEntries(t *testing.T) {
	dir := t.TempDir()
	configPath := writeTestConfig(t, dir)
	queuePath := filepath.Join(dir, "queue.json")

	// Write a queue with one past entry and one future entry
	queue := &Queue{
		Entries: []QueueEntry{
			{
				PostID:       "past-post",
				ScheduledFor: time.Now().Add(-1 * time.Hour),
				SlotID:       "monday-08:00",
			},
			{
				PostID:       "future-post",
				ScheduledFor: time.Now().Add(24 * time.Hour),
				SlotID:       "wednesday-12:00",
			},
		},
	}
	data, _ := json.MarshalIndent(queue, "", "  ")
	os.WriteFile(queuePath, data, 0o644)

	s, _ := New(Options{ConfigPath: configPath, QueuePath: queuePath})

	due, err := s.DueEntries()
	if err != nil {
		t.Fatalf("DueEntries: %v", err)
	}

	if len(due) != 1 {
		t.Fatalf("due entries = %d, want 1", len(due))
	}
	if due[0].PostID != "past-post" {
		t.Errorf("due post = %q, want %q", due[0].PostID, "past-post")
	}
}

func TestNew_InvalidConfig(t *testing.T) {
	dir := t.TempDir()

	// Missing config file
	_, err := New(Options{ConfigPath: filepath.Join(dir, "nope.json"), QueuePath: "/tmp/q.json"})
	if err == nil {
		t.Error("expected error for missing config")
	}

	// Invalid timezone
	cfg := ScheduleConfig{
		Timezone: "Invalid/Zone",
		Slots:    []SlotConfig{{Day: "monday", Time: "08:00"}},
	}
	data, _ := json.Marshal(cfg)
	cfgPath := filepath.Join(dir, "bad.json")
	os.WriteFile(cfgPath, data, 0o644)

	_, err = New(Options{ConfigPath: cfgPath, QueuePath: "/tmp/q.json"})
	if err == nil {
		t.Error("expected error for invalid timezone")
	}

	// No slots
	cfg2 := ScheduleConfig{Timezone: "UTC", Slots: []SlotConfig{}}
	data2, _ := json.Marshal(cfg2)
	cfgPath2 := filepath.Join(dir, "empty.json")
	os.WriteFile(cfgPath2, data2, 0o644)

	_, err = New(Options{ConfigPath: cfgPath2, QueuePath: "/tmp/q.json"})
	if err == nil {
		t.Error("expected error for no slots")
	}
}

func TestParseDayOfWeek(t *testing.T) {
	tests := []struct {
		input string
		want  time.Weekday
	}{
		{"monday", time.Monday},
		{"Wednesday", time.Wednesday},
		{"FRIDAY", time.Friday},
		{"sunday", time.Sunday},
	}
	for _, tt := range tests {
		got := parseDayOfWeek(tt.input)
		if got != tt.want {
			t.Errorf("parseDayOfWeek(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}
