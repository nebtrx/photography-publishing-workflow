// Package publisher orchestrates the full Instagram publishing flow:
// upload to R2 → resolve location → create containers → poll → publish → story → cleanup.
package publisher

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"photography-publishing-workflow/internal/hosting"
	"photography-publishing-workflow/internal/manifest"
	"photography-publishing-workflow/internal/obslog"
)

// InstagramAPI defines the Instagram Graph API operations needed by the publisher.
// The instagram.Client satisfies this interface.
type InstagramAPI interface {
	VerifyToken() (string, error)
	CreateChildContainer(imageURL string) (string, error)
	CreateSingleContainer(imageURL, caption, locationID string) (string, error)
	CreateCarouselContainer(childIDs []string, caption, locationID string) (string, error)
	CreateStoryContainer(imageURL string) (string, error)
	PollContainer(containerID string, pollInterval time.Duration, maxPolls int) error
	Publish(containerID string) (string, error)
	GetPermalink(mediaID string) (string, error)
	SearchLocation(query string) (string, string, error)
}

const (
	pollInterval = 5 * time.Second
	maxPolls     = 60
)

// Publisher orchestrates the publishing flow.
type Publisher struct {
	host              hosting.Host
	ig                InstagramAPI
	logger            *log.Logger
	dryRun            bool
	enableFacebook    bool
	enableThreads     bool
	strictSyndication bool
	facebook          FacebookSyndicator
	threads           ThreadsSyndicator
}

// Options configures the publisher.
type Options struct {
	DryRun            bool
	EnableFacebook    bool
	EnableThreads     bool
	StrictSyndication bool
	Facebook          FacebookSyndicator
	Threads           ThreadsSyndicator
	LogOutput         io.Writer
}

// New creates a new Publisher.
func New(host hosting.Host, ig InstagramAPI, opts Options) *Publisher {
	logOutput := opts.LogOutput
	if logOutput == nil {
		logOutput = os.Stderr
	}

	return &Publisher{
		host:              host,
		ig:                ig,
		logger:            log.New(logOutput, "[publish] ", log.LstdFlags),
		dryRun:            opts.DryRun,
		enableFacebook:    opts.EnableFacebook,
		enableThreads:     opts.EnableThreads,
		strictSyndication: opts.StrictSyndication,
		facebook:          opts.Facebook,
		threads:           opts.Threads,
	}
}

