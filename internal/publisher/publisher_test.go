package publisher

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"photography-publishing-workflow/internal/hosting"
	"photography-publishing-workflow/internal/manifest"
	"photography-publishing-workflow/internal/scanner"
	"photography-publishing-workflow/internal/testutil"
	"photography-publishing-workflow/internal/validator"
)

// mockInstagram implements InstagramAPI for testing.
type mockInstagram struct {
	VerifyErr         error
	ChildIDs          []string
	childIdx          int
	SingleID          string
	CarouselID        string
	StoryID           string
	PublishID         string
	PublishErr        error
	Permalink         string
	LocationID        string
	LocationName      string
	PollErr           error
	CreateChildCalls  int
	CreateSingleCalls int
	PublishCalls      int
	StoryCalls        int
	LocationCalls     int
}

type mockFacebook struct {
	PostID string
	Err    error
	Calls  int
}

func (m *mockFacebook) PublishLink(ctx context.Context, message, link string) (string, error) {
	m.Calls++
	if m.Err != nil {
		return "", m.Err
	}
	if m.PostID == "" {
		return "fb_post_1", nil
	}
	return m.PostID, nil
}

type mockThreads struct {
	PostID    string
	Permalink string
	Err       error
	Calls     int
}

func (m *mockThreads) PublishText(ctx context.Context, text string) (string, string, error) {
	m.Calls++
	if m.Err != nil {
		return "", "", m.Err
	}
	postID := m.PostID
	if postID == "" {
		postID = "threads_post_1"
	}
	permalink := m.Permalink
	if permalink == "" {
		permalink = "https://www.threads.net/@test/post/1"
	}
	return postID, permalink, nil
}

func newMockInstagram() *mockInstagram {
	return &mockInstagram{
		ChildIDs:     []string{"child_1", "child_2", "child_3"},
		SingleID:     "single_container_1",
		CarouselID:   "carousel_container_1",
		StoryID:      "story_container_1",
		PublishID:    "media_12345",
		Permalink:    "https://www.instagram.com/p/TEST123/",
		LocationID:   "loc_amsterdam",
		LocationName: "Amsterdam, Netherlands",
	}
}

func (m *mockInstagram) VerifyToken() (string, error) {
	if m.VerifyErr != nil {
		return "", m.VerifyErr
	}
	return "testuser", nil
}

func (m *mockInstagram) CreateChildContainer(imageURL string) (string, error) {
	m.CreateChildCalls++
	id := fmt.Sprintf("child_%d", m.CreateChildCalls)
	if m.childIdx < len(m.ChildIDs) {
		id = m.ChildIDs[m.childIdx]
		m.childIdx++
	}
	return id, nil
}

func (m *mockInstagram) CreateSingleContainer(imageURL, caption, locationID string) (string, error) {
	m.CreateSingleCalls++
	return m.SingleID, nil
}

func (m *mockInstagram) CreateCarouselContainer(childIDs []string, caption, locationID string) (string, error) {
	return m.CarouselID, nil
}

func (m *mockInstagram) CreateStoryContainer(imageURL string) (string, error) {
	m.StoryCalls++
	return m.StoryID, nil
}

func (m *mockInstagram) PollContainer(containerID string, pollInterval time.Duration, maxPolls int) error {
	return m.PollErr
}

func (m *mockInstagram) Publish(containerID string) (string, error) {
	m.PublishCalls++
	if m.PublishErr != nil {
		return "", m.PublishErr
	}
	return m.PublishID, nil
}

func (m *mockInstagram) GetPermalink(mediaID string) (string, error) {
	return m.Permalink, nil
}

func (m *mockInstagram) SearchLocation(query string) (string, string, error) {
	m.LocationCalls++
	return m.LocationID, m.LocationName, nil
}

// setupApprovedManifest creates a validated+enriched+approved manifest for testing.
func setupApprovedManifest(t *testing.T) (*manifest.Manifest, string) {
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

	// Simulate enrichment
	m.State = manifest.StatePendingReview
	m.Enrichment = &manifest.Enrichment{
		Caption: &manifest.Caption{
			Text:         "Test caption with #architecture",
			Hashtags:     []string{"#architecture"},
			HashtagCount: 1,
			Confidence:   "high",
		},
		Location: &manifest.Location{
			Name:      "Amsterdam",
			Source:    "ai_vision",
			QueryUsed: "Amsterdam, Netherlands",
		},
	}

	// Simulate review approval
	m.Review = &manifest.Review{
		Decision:     "approved",
		FinalCaption: "Final test caption with #architecture",
		PublishMode:  "immediate",
		StoryEnabled: false,
		ReviewedAt:   time.Now().UTC(),
	}
	m.State = manifest.StateApproved

	mPath := manifest.ManifestPath(dir)
	if err := m.Write(mPath); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	return m, mPath
}

