package enricher

import (
	"context"
	"testing"

	"photography-publishing-workflow/internal/ai"
	"photography-publishing-workflow/internal/manifest"
	"photography-publishing-workflow/internal/scanner"
	"photography-publishing-workflow/internal/testutil"
	"photography-publishing-workflow/internal/validator"
)

func setupValidatedManifest(t *testing.T) *manifest.Manifest {
	t.Helper()
	dir := t.TempDir()
	if err := testutil.SetupSamplePost(dir); err != nil {
		t.Fatalf("setup: %v", err)
	}
	m, err := scanner.Scan(dir)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if err := validator.Apply(m); err != nil {
		t.Fatalf("validate: %v", err)
	}
	return m
}

func TestEnrich_FullPipeline(t *testing.T) {
	m := setupValidatedManifest(t)

	mockProvider := ai.NewMock(
		// Caption response
		`The quiet geometry of #architecture reveals itself in the morning light, when every line becomes a conversation between #shadow and #concrete`,
		// Location response
		`{"location_name": "Markthal, Rotterdam", "city": "Rotterdam", "country": "Netherlands", "confidence": "high", "reasoning": "The distinctive arched ceiling of the Markthal is visible"}`,
	)

	e := New(mockProvider, Options{})
	if err := e.Enrich(context.Background(), m); err != nil {
		t.Fatalf("Enrich: %v", err)
	}

	// State should be pending_review
	if m.State != manifest.StatePendingReview {
		t.Errorf("state = %q, want %q", m.State, manifest.StatePendingReview)
	}

	// Caption should be populated
	if m.Enrichment == nil || m.Enrichment.Caption == nil {
		t.Fatal("enrichment.caption is nil")
	}
	if m.Enrichment.Caption.Text == "" {
		t.Error("caption text is empty")
	}
	if m.Enrichment.Caption.HashtagCount == 0 {
		t.Error("expected hashtags in caption")
	}
	if m.Enrichment.Caption.HashtagCount > 5 {
		t.Errorf("hashtag count = %d, want ≤5", m.Enrichment.Caption.HashtagCount)
	}

	// Location should be populated
	if m.Enrichment.Location == nil {
		t.Fatal("enrichment.location is nil")
	}
	if m.Enrichment.Location.Name != "Markthal, Rotterdam" {
		t.Errorf("location = %q, want %q", m.Enrichment.Location.Name, "Markthal, Rotterdam")
	}
	if m.Enrichment.Location.Source != "ai_vision" {
		t.Errorf("location source = %q, want %q", m.Enrichment.Location.Source, "ai_vision")
	}

	// Should have made 2 AI calls
	if len(mockProvider.Calls) != 2 {
		t.Errorf("AI calls = %d, want 2", len(mockProvider.Calls))
	}
}

func TestEnrich_SkipLocation(t *testing.T) {
	m := setupValidatedManifest(t)

	mockProvider := ai.NewMock(
		// Only caption response needed
		`Lines converge where the #city meets the sky, a silent #geometry of glass and steel`,
	)

	e := New(mockProvider, Options{
		SkipLocation: true,
	})
	if err := e.Enrich(context.Background(), m); err != nil {
		t.Fatalf("Enrich: %v", err)
	}

	if m.Enrichment.Caption == nil {
		t.Fatal("caption should be generated")
	}
	if m.Enrichment.Location != nil {
		t.Error("location should be nil when skipped")
	}

	// Should have made only 1 AI call
	if len(mockProvider.Calls) != 1 {
		t.Errorf("AI calls = %d, want 1", len(mockProvider.Calls))
	}
}

func TestEnrich_LocationFallbackToNone(t *testing.T) {
	m := setupValidatedManifest(t)

	mockProvider := ai.NewMock(
		// Caption
		`A moment between #light and #dark`,
		// Location with no confidence
		`{"location_name": null, "city": null, "country": null, "confidence": "none", "reasoning": "Abstract image"}`,
	)

	e := New(mockProvider, Options{})
	if err := e.Enrich(context.Background(), m); err != nil {
		t.Fatalf("Enrich: %v", err)
	}

	// Location should be nil (no GPS on test images, AI said "none")
	if m.Enrichment.Location != nil {
		t.Errorf("expected nil location, got: %+v", m.Enrichment.Location)
	}
}

func TestEnrich_WrongState(t *testing.T) {
	m := manifest.New("test", "/tmp/test")
	m.State = manifest.StateScanned // not validated

	e := New(ai.NewMock("response"), Options{})
	if err := e.Enrich(context.Background(), m); err == nil {
		t.Error("expected error for wrong state")
	}
}

func TestParseCaption(t *testing.T) {
	text := `That quiet moment when the bridge lights catch the #mist rising off the Maas, before #sunset fades behind the #erasmusbrug`
	c := parseCaption(text)

	if c.HashtagCount != 3 {
		t.Errorf("hashtag count = %d, want 3", c.HashtagCount)
	}
	if len(c.Hashtags) != 3 {
		t.Errorf("hashtags = %v, want 3 items", c.Hashtags)
	}
}

func TestParseCaption_CleanupQuotes(t *testing.T) {
	text := `"The city sleeps under #fog"`
	c := parseCaption(text)
	if c.Text[0] == '"' {
		t.Error("quotes should be stripped")
	}
}

func TestValidateCaption_TooManyHashtags(t *testing.T) {
	c := &manifest.Caption{
		Text:         "A #one #two #three #four #five #six #seven post",
		Hashtags:     []string{"#one", "#two", "#three", "#four", "#five", "#six", "#seven"},
		HashtagCount: 7,
		Confidence:   "high",
	}
	c = validateCaption(c)
	if c.HashtagCount != 5 {
		t.Errorf("hashtag count = %d, want 5", c.HashtagCount)
	}
	if c.Confidence != "medium" {
		t.Errorf("confidence = %q, want %q", c.Confidence, "medium")
	}
}

func TestExtractJSON(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{
			input: `{"key": "value"}`,
			want:  `{"key": "value"}`,
		},
		{
			input: "```json\n{\"key\": \"value\"}\n```",
			want:  `{"key": "value"}`,
		},
		{
			input: "Here is the result:\n{\"key\": \"value\"}",
			want:  `{"key": "value"}`,
		},
	}

	for _, tt := range tests {
		got := extractJSON(tt.input)
		if got != tt.want {
			t.Errorf("extractJSON(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestDeriveHardwareHashtag(t *testing.T) {
	tests := []struct {
		camera string
		want   string
	}{
		{"NIKON Z 8", "#nikon #Z8"},
		{"Nikon Z8", "#nikon #Z8"},
		{"NIKON D850", "#nikon"},
		{"FUJIFILM GFX100S RF", "#fujifilm #fujigfx100rf"},
		{"Sony A7IV", "#sony"},
		{"", "(derive from camera hardware)"},
	}

	for _, tt := range tests {
		got := deriveHardwareHashtag(tt.camera)
		if got != tt.want {
			t.Errorf("deriveHardwareHashtag(%q) = %q, want %q", tt.camera, got, tt.want)
		}
	}
}