// Publish executes the full publishing flow for a manifest.
// Supports resuming from a partially failed "publishing" state.
func (p *Publisher) Publish(ctx context.Context, m *manifest.Manifest, manifestPath string) error {
	// 1. Validate state
	if m.State != manifest.StateApproved && m.State != manifest.StatePublishing {
		return fmt.Errorf("manifest state is %q, expected %q or %q", m.State, manifest.StateApproved, manifest.StatePublishing)
	}

	if m.Review == nil || m.Review.Decision != "approved" {
		return fmt.Errorf("manifest review decision is not 'approved'")
	}

	if len(m.Images) == 0 {
		return fmt.Errorf("manifest has no images")
	}

	// Initialize publishing section if needed
	if m.Publishing == nil {
		m.Publishing = &manifest.Publishing{}
	}

	// 2. Pre-flight: verify token
	verifyStart := obslog.Intent(
		p.logger,
		"publisher",
		m.ID,
		m.ID,
		"verify_instagram_token",
		"verify instagram access token before publish",
		nil,
	)
	if !p.dryRun {
		username, err := p.ig.VerifyToken()
		if err != nil {
			obslog.Result(p.logger, "publisher", m.ID, m.ID, "verify_instagram_token", verifyStart, err, nil)
			return fmt.Errorf("pre-flight token check: %w", err)
		}
		p.logger.Printf("Token verified for @%s", username)
		obslog.Result(
			p.logger,
			"publisher",
			m.ID,
			m.ID,
			"verify_instagram_token",
			verifyStart,
			nil,
			map[string]any{"username": username},
		)
	} else {
		p.logger.Printf("[DRY RUN] Would verify Instagram token")
		obslog.Result(
			p.logger,
			"publisher",
			m.ID,
			m.ID,
			"verify_instagram_token",
			verifyStart,
			nil,
			map[string]any{"dry_run": true},
		)
	}

	// 3. Upload images to R2
	if err := p.uploadImages(ctx, m, manifestPath); err != nil {
		return fmt.Errorf("upload images: %w", err)
	}

	// 4. Transition to publishing (if not already resuming)
	if m.State == manifest.StateApproved {
		if err := m.Transition(manifest.StatePublishing); err != nil {
			return fmt.Errorf("transition to publishing: %w", err)
		}
		if err := m.Write(manifestPath); err != nil {
			return fmt.Errorf("write manifest after transition: %w", err)
		}
	}

	// 5. Resolve Instagram location
	locationID := p.resolveLocation(m)

	// 6. Get the final caption
	caption := p.resolveCaption(m)

	// 7. Create containers + poll + publish
	instagramStart := obslog.Intent(
		p.logger,
		"publisher",
		m.ID,
		m.ID,
		"publish_instagram",
		"publish approved post to instagram feed",
		map[string]any{"image_count": len(m.Images)},
	)
	if err := p.createAndPublish(ctx, m, manifestPath, caption, locationID); err != nil {
		obslog.Result(p.logger, "publisher", m.ID, m.ID, "publish_instagram", instagramStart, err, nil)
		p.setError(m, manifestPath, err)
		return err
	}
	obslog.Result(
		p.logger,
		"publisher",
		m.ID,
		m.ID,
		"publish_instagram",
		instagramStart,
		nil,
		map[string]any{"post_id": m.Publishing.InstagramPostID, "permalink": m.Publishing.Permalink},
	)

	// 8. Story (if enabled)
	if m.Review.StoryEnabled {
		if err := p.publishStory(ctx, m, manifestPath); err != nil {
			// Story failure is non-fatal — post is already published
			p.logger.Printf("Warning: story publishing failed: %v", err)
		}
	}

	// 9. Optional syndication (facebook / threads)
	if err := p.syndicate(ctx, m, manifestPath, caption); err != nil {
		if p.strictSyndication {
			p.setError(m, manifestPath, err)
			return err
		}
		p.logger.Printf("Warning: syndication failed: %v", err)
	}

	// 10. Cleanup R2
	p.cleanupR2(ctx, m, manifestPath)

	// 11. Transition to published
	if err := m.Transition(manifest.StatePublished); err != nil {
		return fmt.Errorf("transition to published: %w", err)
	}
	m.Publishing.PublishedAt = time.Now().UTC()
	if err := m.Write(manifestPath); err != nil {
		return fmt.Errorf("write manifest after publish: %w", err)
	}

	p.logger.Printf("Published: post_id=%s permalink=%s", m.Publishing.InstagramPostID, m.Publishing.Permalink)
	if m.Publishing.InstagramStoryID != "" {
		p.logger.Printf("Story published: story_id=%s", m.Publishing.InstagramStoryID)
	}

	return nil
}

// uploadImages uploads all images to R2, skipping if already done (resume case).
func (p *Publisher) uploadImages(ctx context.Context, m *manifest.Manifest, manifestPath string) error {
	// If R2 keys already exist and match image count, skip (resume case)
	if len(m.Publishing.R2Keys) == len(m.Images) && len(m.Publishing.R2URLs) == len(m.Images) {
		p.logger.Printf("R2 images already uploaded (%d), skipping", len(m.Publishing.R2Keys))
		return nil
	}

	keys := make([]string, 0, len(m.Images))
	urls := make([]string, 0, len(m.Images))

	for _, img := range m.Images {
		key := fmt.Sprintf("posts/%s/%s", m.ID, img.Filename)

		publicURL, err := p.host.Upload(ctx, key, img.Path)
		if err != nil {
			// Retry once
			p.logger.Printf("Upload failed for %s, retrying: %v", img.Filename, err)
			publicURL, err = p.host.Upload(ctx, key, img.Path)
			if err != nil {
				// Cleanup already uploaded
				p.cleanupKeys(ctx, keys)
				return fmt.Errorf("upload %s (after retry): %w", img.Filename, err)
			}
		}

		keys = append(keys, key)
		urls = append(urls, publicURL)
		p.logger.Printf("Uploaded %s → %s", img.Filename, publicURL)
	}

	m.Publishing.R2Keys = keys
	m.Publishing.R2URLs = urls

	// Write manifest with R2 keys for recovery
	if err := m.Write(manifestPath); err != nil {
		return fmt.Errorf("write manifest after upload: %w", err)
	}

	return nil
}