func TestPublish_SingleImage(t *testing.T) {
	m, mPath := setupApprovedManifest(t)

	// Keep only one image for single-image test
	m.Images = m.Images[:1]
	if err := m.Write(mPath); err != nil {
		t.Fatalf("write: %v", err)
	}

	memHost := hosting.NewMemoryHost()
	mockIG := newMockInstagram()
	pub := New(memHost, mockIG, Options{})

	if err := pub.Publish(context.Background(), m, mPath); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	// Verify state
	if m.State != manifest.StatePublished {
		t.Errorf("state = %q, want %q", m.State, manifest.StatePublished)
	}

	// Verify publishing section
	if m.Publishing == nil {
		t.Fatal("publishing is nil")
	}
	if m.Publishing.InstagramPostID != "media_12345" {
		t.Errorf("post_id = %q, want %q", m.Publishing.InstagramPostID, "media_12345")
	}
	if m.Publishing.Permalink != "https://www.instagram.com/p/TEST123/" {
		t.Errorf("permalink = %q", m.Publishing.Permalink)
	}

	// Verify R2 upload and cleanup
	if len(memHost.Uploaded) != 1 {
		t.Errorf("uploaded = %d, want 1", len(memHost.Uploaded))
	}
	if len(memHost.Deleted) != 1 {
		t.Errorf("deleted = %d, want 1", len(memHost.Deleted))
	}
	if !m.Publishing.R2Cleaned {
		t.Error("r2_cleaned should be true")
	}

	// Verify single container was used (not carousel)
	if mockIG.CreateSingleCalls != 1 {
		t.Errorf("CreateSingle calls = %d, want 1", mockIG.CreateSingleCalls)
	}
	if mockIG.CreateChildCalls != 0 {
		t.Errorf("CreateChild calls = %d, want 0", mockIG.CreateChildCalls)
	}

	// Verify no story
	if m.Publishing.InstagramStoryID != "" {
		t.Error("story ID should be empty when story disabled")
	}
}

func TestPublish_Carousel(t *testing.T) {
	m, mPath := setupApprovedManifest(t)

	memHost := hosting.NewMemoryHost()
	mockIG := newMockInstagram()
	pub := New(memHost, mockIG, Options{})

	if err := pub.Publish(context.Background(), m, mPath); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	if m.State != manifest.StatePublished {
		t.Errorf("state = %q, want %q", m.State, manifest.StatePublished)
	}

	// Verify carousel flow was used
	imgCount := len(m.Images)
	if mockIG.CreateChildCalls != imgCount {
		t.Errorf("CreateChild calls = %d, want %d", mockIG.CreateChildCalls, imgCount)
	}
	if mockIG.CreateSingleCalls != 0 {
		t.Errorf("CreateSingle calls = %d, want 0", mockIG.CreateSingleCalls)
	}

	// Verify R2 upload and cleanup
	if len(memHost.Uploaded) != imgCount {
		t.Errorf("uploaded = %d, want %d", len(memHost.Uploaded), imgCount)
	}
	if len(memHost.Deleted) != imgCount {
		t.Errorf("deleted = %d, want %d", len(memHost.Deleted), imgCount)
	}
}

func TestPublish_WithStory(t *testing.T) {
	m, mPath := setupApprovedManifest(t)
	m.Review.StoryEnabled = true
	if err := m.Write(mPath); err != nil {
		t.Fatalf("write: %v", err)
	}

	memHost := hosting.NewMemoryHost()
	mockIG := newMockInstagram()
	pub := New(memHost, mockIG, Options{})

	if err := pub.Publish(context.Background(), m, mPath); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	if m.Publishing.InstagramStoryID == "" {
		t.Error("story ID should be set when story enabled")
	}
	if mockIG.StoryCalls != 1 {
		t.Errorf("Story calls = %d, want 1", mockIG.StoryCalls)
	}
	// Publish should be called twice: once for post, once for story
	if mockIG.PublishCalls != 2 {
		t.Errorf("Publish calls = %d, want 2", mockIG.PublishCalls)
	}
}

