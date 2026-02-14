// Package manifest defines the post manifest schema and provides
// read/write/transition operations. The manifest is the single data
// contract that flows through all pipeline stages.
package manifest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Version is the current manifest schema version.
const Version = 1

// State represents a post's position in the pipeline.
type State string

const (
	StateScanned       State = "scanned"
	StateValidated     State = "validated"
	StateEnriched      State = "enriched"
	StatePendingReview State = "pending_review"
	StateApproved      State = "approved"
	StateRejected      State = "rejected"
	StatePublishing    State = "publishing"
	StatePublished     State = "published"
	StateScheduled     State = "scheduled"
	StateArchived      State = "archived"
	StateError         State = "error"
)

// validTransitions defines which state transitions are allowed.
var validTransitions = map[State][]State{
	StateScanned:       {StateValidated, StateError},
	StateValidated:     {StatePendingReview, StateError},
	StateEnriched:      {StatePendingReview, StateError},
	StatePendingReview: {StateApproved, StateRejected, StateValidated}, // validated = re-enrich
	StateApproved:      {StatePublishing, StateScheduled, StateError},
	StateRejected:      {StatePendingReview}, // can re-review
	StatePublishing:    {StatePublished, StateError},
	StateScheduled:     {StatePublishing, StateApproved, StateError}, // approved = unschedule
	StatePublished:     {StateArchived, StateError},
	StateError:         {StateScanned}, // re-scan to retry
}

// GPS holds latitude and longitude.
type GPS struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

// EXIF holds extracted metadata from an image.
type EXIF struct {
	Camera       string     `json:"camera,omitempty"`
	Lens         string     `json:"lens,omitempty"`
	FocalLength  string     `json:"focal_length,omitempty"`
	Aperture     string     `json:"aperture,omitempty"`
	ShutterSpeed string     `json:"shutter_speed,omitempty"`
	ISO          int        `json:"iso,omitempty"`
	CaptureDate  *time.Time `json:"capture_date,omitempty"`
	GPS          *GPS       `json:"gps,omitempty"`
}