// resolveLocation searches for an Instagram location ID.
func (p *Publisher) resolveLocation(m *manifest.Manifest) string {
	if m.Enrichment == nil || m.Enrichment.Location == nil {
		return ""
	}

	// If already resolved from a previous run
	if m.Enrichment.Location.InstagramLocationID != "" {
		p.logger.Printf("Using cached location ID: %s (%s)",
			m.Enrichment.Location.InstagramLocationID, m.Enrichment.Location.InstagramLocationName)
		return m.Enrichment.Location.InstagramLocationID
	}

	query := m.Enrichment.Location.QueryUsed
	if query == "" {
		query = m.Enrichment.Location.Name
	}
	if query == "" {
		return ""
	}

	if p.dryRun {
		p.logger.Printf("[DRY RUN] Would search Instagram location: %q", query)
		return ""
	}

	locID, locName, err := p.ig.SearchLocation(query)
	if err != nil {
		p.logger.Printf("Warning: location search failed: %v", err)
		return ""
	}
	if locID == "" {
		p.logger.Printf("No Instagram location found for %q", query)
		return ""
	}

	p.logger.Printf("Location resolved: %s (%s)", locName, locID)
	m.Enrichment.Location.InstagramLocationID = locID
	m.Enrichment.Location.InstagramLocationName = locName
	return locID
}

// resolveCaption returns the final caption for publishing.
func (p *Publisher) resolveCaption(m *manifest.Manifest) string {
	caption := ""
	if m.Review != nil && m.Review.FinalCaption != "" {
		caption = m.Review.FinalCaption
	} else if m.Enrichment != nil && m.Enrichment.Caption != nil {
		caption = m.Enrichment.Caption.Text
	}

	// Enforce Instagram's 2200 char limit
	if len(caption) > 2200 {
		p.logger.Printf("Warning: caption truncated from %d to 2200 chars", len(caption))
		caption = caption[:2200]
	}

	return caption
}

// createAndPublish creates media containers, polls, and publishes.
func (p *Publisher) createAndPublish(ctx context.Context, m *manifest.Manifest, manifestPath, caption, locationID string) error {
	if m.Publishing.ContainerIDs == nil {
		m.Publishing.ContainerIDs = &manifest.ContainerIDs{}
	}

	if len(m.Images) == 1 {
		return p.publishSingle(ctx, m, manifestPath, caption, locationID)
	}
	return p.publishCarousel(ctx, m, manifestPath, caption, locationID)
}

// publishSingle handles single-image post publishing.
func (p *Publisher) publishSingle(ctx context.Context, m *manifest.Manifest, manifestPath, caption, locationID string) error {
	imageURL := m.Publishing.R2URLs[0]

	// Create container (skip if already exists from previous run)
	containerID := m.Publishing.ContainerIDs.Single
	if containerID == "" {
		if p.dryRun {
			p.logger.Printf("[DRY RUN] Would create single container: image=%s caption_len=%d location=%s", imageURL, len(caption), locationID)
			p.logger.Printf("[DRY RUN] Would poll container until FINISHED")
			p.logger.Printf("[DRY RUN] Would publish container")
			p.logger.Printf("[DRY RUN] Would fetch permalink")
			return nil
		}

		var err error
		containerID, err = p.ig.CreateSingleContainer(imageURL, caption, locationID)
		if err != nil {
			return fmt.Errorf("create single container: %w", err)
		}
		m.Publishing.ContainerIDs.Single = containerID
		if err := m.Write(manifestPath); err != nil {
			return fmt.Errorf("write manifest after container creation: %w", err)
		}
		p.logger.Printf("Created single container: %s", containerID)
	} else {
		p.logger.Printf("Resuming with existing container: %s", containerID)
	}

	// Poll
	if err := p.ig.PollContainer(containerID, pollInterval, maxPolls); err != nil {
		return fmt.Errorf("poll single container: %w", err)
	}
	p.logger.Printf("Container ready: %s", containerID)

	// Publish
	return p.publishContainer(m, manifestPath, containerID)
}