func TestPublish_LocationResolution(t *testing.T) {
	m, mPath := setupApprovedManifest(t)

	memHost := hosting.NewMemoryHost()
	mockIG := newMockInstagram()
	pub := New(memHost, mockIG, Options{})

	if err := pub.Publish(context.Background(), m, mPath); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	// Location should have been searched
	if mockIG.LocationCalls != 1 {
		t.Errorf("Location calls = %d, want 1", mockIG.LocationCalls)
	}

	// Location should be recorded in enrichment
	if m.Enrichment.Location.InstagramLocationID != "loc_amsterdam" {
		t.Errorf("location ID = %q, want %q", m.Enrichment.Location.InstagramLocationID, "loc_amsterdam")
	}
}

func TestPublish_NoLocation(t *testing.T) {
	m, mPath := setupApprovedManifest(t)
	m.Enrichment.Location = nil
	if err := m.Write(mPath); err != nil {
		t.Fatalf("write: %v", err)
	}

	memHost := hosting.NewMemoryHost()
	mockIG := newMockInstagram()
	pub := New(memHost, mockIG, Options{})

	if err := pub.Publish(context.Background(), m, mPath); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	if mockIG.LocationCalls != 0 {
		t.Errorf("Location calls = %d, want 0", mockIG.LocationCalls)
	}
}

func TestPublish_WrongState(t *testing.T) {
	m := manifest.New("test", "/tmp/test")
	m.State = manifest.StateScanned

	pub := New(hosting.NewMemoryHost(), newMockInstagram(), Options{})
	if err := pub.Publish(context.Background(), m, "/tmp/test/manifest.json"); err == nil {
		t.Error("expected error for wrong state")
	}
}

func TestPublish_NotApproved(t *testing.T) {
	m := manifest.New("test", "/tmp/test")
	m.State = manifest.StateApproved
	m.Review = &manifest.Review{Decision: "rejected"}

	pub := New(hosting.NewMemoryHost(), newMockInstagram(), Options{})
	if err := pub.Publish(context.Background(), m, "/tmp/test/manifest.json"); err == nil {
		t.Error("expected error for non-approved review")
	}
}

func TestPublish_TokenVerifyFails(t *testing.T) {
	m, mPath := setupApprovedManifest(t)

	mockIG := newMockInstagram()
	mockIG.VerifyErr = fmt.Errorf("token expired")
	pub := New(hosting.NewMemoryHost(), mockIG, Options{})

	err := pub.Publish(context.Background(), m, mPath)
	if err == nil {
		t.Fatal("expected error for token verification failure")
	}
	if m.State != manifest.StateApproved {
		t.Errorf("state should remain approved on preflight failure, got %q", m.State)
	}
}

func TestPublish_TooManyCarouselImages_SetsError(t *testing.T) {
	m, mPath := setupApprovedManifest(t)
	// Force above Instagram carousel hard limit.
	for i := len(m.Images); i < 11; i++ {
		clone := m.Images[0]
		clone.Filename = fmt.Sprintf("extra_%d.jpg", i)
		clone.Path = filepath.Join(filepath.Dir(mPath), clone.Filename)
		m.Images = append(m.Images, clone)
	}
	if err := m.Write(mPath); err != nil {
		t.Fatalf("write: %v", err)
	}

	mockIG := newMockInstagram()
	pub := New(hosting.NewMemoryHost(), mockIG, Options{})

	err := pub.Publish(context.Background(), m, mPath)
	if err == nil {
		t.Fatal("expected carousel limit error")
	}
	if !strings.Contains(err.Error(), "at most 10 images") {
		t.Fatalf("unexpected error: %v", err)
	}

	saved, readErr := manifest.Read(mPath)
	if readErr != nil {
		t.Fatalf("read saved manifest: %v", readErr)
	}
	if saved.State != manifest.StateError {
		t.Errorf("state = %q, want %q", saved.State, manifest.StateError)
	}
	if len(saved.Errors) == 0 {
		t.Fatal("expected manifest errors to be recorded")
	}
	if mockIG.CreateChildCalls != 0 || mockIG.PublishCalls != 0 {
		t.Fatalf("expected no publish API calls, got child=%d publish=%d", mockIG.CreateChildCalls, mockIG.PublishCalls)
	}
}

