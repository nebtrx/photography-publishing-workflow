package scanner

import (
	"testing"

	"photography-publishing-workflow/internal/manifest"
	"photography-publishing-workflow/internal/testutil"
)

func TestScan_SamplePost(t *testing.T) {
	dir := t.TempDir()
	if err := testutil.SetupSamplePost(dir); err != nil {
		t.Fatalf("setup: %v", err)
	}

	m, err := Scan(dir)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	if len(m.Images) != 3 {
		t.Fatalf("images = %d, want 3", len(m.Images))
	}

	// Check ordering
	for i, img := range m.Images {
		wantOrder := i + 1
		if img.Order != wantOrder {
			t.Errorf("image %d order = %d, want %d", i, img.Order, wantOrder)
		}
	}

	// Check hero
	if !m.Images[0].IsHero {
		t.Error("first image should be hero")
	}
	for _, img := range m.Images[1:] {
		if img.IsHero {
			t.Errorf("%s should not be hero", img.Filename)
		}
	}

	// Check dimensions (800x1000)
	for _, img := range m.Images {
		if img.Width != 800 || img.Height != 1000 {
			t.Errorf("%s dimensions = %dx%d, want 800x1000", img.Filename, img.Width, img.Height)
		}
	}

	// Check aspect ratio classification (800/1000 = 0.8 → 4:5)
	for _, img := range m.Images {
		if img.AspectRatio != "4:5" {
			t.Errorf("%s aspect_ratio = %q, want %q", img.Filename, img.AspectRatio, "4:5")
		}
	}

	// Check state
	if m.State != manifest.StateScanned {
		t.Errorf("state = %q, want %q", m.State, manifest.StateScanned)
	}
}

func TestScan_NoSuffix(t *testing.T) {
	dir := t.TempDir()
	if err := testutil.SetupNoSuffix(dir); err != nil {
		t.Fatalf("setup: %v", err)
	}

	m, err := Scan(dir)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	if len(m.Images) != 3 {
		t.Fatalf("images = %d, want 3", len(m.Images))
	}

	// Should fall back to filename ordering (no EXIF dates on synthetic images)
	// afternoon.jpg, evening.jpg, morning.jpg (alphabetical)
	wantOrder := []string{"afternoon.jpg", "evening.jpg", "morning.jpg"}
	for i, img := range m.Images {
		if img.Filename != wantOrder[i] {
			t.Errorf("image %d = %q, want %q", i, img.Filename, wantOrder[i])
		}
		if img.Order != i+1 {
			t.Errorf("image %d order = %d, want %d", i, img.Order, i+1)
		}
	}

	if !m.Images[0].IsHero {
		t.Error("first image should be hero")
	}
}

func TestScan_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	_, err := Scan(dir)
	if err == nil {
		t.Error("expected error for empty directory")
	}
}

func TestScan_NonExistentDir(t *testing.T) {
	_, err := Scan("/nonexistent/path")
	if err == nil {
		t.Error("expected error for non-existent directory")
	}
}

func TestClassifyAspectRatio(t *testing.T) {
	tests := []struct {
		w, h int
		want string
	}{
		{800, 1000, "4:5"},   // 0.8 → portrait
		{1000, 1000, "1:1"},  // 1.0 → square
		{1200, 1000, "1.91:1"}, // 1.2 → landscape
		{900, 1000, "1:1"},   // 0.9 → square (within 0.85-1.15)
		{1100, 1000, "1:1"},  // 1.1 → square (within 0.85-1.15)
		{700, 1000, "4:5"},   // 0.7 → portrait
		{1920, 1000, "1.91:1"}, // 1.92 → landscape
		{0, 0, "unknown"},    // zero height
	}

	for _, tt := range tests {
		got := classifyAspectRatio(tt.w, tt.h)
		if got != tt.want {
			t.Errorf("classifyAspectRatio(%d, %d) = %q, want %q", tt.w, tt.h, got, tt.want)
		}
	}
}

func TestParseSuffix(t *testing.T) {
	tests := []struct {
		filename string
		wantN    int
		wantOK   bool
	}{
		{"photo_1.jpg", 1, true},
		{"photo_2.jpeg", 2, true},
		{"photo_10.JPG", 10, true},
		{"photo_03.jpg", 3, true},
		{"morning.jpg", 0, false},
		{"photo.jpg", 0, false},
		{"photo_abc.jpg", 0, false},
	}

	for _, tt := range tests {
		n, ok := parseSuffix(tt.filename)
		if ok != tt.wantOK || n != tt.wantN {
			t.Errorf("parseSuffix(%q) = (%d, %v), want (%d, %v)", tt.filename, n, ok, tt.wantN, tt.wantOK)
		}
	}
}

func TestHasMixedOrdering(t *testing.T) {
	mixed := []manifest.Image{
		{Filename: "photo_1.jpg"},
		{Filename: "random.jpg"},
	}
	if !HasMixedOrdering(mixed) {
		t.Error("expected mixed ordering")
	}

	uniform := []manifest.Image{
		{Filename: "photo_1.jpg"},
		{Filename: "photo_2.jpg"},
	}
	if HasMixedOrdering(uniform) {
		t.Error("expected uniform ordering")
	}
}

func TestHasDuplicateSuffixes(t *testing.T) {
	images := []manifest.Image{
		{Filename: "a_1.jpg"},
		{Filename: "b_1.jpg"},
		{Filename: "c_2.jpg"},
	}
	dupes := HasDuplicateSuffixes(images)
	if len(dupes) != 1 {
		t.Fatalf("expected 1 duplicate suffix, got %d", len(dupes))
	}
	if len(dupes[1]) != 2 {
		t.Errorf("expected 2 files for suffix _1, got %d", len(dupes[1]))
	}
}