// publishCarousel handles carousel post publishing.
func (p *Publisher) publishCarousel(ctx context.Context, m *manifest.Manifest, manifestPath, caption, locationID string) error {
	// Create child containers (skip already created ones for resume)
	existingChildren := len(m.Publishing.ContainerIDs.Children)
	for i := existingChildren; i < len(m.Images); i++ {
		imageURL := m.Publishing.R2URLs[i]

		if p.dryRun {
			p.logger.Printf("[DRY RUN] Would create child container %d: %s", i+1, imageURL)
			continue
		}

		childID, err := p.ig.CreateChildContainer(imageURL)
		if err != nil {
			return fmt.Errorf("create child container %d: %w", i+1, err)
		}
		m.Publishing.ContainerIDs.Children = append(m.Publishing.ContainerIDs.Children, childID)

		// Write manifest after each child for recovery
		if err := m.Write(manifestPath); err != nil {
			return fmt.Errorf("write manifest after child %d: %w", i+1, err)
		}
		p.logger.Printf("Created child container %d/%d: %s", i+1, len(m.Images), childID)
	}

	// Poll all children
	if !p.dryRun {
		for i, childID := range m.Publishing.ContainerIDs.Children {
			if err := p.ig.PollContainer(childID, pollInterval, maxPolls); err != nil {
				return fmt.Errorf("poll child container %d: %w", i+1, err)
			}
			p.logger.Printf("Child container %d/%d ready: %s", i+1, len(m.Images), childID)
		}
	} else {
		p.logger.Printf("[DRY RUN] Would poll %d child containers", len(m.Images))
	}

	// Create carousel container (skip if exists)
	carouselID := m.Publishing.ContainerIDs.Carousel
	if carouselID == "" {
		if p.dryRun {
			p.logger.Printf("[DRY RUN] Would create carousel container: children=%d caption_len=%d location=%s",
				len(m.Images), len(caption), locationID)
			p.logger.Printf("[DRY RUN] Would poll carousel container")
			p.logger.Printf("[DRY RUN] Would publish carousel")
			p.logger.Printf("[DRY RUN] Would fetch permalink")
			return nil
		}

		var err error
		carouselID, err = p.ig.CreateCarouselContainer(m.Publishing.ContainerIDs.Children, caption, locationID)
		if err != nil {
			return fmt.Errorf("create carousel container: %w", err)
		}
		m.Publishing.ContainerIDs.Carousel = carouselID
		if err := m.Write(manifestPath); err != nil {
			return fmt.Errorf("write manifest after carousel creation: %w", err)
		}
		p.logger.Printf("Created carousel container: %s", carouselID)
	} else {
		p.logger.Printf("Resuming with existing carousel container: %s", carouselID)
	}

	// Poll carousel
	if err := p.ig.PollContainer(carouselID, pollInterval, maxPolls); err != nil {
		return fmt.Errorf("poll carousel container: %w", err)
	}
	p.logger.Printf("Carousel container ready: %s", carouselID)

	// Publish
	return p.publishContainer(m, manifestPath, carouselID)
}

// publishContainer publishes a container and records the result.
func (p *Publisher) publishContainer(m *manifest.Manifest, manifestPath, containerID string) error {
	mediaID, err := p.ig.Publish(containerID)
	if err != nil {
		return fmt.Errorf("publish container %s: %w", containerID, err)
	}
	m.Publishing.InstagramPostID = mediaID
	p.logger.Printf("Published: media_id=%s", mediaID)

	// Fetch permalink
	permalink, err := p.ig.GetPermalink(mediaID)
	if err != nil {
		p.logger.Printf("Warning: failed to get permalink: %v", err)
	} else {
		m.Publishing.Permalink = permalink
	}

	if err := m.Write(manifestPath); err != nil {
		return fmt.Errorf("write manifest after publish: %w", err)
	}

	return nil
}

// publishStory publishes the hero image as a story.
func (p *Publisher) publishStory(ctx context.Context, m *manifest.Manifest, manifestPath string) error {
	// Find hero image URL
	heroURL := ""
	for i, img := range m.Images {
		if img.IsHero {
			heroURL = m.Publishing.R2URLs[i]
			break
		}
	}
	if heroURL == "" {
		// Fallback to first image
		heroURL = m.Publishing.R2URLs[0]
	}

	if p.dryRun {
		p.logger.Printf("[DRY RUN] Would create story container: %s", heroURL)
		p.logger.Printf("[DRY RUN] Would poll and publish story")
		return nil
	}

	// Create story container
	storyID, err := p.ig.CreateStoryContainer(heroURL)
	if err != nil {
		return fmt.Errorf("create story container: %w", err)
	}
	p.logger.Printf("Created story container: %s", storyID)

	// Poll
	if err := p.ig.PollContainer(storyID, pollInterval, maxPolls); err != nil {
		return fmt.Errorf("poll story container: %w", err)
	}

	// Publish
	mediaID, err := p.ig.Publish(storyID)
	if err != nil {
		return fmt.Errorf("publish story: %w", err)
	}

	m.Publishing.InstagramStoryID = mediaID
	if err := m.Write(manifestPath); err != nil {
		return fmt.Errorf("write manifest after story: %w", err)
	}
	p.logger.Printf("Story published: story_id=%s", mediaID)

	return nil
}