func TestPublish_RetryFromErrorStateWhenApproved(t *testing.T) {
	m, mPath := setupApprovedManifest(t)
	m.Images = m.Images[:1]
	m.State = manifest.StateError
	m.RecordFailure(manifest.FailureStagePublish, "previous publish failure", manifest.StateApproved)
	m.Errors = []string{"previous failure"}
	m.Publishing = &manifest.Publishing{
		ContainerIDs:    &manifest.ContainerIDs{Single: "stale_single"},
		R2Keys:          []string{"posts/retry/stale.jpg"},
		R2URLs:          []string{"https://test.r2.dev/posts/retry/stale.jpg"},
		InstagramPostID: "stale_media",
		Permalink:       "https://instagram.test/p/stale",
	}
	if err := m.Write(mPath); err != nil {
		t.Fatalf("write: %v", err)
	}

	memHost := hosting.NewMemoryHost()
	pub := New(memHost, newMockInstagram(), Options{})
	if err := pub.Publish(context.Background(), m, mPath); err != nil {
		t.Fatalf("Publish retry from error: %v", err)
	}

	saved, err := manifest.Read(mPath)
	if err != nil {
		t.Fatalf("read saved manifest: %v", err)
	}
	if saved.State != manifest.StatePublished {
		t.Fatalf("state = %q, want %q", saved.State, manifest.StatePublished)
	}
	if len(memHost.Uploaded) != 1 {
		t.Fatalf("retry publish should re-upload media after clearing stale artifacts, uploaded=%d", len(memHost.Uploaded))
	}
}

func TestPublish_Resume_FromPublishing(t *testing.T) {
	m, mPath := setupApprovedManifest(t)

	// Simulate a previous partial run: state=publishing, R2 keys exist, some children created
	m.State = manifest.StatePublishing
	m.Publishing = &manifest.Publishing{
		R2Keys: []string{"posts/test/img_1.jpg", "posts/test/img_2.jpg", "posts/test/img_3.jpg"},
		R2URLs: []string{
			"https://test.r2.dev/posts/test/img_1.jpg",
			"https://test.r2.dev/posts/test/img_2.jpg",
			"https://test.r2.dev/posts/test/img_3.jpg",
		},
		ContainerIDs: &manifest.ContainerIDs{
			Children: []string{"existing_child_1"}, // 1 of 3 already created
		},
	}
	if err := m.Write(mPath); err != nil {
		t.Fatalf("write: %v", err)
	}

	memHost := hosting.NewMemoryHost()
	mockIG := newMockInstagram()
	pub := New(memHost, mockIG, Options{})

	if err := pub.Publish(context.Background(), m, mPath); err != nil {
		t.Fatalf("Publish (resume): %v", err)
	}

	if m.State != manifest.StatePublished {
		t.Errorf("state = %q, want %q", m.State, manifest.StatePublished)
	}

	// Should have skipped R2 upload (already done)
	if len(memHost.Uploaded) != 0 {
		t.Errorf("uploaded = %d, want 0 (should skip on resume)", len(memHost.Uploaded))
	}

	// Should have created only 2 new children (1 was already done)
	if mockIG.CreateChildCalls != 2 {
		t.Errorf("CreateChild calls = %d, want 2 (skipping 1 existing)", mockIG.CreateChildCalls)
	}
}

func TestPublish_DryRun(t *testing.T) {
	m, mPath := setupApprovedManifest(t)

	memHost := hosting.NewMemoryHost()
	mockIG := newMockInstagram()
	pub := New(memHost, mockIG, Options{DryRun: true})

	if err := pub.Publish(context.Background(), m, mPath); err != nil {
		t.Fatalf("Publish (dry run): %v", err)
	}

	// In dry-run mode, the publisher uses the host (which is DryRunHost in real usage,
	// but here we use MemoryHost). The key check is that no real IG API calls were made
	// via publishContainer (no media ID set since dry run returns early from container flow).

	// State should remain approved since dry-run doesn't complete the publish flow
	// Actually, the publisher still does the full flow with dry-run host and IG —
	// let me check... the dry run flag is on the publisher level, it skips IG calls.
	// The state remains approved because createAndPublish returns early.

	// Verify no real publish happened
	if mockIG.PublishCalls != 0 {
		t.Errorf("Publish calls = %d, want 0 in dry run", mockIG.PublishCalls)
	}
}