// Image represents one image in the post.
type Image struct {
	Filename    string `json:"filename"`
	Path        string `json:"path"`
	Order       int    `json:"order"`
	IsHero      bool   `json:"is_hero"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	AspectRatio string `json:"aspect_ratio"`
	EXIF        EXIF   `json:"exif"`
}

// ValidationIssue describes a problem found during validation.
type ValidationIssue struct {
	Severity string `json:"severity"` // "error" or "warning"
	Message  string `json:"message"`
}

// Validation holds the results of the validation stage.
type Validation struct {
	Passed                bool              `json:"passed"`
	Issues                []ValidationIssue `json:"issues"`
	AspectRatioConsistent bool              `json:"aspect_ratio_consistent"`
	ResolvedAspectRatio   string            `json:"resolved_aspect_ratio,omitempty"`
	ImageCountValid       bool              `json:"image_count_valid"`
	ImageCount            int               `json:"image_count"`
}

// Caption holds the AI-generated caption.
type Caption struct {
	Text         string    `json:"text"`
	Hashtags     []string  `json:"hashtags"`
	HashtagCount int       `json:"hashtag_count"`
	GeneratedAt  time.Time `json:"generated_at"`
	Model        string    `json:"model"`
	Confidence   string    `json:"confidence"` // high, medium, low, error
}

// Location holds the identified location.
type Location struct {
	Name                  string `json:"name"`
	Source                string `json:"source"` // ai_vision, exif_gps
	QueryUsed             string `json:"query_used,omitempty"`
	InstagramLocationID   string `json:"instagram_location_id,omitempty"`
	InstagramLocationName string `json:"instagram_location_name,omitempty"`
	Confidence            string `json:"confidence,omitempty"`
	FallbackUsed          bool   `json:"fallback_used"`
}

// MusicSuggestion holds the AI-suggested music track.
type MusicSuggestion struct {
	Artist      string    `json:"artist"`
	Title       string    `json:"title"`
	Mood        string    `json:"mood,omitempty"`
	GeneratedAt time.Time `json:"generated_at"`
}

// Enrichment holds all AI-generated metadata.
type Enrichment struct {
	Caption         *Caption         `json:"caption,omitempty"`
	Location        *Location        `json:"location,omitempty"`
	MusicSuggestion *MusicSuggestion `json:"music_suggestion,omitempty"`
}

// Review holds the user's review decision.
type Review struct {
	Decision      string    `json:"decision"` // approved, rejected
	FinalCaption  string    `json:"final_caption,omitempty"`
	CaptionEdited bool      `json:"caption_edited"`
	PublishMode   string    `json:"publish_mode,omitempty"` // immediate, queued
	StoryEnabled  bool      `json:"story_enabled"`
	ReviewedAt    time.Time `json:"reviewed_at"`
}

// ContainerIDs records Instagram container IDs for partial failure recovery.
type ContainerIDs struct {
	Children []string `json:"children,omitempty"`
	Carousel string   `json:"carousel,omitempty"`
	Single   string   `json:"single,omitempty"`
}

// Publishing holds the Instagram API publishing results.
type Publishing struct {
	InstagramPostID  string        `json:"instagram_post_id,omitempty"`
	InstagramStoryID string        `json:"instagram_story_id,omitempty"`
	Permalink        string        `json:"permalink,omitempty"`
	PublishedAt      time.Time     `json:"published_at"`
	ContainerIDs     *ContainerIDs `json:"container_ids,omitempty"`
	R2Keys           []string      `json:"r2_keys,omitempty"`
	R2URLs           []string      `json:"r2_urls,omitempty"`
	R2Cleaned        bool          `json:"r2_cleaned"`
	Syndication      *Syndication  `json:"syndication,omitempty"`
}

// Syndication stores publish outcomes for optional cross-platform destinations.
type Syndication struct {
	Facebook *SyndicationTarget `json:"facebook,omitempty"`
	Threads  *SyndicationTarget `json:"threads,omitempty"`
}

// SyndicationTarget stores one destination result (facebook or threads).
type SyndicationTarget struct {
	Enabled     bool       `json:"enabled"`
	Status      string     `json:"status,omitempty"` // published, failed, skipped, dry_run
	PostID      string     `json:"post_id,omitempty"`
	Permalink   string     `json:"permalink,omitempty"`
	Mode        string     `json:"mode,omitempty"`
	Error       string     `json:"error,omitempty"`
	AttemptedAt *time.Time `json:"attempted_at,omitempty"`
	PublishedAt *time.Time `json:"published_at,omitempty"`
}

// Scheduling holds schedule queue assignment.
type Scheduling struct {
	ScheduledFor *time.Time `json:"scheduled_for,omitempty"`
	SlotID       string     `json:"slot_id,omitempty"`
	BatchID      string     `json:"batch_id,omitempty"`
	QueuedAt     *time.Time `json:"queued_at,omitempty"`
	Method       string     `json:"method,omitempty"` // instagram_publish_time, client_queue
}

// Archival holds post-publish archival info.
type Archival struct {
	ArchivedAt      time.Time `json:"archived_at"`
	ArchiveDir      string    `json:"archive_dir"`
	LogEntryWritten bool      `json:"log_entry_written"`
}

// Manifest is the top-level post manifest.
type Manifest struct {
	Version    int         `json:"version"`
	ID         string      `json:"id"`
	SourceDir  string      `json:"source_dir"`
	State      State       `json:"state"`
	CreatedAt  time.Time   `json:"created_at"`
	UpdatedAt  time.Time   `json:"updated_at"`
	Images     []Image     `json:"images"`
	Validation *Validation `json:"validation,omitempty"`
	Enrichment *Enrichment `json:"enrichment,omitempty"`
	Review     *Review     `json:"review,omitempty"`
	Publishing *Publishing `json:"publishing,omitempty"`
	Scheduling *Scheduling `json:"scheduling,omitempty"`
	Archival   *Archival   `json:"archival,omitempty"`
	Errors     []string    `json:"errors,omitempty"`
}

// New creates a new manifest for a post directory.
func New(id, sourceDir string) *Manifest {
	now := time.Now().UTC()
	return &Manifest{
		Version:   Version,
		ID:        id,
		SourceDir: sourceDir,
		State:     StateScanned,
		CreatedAt: now,
		UpdatedAt: now,
		Images:    []Image{},
		Errors:    []string{},
	}
}

// Transition changes the manifest state, enforcing valid transitions.
func (m *Manifest) Transition(to State) error {
	allowed, ok := validTransitions[m.State]
	if !ok {
		return fmt.Errorf("no transitions defined from state %q", m.State)
	}
	for _, s := range allowed {
		if s == to {
			m.State = to
			m.UpdatedAt = time.Now().UTC()
			return nil
		}
	}
	return fmt.Errorf("invalid state transition: %q → %q (allowed: %v)", m.State, to, allowed)
}

// Write serializes the manifest to a file using atomic write (temp + rename).
func (m *Manifest) Write(path string) error {
	m.UpdatedAt = time.Now().UTC()

	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	b = append(b, '\n')

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create directory %s: %w", dir, err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return fmt.Errorf("write temp file %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp) // best-effort cleanup
		return fmt.Errorf("rename %s → %s: %w", tmp, path, err)
	}
	return nil
}

// Read deserializes a manifest from a file.
func Read(path string) (*Manifest, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read manifest %s: %w", path, err)
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("parse manifest %s: %w", path, err)
	}
	return &m, nil
}

// ManifestPath returns the default manifest file path for a post directory.
func ManifestPath(postDir string) string {
	return filepath.Join(postDir, "manifest.json")
}