// cleanupR2 deletes all uploaded images from R2.
func (p *Publisher) cleanupR2(ctx context.Context, m *manifest.Manifest, manifestPath string) {
	if m.Publishing.R2Cleaned {
		return
	}

	p.cleanupKeys(ctx, m.Publishing.R2Keys)

	m.Publishing.R2Cleaned = true
	if err := m.Write(manifestPath); err != nil {
		p.logger.Printf("Warning: failed to write manifest after R2 cleanup: %v", err)
	}
}

// cleanupKeys deletes a list of R2 keys (best-effort).
func (p *Publisher) cleanupKeys(ctx context.Context, keys []string) {
	for _, key := range keys {
		if err := p.host.Delete(ctx, key); err != nil {
			p.logger.Printf("Warning: failed to delete R2 key %s: %v", key, err)
		}
	}
}

// setError transitions the manifest to error state and records the error.
func (p *Publisher) setError(m *manifest.Manifest, manifestPath string, publishErr error) {
	m.Errors = append(m.Errors, publishErr.Error())
	if err := m.Transition(manifest.StateError); err != nil {
		p.logger.Printf("Warning: failed to transition to error state: %v", err)
	}
	if err := m.Write(manifestPath); err != nil {
		p.logger.Printf("Warning: failed to write manifest after error: %v", err)
	}
}