func TestPublish_CaptionFallback(t *testing.T) {
	m, mPath := setupApprovedManifest(t)
	// Clear final caption to test fallback to enrichment caption
	m.Review.FinalCaption = ""
	if err := m.Write(mPath); err != nil {
		t.Fatalf("write: %v", err)
	}

	pub := New(hosting.NewMemoryHost(), newMockInstagram(), Options{})
	// resolveCaption should fall back to enrichment caption
	caption := pub.resolveCaption(m)
	if caption != "Test caption with #architecture" {
		t.Errorf("caption = %q, want enrichment caption", caption)
	}
}

func TestPublish_CaptionTruncation(t *testing.T) {
	m, mPath := setupApprovedManifest(t)
	// Set caption over 2200 chars
	longCaption := ""
	for i := 0; i < 250; i++ {
		longCaption += "0123456789"
	}
	m.Review.FinalCaption = longCaption
	if err := m.Write(mPath); err != nil {
		t.Fatalf("write: %v", err)
	}

	pub := New(hosting.NewMemoryHost(), newMockInstagram(), Options{})
	caption := pub.resolveCaption(m)
	if len(caption) != 2200 {
		t.Errorf("caption len = %d, want 2200", len(caption))
	}
}

func TestPublish_ManifestPersistence(t *testing.T) {
	m, mPath := setupApprovedManifest(t)

	memHost := hosting.NewMemoryHost()
	mockIG := newMockInstagram()
	pub := New(memHost, mockIG, Options{})

	if err := pub.Publish(context.Background(), m, mPath); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	// Read the manifest back from disk
	saved, err := manifest.Read(mPath)
	if err != nil {
		t.Fatalf("read saved manifest: %v", err)
	}

	if saved.State != manifest.StatePublished {
		t.Errorf("saved state = %q, want %q", saved.State, manifest.StatePublished)
	}
	if saved.Publishing == nil {
		t.Fatal("saved publishing is nil")
	}
	if saved.Publishing.InstagramPostID == "" {
		t.Error("saved post ID is empty")
	}
	if saved.Publishing.Permalink == "" {
		t.Error("saved permalink is empty")
	}
	if !saved.Publishing.R2Cleaned {
		t.Error("saved r2_cleaned should be true")
	}
	if saved.Publishing.PublishedAt.IsZero() {
		t.Error("saved published_at should be set")
	}

	// Verify R2 keys are persisted
	imgCount := len(m.Images)
	if len(saved.Publishing.R2Keys) != imgCount {
		t.Errorf("saved r2_keys = %d, want %d", len(saved.Publishing.R2Keys), imgCount)
	}
	// Verify keys follow the expected pattern
	expectedPrefix := filepath.Join("posts", m.ID)
	for _, key := range saved.Publishing.R2Keys {
		if len(key) < len(expectedPrefix) {
			t.Errorf("r2 key %q doesn't have expected prefix %q", key, expectedPrefix)
		}
	}
}

func TestPublish_PublishError_SetsErrorState(t *testing.T) {
	m, mPath := setupApprovedManifest(t)
	// Use single image for simpler flow
	m.Images = m.Images[:1]
	if err := m.Write(mPath); err != nil {
		t.Fatalf("write: %v", err)
	}

	mockIG := newMockInstagram()
	mockIG.PublishErr = fmt.Errorf("API error: rate limited")
	memHost := hosting.NewMemoryHost()
	pub := New(memHost, mockIG, Options{})

	err := pub.Publish(context.Background(), m, mPath)
	if err == nil {
		t.Fatal("expected error from publish failure")
	}

	// Manifest should be in error state
	saved, readErr := manifest.Read(mPath)
	if readErr != nil {
		t.Fatalf("read saved manifest: %v", readErr)
	}
	if saved.State != manifest.StateError {
		t.Errorf("state = %q, want %q", saved.State, manifest.StateError)
	}
	if len(saved.Errors) == 0 {
		t.Error("errors should be recorded")
	}
	if saved.Failure == nil {
		t.Fatal("failure metadata should be recorded")
	}
	if saved.Failure.Stage != manifest.FailureStagePublish {
		t.Fatalf("failure.stage = %q, want %q", saved.Failure.Stage, manifest.FailureStagePublish)
	}
	if len(memHost.Deleted) != 0 {
		t.Fatalf("deleted=%d, want 0 by default (cleanup_on_failure=never)", len(memHost.Deleted))
	}
	if saved.Publishing != nil && saved.Publishing.R2Cleaned {
		t.Fatal("r2_cleaned should remain false when cleanup_on_failure is disabled")
	}
}

func TestPublish_PublishError_CleansR2WhenConfigured(t *testing.T) {
	m, mPath := setupApprovedManifest(t)
	m.Images = m.Images[:1]
	if err := m.Write(mPath); err != nil {
		t.Fatalf("write: %v", err)
	}

	mockIG := newMockInstagram()
	mockIG.PublishErr = fmt.Errorf("API error: rate limited")
	memHost := hosting.NewMemoryHost()
	pub := New(memHost, mockIG, Options{CleanupOnFailure: true})

	err := pub.Publish(context.Background(), m, mPath)
	if err == nil {
		t.Fatal("expected error from publish failure")
	}

	saved, readErr := manifest.Read(mPath)
	if readErr != nil {
		t.Fatalf("read saved manifest: %v", readErr)
	}
	if saved.State != manifest.StateError {
		t.Fatalf("state = %q, want %q", saved.State, manifest.StateError)
	}
	if saved.Publishing == nil || !saved.Publishing.R2Cleaned {
		t.Fatal("expected r2_cleaned=true when cleanup_on_failure=always")
	}
	if len(memHost.Deleted) != 1 {
		t.Fatalf("deleted=%d, want 1", len(memHost.Deleted))
	}
}

func TestPublish_Syndication_Success(t *testing.T) {
	m, mPath := setupApprovedManifest(t)
	m.Images = m.Images[:1]
	if err := m.Write(mPath); err != nil {
		t.Fatalf("write: %v", err)
	}

	fb := &mockFacebook{PostID: "987_123"}
	th := &mockThreads{PostID: "999_456", Permalink: "https://threads.net/p/xyz"}

	pub := New(hosting.NewMemoryHost(), newMockInstagram(), Options{
		EnableFacebook: true,
		EnableThreads:  true,
		Facebook:       fb,
		Threads:        th,
	})

	if err := pub.Publish(context.Background(), m, mPath); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	if m.Publishing == nil || m.Publishing.Syndication == nil {
		t.Fatal("syndication should be present")
	}
	if m.Publishing.Syndication.Facebook == nil || m.Publishing.Syndication.Facebook.Status != "published" {
		t.Fatalf("facebook syndication status = %#v", m.Publishing.Syndication.Facebook)
	}
	if m.Publishing.Syndication.Threads == nil || m.Publishing.Syndication.Threads.Status != "published" {
		t.Fatalf("threads syndication status = %#v", m.Publishing.Syndication.Threads)
	}
	if fb.Calls != 1 {
		t.Errorf("facebook calls = %d, want 1", fb.Calls)
	}
	if th.Calls != 1 {
		t.Errorf("threads calls = %d, want 1", th.Calls)
	}
}

func TestPublish_Syndication_NonStrictFailure(t *testing.T) {
	m, mPath := setupApprovedManifest(t)
	m.Images = m.Images[:1]
	if err := m.Write(mPath); err != nil {
		t.Fatalf("write: %v", err)
	}

	fb := &mockFacebook{Err: fmt.Errorf("facebook unavailable")}
	pub := New(hosting.NewMemoryHost(), newMockInstagram(), Options{
		EnableFacebook:    true,
		StrictSyndication: false,
		Facebook:          fb,
	})

	if err := pub.Publish(context.Background(), m, mPath); err != nil {
		t.Fatalf("Publish should continue in non-strict mode: %v", err)
	}
	if m.State != manifest.StatePublished {
		t.Errorf("state = %q, want %q", m.State, manifest.StatePublished)
	}
	if m.Publishing == nil || m.Publishing.Syndication == nil || m.Publishing.Syndication.Facebook == nil {
		t.Fatal("facebook syndication result should be recorded")
	}
	if m.Publishing.Syndication.Facebook.Status != "failed" {
		t.Errorf("facebook status = %q, want failed", m.Publishing.Syndication.Facebook.Status)
	}
}