func (p *Publisher) syndicate(ctx context.Context, m *manifest.Manifest, manifestPath, caption string) error {
	if !p.enableFacebook && !p.enableThreads {
		return nil
	}

	if m.Publishing.Syndication == nil {
		m.Publishing.Syndication = &manifest.Syndication{}
	}

	var errs []string
	if p.enableFacebook {
		if err := p.syndicateFacebook(ctx, m, caption); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if p.enableThreads {
		if err := p.syndicateThreads(ctx, m, caption); err != nil {
			errs = append(errs, err.Error())
		}
	}

	if err := m.Write(manifestPath); err != nil {
		return fmt.Errorf("write manifest after syndication: %w", err)
	}

	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func (p *Publisher) syndicateFacebook(ctx context.Context, m *manifest.Manifest, caption string) error {
	if m.Publishing.Syndication.Facebook == nil {
		m.Publishing.Syndication.Facebook = &manifest.SyndicationTarget{
			Enabled: true,
			Mode:    "link_share",
		}
	}
	target := m.Publishing.Syndication.Facebook
	target.Enabled = true
	target.Mode = "link_share"

	if target.Status == "published" && target.PostID != "" {
		target.Status = "skipped"
		return nil
	}

	attempted := time.Now().UTC()
	target.AttemptedAt = &attempted

	if p.dryRun {
		target.Status = "dry_run"
		target.Error = ""
		p.logger.Printf("[DRY RUN] Would syndicate to Facebook Page: mode=link_share")
		obslog.Result(
			p.logger,
			"publisher",
			m.ID,
			m.ID,
			"publish_facebook",
			obslog.Intent(
				p.logger,
				"publisher",
				m.ID,
				m.ID,
				"publish_facebook",
				"publish instagram permalink to facebook page",
				map[string]any{"mode": "link_share", "dry_run": true},
			),
			nil,
			map[string]any{"dry_run": true},
		)
		return nil
	}
	stepStart := obslog.Intent(
		p.logger,
		"publisher",
		m.ID,
		m.ID,
		"publish_facebook",
		"publish instagram permalink to facebook page",
		map[string]any{"mode": "link_share"},
	)

	if p.facebook == nil {
		target.Status = "failed"
		target.Error = "facebook syndication enabled but no facebook client configured"
		obslog.Result(p.logger, "publisher", m.ID, m.ID, "publish_facebook", stepStart, fmt.Errorf("%s", target.Error), nil)
		return fmt.Errorf("facebook syndication: %s", target.Error)
	}

	if m.Publishing.Permalink == "" {
		target.Status = "failed"
		target.Error = "instagram permalink missing; cannot create facebook link share"
		obslog.Result(p.logger, "publisher", m.ID, m.ID, "publish_facebook", stepStart, fmt.Errorf("%s", target.Error), nil)
		return fmt.Errorf("facebook syndication: %s", target.Error)
	}

	postID, err := p.facebook.PublishLink(ctx, caption, m.Publishing.Permalink)
	if err != nil {
		target.Status = "failed"
		target.Error = err.Error()
		obslog.Result(p.logger, "publisher", m.ID, m.ID, "publish_facebook", stepStart, err, nil)
		return fmt.Errorf("facebook syndication: %w", err)
	}

	published := time.Now().UTC()
	target.Status = "published"
	target.PostID = postID
	target.PublishedAt = &published
	target.Permalink = ""
	target.Error = ""
	p.logger.Printf("Syndicated to Facebook: post_id=%s", postID)
	obslog.Result(
		p.logger,
		"publisher",
		m.ID,
		m.ID,
		"publish_facebook",
		stepStart,
		nil,
		map[string]any{"status": "published", "post_id": postID},
	)
	return nil
}

func (p *Publisher) syndicateThreads(ctx context.Context, m *manifest.Manifest, caption string) error {
	if m.Publishing.Syndication.Threads == nil {
		m.Publishing.Syndication.Threads = &manifest.SyndicationTarget{
			Enabled: true,
			Mode:    "text_link",
		}
	}
	target := m.Publishing.Syndication.Threads
	target.Enabled = true
	target.Mode = "text_link"

	if target.Status == "published" && target.PostID != "" {
		target.Status = "skipped"
		return nil
	}

	attempted := time.Now().UTC()
	target.AttemptedAt = &attempted

	if p.dryRun {
		target.Status = "dry_run"
		target.Error = ""
		p.logger.Printf("[DRY RUN] Would syndicate to Threads: mode=text_link")
		obslog.Result(
			p.logger,
			"publisher",
			m.ID,
			m.ID,
			"publish_threads",
			obslog.Intent(
				p.logger,
				"publisher",
				m.ID,
				m.ID,
				"publish_threads",
				"publish text+permalink to threads",
				map[string]any{"mode": "text_link", "dry_run": true},
			),
			nil,
			map[string]any{"dry_run": true},
		)
		return nil
	}
	stepStart := obslog.Intent(
		p.logger,
		"publisher",
		m.ID,
		m.ID,
		"publish_threads",
		"publish text+permalink to threads",
		map[string]any{"mode": "text_link"},
	)

	if p.threads == nil {
		target.Status = "failed"
		target.Error = "threads syndication enabled but no threads client configured"
		obslog.Result(p.logger, "publisher", m.ID, m.ID, "publish_threads", stepStart, fmt.Errorf("%s", target.Error), nil)
		return fmt.Errorf("threads syndication: %s", target.Error)
	}

	text := strings.TrimSpace(caption)
	if m.Publishing.Permalink != "" {
		if text != "" {
			text += "\n\n"
		}
		text += m.Publishing.Permalink
	}
	if text == "" {
		target.Status = "failed"
		target.Error = "caption/permalink both empty; cannot publish to threads"
		obslog.Result(p.logger, "publisher", m.ID, m.ID, "publish_threads", stepStart, fmt.Errorf("%s", target.Error), nil)
		return fmt.Errorf("threads syndication: %s", target.Error)
	}

	postID, permalink, err := p.threads.PublishText(ctx, text)
	if err != nil {
		target.Status = "failed"
		target.Error = err.Error()
		obslog.Result(p.logger, "publisher", m.ID, m.ID, "publish_threads", stepStart, err, nil)
		return fmt.Errorf("threads syndication: %w", err)
	}

	published := time.Now().UTC()
	target.Status = "published"
	target.PostID = postID
	target.PublishedAt = &published
	target.Permalink = permalink
	target.Error = ""
	p.logger.Printf("Syndicated to Threads: post_id=%s", postID)
	obslog.Result(
		p.logger,
		"publisher",
		m.ID,
		m.ID,
		"publish_threads",
		stepStart,
		nil,
		map[string]any{"status": "published", "post_id": postID, "permalink": permalink},
	)
	return nil
}