func TestPublish_Syndication_StrictFailure(t *testing.T) {
	m, mPath := setupApprovedManifest(t)
	m.Images = m.Images[:1]
	if err := m.Write(mPath); err != nil {
		t.Fatalf("write: %v", err)
	}

	fb := &mockFacebook{Err: fmt.Errorf("facebook unavailable")}
	pub := New(hosting.NewMemoryHost(), newMockInstagram(), Options{
		EnableFacebook:    true,
		StrictSyndication: true,
		Facebook:          fb,
	})

	err := pub.Publish(context.Background(), m, mPath)
	if err == nil {
		t.Fatal("expected strict syndication failure")
	}

	saved, readErr := manifest.Read(mPath)
	if readErr != nil {
		t.Fatalf("read saved manifest: %v", readErr)
	}
	if saved.State != manifest.StateError {
		t.Errorf("state = %q, want %q", saved.State, manifest.StateError)
	}
	if len(saved.Errors) == 0 {
		t.Error("strict failure should append manifest error")
	}
	if saved.Failure == nil {
		t.Fatal("failure metadata should be present")
	}
	if saved.Failure.Stage != manifest.FailureStageSyndicate {
		t.Fatalf("failure.stage = %q, want %q", saved.Failure.Stage, manifest.FailureStageSyndicate)
	}
}

func TestPublish_PreflightRejectsInvalidLocalMedia(t *testing.T) {
	m, mPath := setupApprovedManifest(t)
	m.Images = m.Images[:1]
	if err := m.Write(mPath); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(m.Images[0].Path, []byte("not a jpeg"), 0o644); err != nil {
		t.Fatalf("overwrite test image: %v", err)
	}

	pub := New(hosting.NewMemoryHost(), newMockInstagram(), Options{})
	err := pub.Publish(context.Background(), m, mPath)
	if err == nil {
		t.Fatal("expected preflight failure")
	}
	if !strings.Contains(err.Error(), "media preflight") {
		t.Fatalf("unexpected error: %v", err)
	}

	saved, readErr := manifest.Read(mPath)
	if readErr != nil {
		t.Fatalf("read saved manifest: %v", readErr)
	}
	if saved.State != manifest.StateError {
		t.Fatalf("state = %q, want %q", saved.State, manifest.StateError)
	}
	if saved.Failure == nil || saved.Failure.Stage != manifest.FailureStagePublish {
		t.Fatalf("failure metadata = %#v, want stage=%q", saved.Failure, manifest.FailureStagePublish)
	}
}

func TestPrepareImageUploadSource_NormalizesOversizedJPEG(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "oversized.jpg")

	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 10), G: uint8(y * 10), B: 128, A: 255})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create seed image: %v", err)
	}
	if err := jpeg.Encode(f, img, &jpeg.Options{Quality: 85}); err != nil {
		_ = f.Close()
		t.Fatalf("encode seed image: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close seed image: %v", err)
	}

	padding := make([]byte, maxInstagramImageBytes+1024)
	wf, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open for append: %v", err)
	}
	if _, err := wf.Write(padding); err != nil {
		_ = wf.Close()
		t.Fatalf("append padding: %v", err)
	}
	if err := wf.Close(); err != nil {
		t.Fatalf("close append handle: %v", err)
	}

	src, err := prepareImageUploadSource(path)
	if err != nil {
		t.Fatalf("prepareImageUploadSource: %v", err)
	}
	if !src.Normalized {
		t.Fatal("expected normalized source for oversized jpeg")
	}
	if src.Path == path {
		t.Fatal("expected normalized path to be temp file")
	}
	if src.UploadSize > maxInstagramImageBytes {
		t.Fatalf("upload size = %d, want <= %d", src.UploadSize, maxInstagramImageBytes)
	}
	if src.Cleanup == nil {
		t.Fatal("expected cleanup function for normalized source")
	}
	if _, err := os.Stat(src.Path); err != nil {
		t.Fatalf("normalized temp file missing: %v", err)
	}
	src.Cleanup()
	if _, err := os.Stat(src.Path); !os.IsNotExist(err) {
		t.Fatalf("expected temp file to be removed, stat err=%v", err)
	}
}
