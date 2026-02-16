// Package publisher orchestrates the full Instagram publishing flow:
// upload to R2 → resolve location → create containers → poll → publish → story → cleanup.
package publisher

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
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
	pollInterval           = 5 * time.Second
	maxPolls               = 60
	maxCarousel            = 10
	maxInstagramImageBytes = 8 * 1024 * 1024
)

// Publisher orchestrates the publishing flow.
type Publisher struct {
	host              hosting.Host
	ig                InstagramAPI
	logger            *log.Logger
	httpClient        *http.Client
	dryRun            bool
	cleanupOnFailure  bool
	enableFacebook    bool
	enableThreads     bool
	strictSyndication bool
	facebook          FacebookSyndicator
	threads           ThreadsSyndicator
}

// Options configures the publisher.
type Options struct {
	DryRun            bool
	CleanupOnFailure  bool
	EnableFacebook    bool
	EnableThreads     bool
	StrictSyndication bool
	Facebook          FacebookSyndicator
	Threads           ThreadsSyndicator
	LogOutput         io.Writer
	HTTPClient        *http.Client
}

type mediaCandidate struct {
	Index             int
	Filename          string
	LocalPath         string
	URL               string
	LocalContentType  string
	RemoteStatusCode  int
	RemoteContentType string
	Width             int
	Height            int
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
		httpClient:        defaultHTTPClient(opts.HTTPClient),
		dryRun:            opts.DryRun,
		cleanupOnFailure:  opts.CleanupOnFailure,
		enableFacebook:    opts.EnableFacebook,
		enableThreads:     opts.EnableThreads,
		strictSyndication: opts.StrictSyndication,
		facebook:          opts.Facebook,
		threads:           opts.Threads,
	}
}

func defaultHTTPClient(c *http.Client) *http.Client {
	if c != nil {
		return c
	}
	return &http.Client{Timeout: 15 * time.Second}
}

// Publish executes the full publishing flow for a manifest.
// Supports resuming from a partially failed "publishing" state.
func (p *Publisher) Publish(ctx context.Context, m *manifest.Manifest, manifestPath string) error {
	// Retry convenience: allow re-running publish on previously approved posts
	// that failed and ended in error state.
	if m.State == manifest.StateError && m.Review != nil && m.Review.Decision == "approved" {
		stage, err := m.PrepareRetry()
		if err != nil {
			return fmt.Errorf("prepare retry: %w", err)
		}
		if stage == manifest.FailureStagePublish {
			p.resetPublishArtifactsForRetry(m)
		}
		if err := m.Write(manifestPath); err != nil {
			return fmt.Errorf("write manifest before retry: %w", err)
		}
	}

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
	if len(m.Images) > maxCarousel {
		err := fmt.Errorf(
			"instagram carousel supports at most %d images, found %d (split this post or reduce images)",
			maxCarousel,
			len(m.Images),
		)
		p.fail(ctx, m, manifestPath, manifest.FailureStagePublish, err)
		return err
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
		p.fail(ctx, m, manifestPath, manifest.FailureStagePublish, err)
		return fmt.Errorf("upload images: %w", err)
	}

	mediaCandidates, err := p.preflightMedia(ctx, m)
	if err != nil {
		p.fail(ctx, m, manifestPath, manifest.FailureStagePublish, err)
		return fmt.Errorf("media preflight: %w", err)
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
	if err := p.createAndPublish(ctx, m, manifestPath, caption, locationID, mediaCandidates); err != nil {
		obslog.Result(p.logger, "publisher", m.ID, m.ID, "publish_instagram", instagramStart, err, nil)
		p.fail(ctx, m, manifestPath, manifest.FailureStagePublish, err)
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
			p.fail(ctx, m, manifestPath, manifest.FailureStageSyndicate, err)
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

func (p *Publisher) fail(
	ctx context.Context,
	m *manifest.Manifest,
	manifestPath string,
	stage manifest.FailureStage,
	err error,
) {
	if p.cleanupOnFailure && m != nil && m.Publishing != nil && !m.Publishing.R2Cleaned && len(m.Publishing.R2Keys) > 0 {
		p.logger.Printf("cleanup_on_failure=always: deleting %d uploaded R2 objects", len(m.Publishing.R2Keys))
		p.cleanupR2(ctx, m, manifestPath)
	}
	p.setError(m, manifestPath, stage, err)
}

func (p *Publisher) resetPublishArtifactsForRetry(m *manifest.Manifest) {
	if m == nil {
		return
	}
	if m.Publishing == nil {
		m.Publishing = &manifest.Publishing{}
		return
	}
	m.Publishing.ContainerIDs = nil
	m.Publishing.R2Keys = nil
	m.Publishing.R2URLs = nil
	m.Publishing.R2Cleaned = false
	m.Publishing.InstagramPostID = ""
	m.Publishing.Permalink = ""
	m.Publishing.InstagramStoryID = ""
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
		src, err := prepareImageUploadSource(img.Path)
		if err != nil {
			return fmt.Errorf("prepare %s for upload: %w", img.Filename, err)
		}
		cleanup := src.Cleanup
		runCleanup := func() {
			if cleanup != nil {
				cleanup()
				cleanup = nil
			}
		}

		publicURL, err := p.host.Upload(ctx, key, src.Path)
		if err != nil {
			// Retry once
			p.logger.Printf("Upload failed for %s, retrying: %v", img.Filename, err)
			publicURL, err = p.host.Upload(ctx, key, src.Path)
			if err != nil {
				runCleanup()
				// Cleanup already uploaded
				p.cleanupKeys(ctx, keys)
				return fmt.Errorf("upload %s (after retry): %w", img.Filename, err)
			}
		}
		runCleanup()

		keys = append(keys, key)
		urls = append(urls, publicURL)
		if src.Normalized {
			p.logger.Printf(
				"Normalized %s for Instagram upload: %d bytes -> %d bytes",
				img.Filename,
				src.OriginalSize,
				src.UploadSize,
			)
		}
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

type uploadSource struct {
	Path         string
	Cleanup      func()
	Normalized   bool
	OriginalSize int64
	UploadSize   int64
}

func prepareImageUploadSource(path string) (uploadSource, error) {
	info, err := os.Stat(path)
	if err != nil {
		return uploadSource{}, err
	}
	src := uploadSource{
		Path:         path,
		OriginalSize: info.Size(),
		UploadSize:   info.Size(),
	}
	if info.Size() <= maxInstagramImageBytes {
		return src, nil
	}

	ext := strings.ToLower(filepath.Ext(path))
	if ext != ".jpg" && ext != ".jpeg" {
		return uploadSource{}, fmt.Errorf(
			"file exceeds instagram image size limit (%d bytes > %d bytes) and auto-normalization only supports JPEG: %s",
			info.Size(),
			maxInstagramImageBytes,
			path,
		)
	}

	f, err := os.Open(path)
	if err != nil {
		return uploadSource{}, err
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	if err != nil {
		return uploadSource{}, fmt.Errorf("decode jpeg for normalization: %w", err)
	}

	qualities := []int{85, 80, 75, 70, 65, 60, 55, 50, 45}
	bestPath := ""
	bestSize := int64(math.MaxInt64)
	for _, q := range qualities {
		tmp, err := os.CreateTemp("", "ppw-ig-normalized-*.jpg")
		if err != nil {
			return uploadSource{}, fmt.Errorf("create temp file for normalization: %w", err)
		}
		tmpPath := tmp.Name()
		encErr := jpeg.Encode(tmp, img, &jpeg.Options{Quality: q})
		closeErr := tmp.Close()
		if encErr != nil || closeErr != nil {
			_ = os.Remove(tmpPath)
			if encErr != nil {
				continue
			}
			return uploadSource{}, closeErr
		}

		tmpInfo, err := os.Stat(tmpPath)
		if err != nil {
			_ = os.Remove(tmpPath)
			continue
		}
		size := tmpInfo.Size()
		if size < bestSize {
			if bestPath != "" {
				_ = os.Remove(bestPath)
			}
			bestPath = tmpPath
			bestSize = size
		} else {
			_ = os.Remove(tmpPath)
		}

		if size <= maxInstagramImageBytes {
			finalPath := bestPath
			return uploadSource{
				Path:         finalPath,
				Cleanup:      func() { _ = os.Remove(finalPath) },
				Normalized:   true,
				OriginalSize: info.Size(),
				UploadSize:   size,
			}, nil
		}
	}

	if bestPath != "" {
		_ = os.Remove(bestPath)
	}
	return uploadSource{}, fmt.Errorf(
		"file exceeds instagram image size limit (%d bytes > %d bytes) and normalization could not reduce it enough",
		info.Size(),
		maxInstagramImageBytes,
	)
}

func (p *Publisher) preflightMedia(ctx context.Context, m *manifest.Manifest) ([]mediaCandidate, error) {
	if m == nil || m.Publishing == nil {
		return nil, fmt.Errorf("missing publishing context for media preflight")
	}
	if len(m.Images) != len(m.Publishing.R2URLs) {
		return nil, fmt.Errorf("media preflight mismatch: images=%d r2_urls=%d", len(m.Images), len(m.Publishing.R2URLs))
	}

	out := make([]mediaCandidate, 0, len(m.Images))
	for i, img := range m.Images {
		url := m.Publishing.R2URLs[i]
		start := obslog.Intent(
			p.logger,
			"publisher",
			m.ID,
			m.ID,
			"preflight_media",
			"validate media candidate before instagram container creation",
			map[string]any{
				"index":    i + 1,
				"filename": img.Filename,
				"path":     img.Path,
				"url":      url,
			},
		)

		candidate, err := p.inspectMediaCandidate(ctx, i, img.Filename, img.Path, url)
		if err != nil {
			obslog.Result(p.logger, "publisher", m.ID, m.ID, "preflight_media", start, err, nil)
			return nil, err
		}
		obslog.Result(
			p.logger,
			"publisher",
			m.ID,
			m.ID,
			"preflight_media",
			start,
			nil,
			map[string]any{
				"index":                i + 1,
				"filename":             candidate.Filename,
				"local_type":           candidate.LocalContentType,
				"remote_type":          candidate.RemoteContentType,
				"remote_status":        candidate.RemoteStatusCode,
				"width":                candidate.Width,
				"height":               candidate.Height,
				"remote_probe_skipped": p.dryRun || shouldSkipRemoteProbe(url),
			},
		)
		out = append(out, candidate)
	}

	return out, nil
}

func (p *Publisher) inspectMediaCandidate(
	ctx context.Context,
	index int,
	filename, localPath, publicURL string,
) (mediaCandidate, error) {
	c := mediaCandidate{
		Index:     index,
		Filename:  filename,
		LocalPath: localPath,
		URL:       publicURL,
	}

	localType, width, height, err := detectLocalImage(localPath)
	if err != nil {
		return c, fmt.Errorf("media candidate %d %s local check failed (%s): %w", index+1, filename, localPath, err)
	}
	c.LocalContentType = localType
	c.Width = width
	c.Height = height
	if !strings.HasPrefix(localType, "image/") {
		return c, fmt.Errorf("media candidate %d %s local content type is %q, expected image/*", index+1, filename, localType)
	}

	if p.dryRun {
		c.RemoteStatusCode = 0
		c.RemoteContentType = "dry-run"
		return c, nil
	}
	if shouldSkipRemoteProbe(publicURL) {
		c.RemoteStatusCode = 0
		c.RemoteContentType = "probe-skipped"
		return c, nil
	}

	status, remoteType, err := p.probeRemoteMedia(ctx, publicURL)
	c.RemoteStatusCode = status
	c.RemoteContentType = remoteType
	if err != nil {
		p.logger.Printf(
			"Warning: remote probe failed for child %d (%s): %v",
			index+1,
			filename,
			err,
		)
		return c, nil
	}
	if status < 200 || status >= 300 {
		return c, fmt.Errorf("media candidate %d %s remote status=%d (%s)", index+1, filename, status, publicURL)
	}
	if !strings.HasPrefix(strings.ToLower(remoteType), "image/") {
		return c, fmt.Errorf(
			"media candidate %d %s remote content type is %q, expected image/* (%s)",
			index+1,
			filename,
			remoteType,
			publicURL,
		)
	}

	return c, nil
}

func detectLocalImage(path string) (contentType string, width int, height int, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, 0, err
	}
	defer f.Close()

	sniff := make([]byte, 512)
	n, readErr := f.Read(sniff)
	if readErr != nil && readErr != io.EOF {
		return "", 0, 0, readErr
	}
	contentType = http.DetectContentType(sniff[:n])

	if _, err := f.Seek(0, 0); err != nil {
		return "", 0, 0, err
	}
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return "", 0, 0, err
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return "", 0, 0, fmt.Errorf("invalid dimensions %dx%d", cfg.Width, cfg.Height)
	}
	return contentType, cfg.Width, cfg.Height, nil
}

func (p *Publisher) probeRemoteMedia(ctx context.Context, mediaURL string) (int, string, error) {
	headReq, err := http.NewRequestWithContext(ctx, http.MethodHead, mediaURL, nil)
	if err != nil {
		return 0, "", err
	}
	resp, err := p.httpClient.Do(headReq)
	if err == nil {
		_ = resp.Body.Close()
		ct := strings.TrimSpace(resp.Header.Get("Content-Type"))
		if resp.StatusCode >= 200 && resp.StatusCode < 300 && ct != "" {
			return resp.StatusCode, ct, nil
		}
	}

	getReq, err := http.NewRequestWithContext(ctx, http.MethodGet, mediaURL, nil)
	if err != nil {
		return 0, "", err
	}
	getReq.Header.Set("Range", "bytes=0-0")
	resp, err = p.httpClient.Do(getReq)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1))
	ct := strings.TrimSpace(resp.Header.Get("Content-Type"))
	return resp.StatusCode, ct, nil
}

func shouldSkipRemoteProbe(mediaURL string) bool {
	u := strings.ToLower(strings.TrimSpace(mediaURL))
	return strings.HasPrefix(u, "https://test.r2.dev/") ||
		strings.HasPrefix(u, "https://dry-run.r2.dev/")
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
func (p *Publisher) createAndPublish(
	ctx context.Context,
	m *manifest.Manifest,
	manifestPath, caption, locationID string,
	candidates []mediaCandidate,
) error {
	if m.Publishing.ContainerIDs == nil {
		m.Publishing.ContainerIDs = &manifest.ContainerIDs{}
	}

	if len(m.Images) == 1 {
		return p.publishSingle(ctx, m, manifestPath, caption, locationID, candidates)
	}
	return p.publishCarousel(ctx, m, manifestPath, caption, locationID, candidates)
}

// publishSingle handles single-image post publishing.
func (p *Publisher) publishSingle(
	ctx context.Context,
	m *manifest.Manifest,
	manifestPath, caption, locationID string,
	candidates []mediaCandidate,
) error {
	_ = ctx
	imageURL := m.Publishing.R2URLs[0]
	candidate := candidateAt(candidates, 0)

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
			return fmt.Errorf(
				"create single container failed for %s (path=%s url=%s local_type=%s remote_type=%s remote_status=%d): %w",
				candidate.Filename,
				candidate.LocalPath,
				candidate.URL,
				candidate.LocalContentType,
				candidate.RemoteContentType,
				candidate.RemoteStatusCode,
				err,
			)
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
func (p *Publisher) publishCarousel(
	ctx context.Context,
	m *manifest.Manifest,
	manifestPath, caption, locationID string,
	candidates []mediaCandidate,
) error {
	_ = ctx
	// Create child containers (skip already created ones for resume)
	existingChildren := len(m.Publishing.ContainerIDs.Children)
	for i := existingChildren; i < len(m.Images); i++ {
		imageURL := m.Publishing.R2URLs[i]
		candidate := candidateAt(candidates, i)

		if p.dryRun {
			p.logger.Printf("[DRY RUN] Would create child container %d: %s", i+1, imageURL)
			continue
		}

		childID, err := p.ig.CreateChildContainer(imageURL)
		if err != nil {
			errText := fmt.Errorf(
				"create child container %d failed for %s (path=%s url=%s local_type=%s remote_type=%s remote_status=%d): %w",
				i+1,
				candidate.Filename,
				candidate.LocalPath,
				candidate.URL,
				candidate.LocalContentType,
				candidate.RemoteContentType,
				candidate.RemoteStatusCode,
				err,
			)
			if strings.Contains(err.Error(), "code=9004") {
				errText = fmt.Errorf("%w; hint: verify the URL serves image bytes (not HTML/403) and carousel has <=10 images", errText)
			}
			return errText
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

// setError transitions the manifest to error state and records structured failure metadata.
func (p *Publisher) setError(
	m *manifest.Manifest,
	manifestPath string,
	stage manifest.FailureStage,
	publishErr error,
) {
	if m == nil || publishErr == nil {
		return
	}
	m.RecordFailure(stage, publishErr.Error(), manifest.RetryStateForFailureStage(stage))
	if err := m.Write(manifestPath); err != nil {
		p.logger.Printf("Warning: failed to write manifest after error: %v", err)
	}
}

func candidateAt(candidates []mediaCandidate, idx int) mediaCandidate {
	if idx >= 0 && idx < len(candidates) {
		return candidates[idx]
	}
	return mediaCandidate{
		Index:             idx,
		Filename:          fmt.Sprintf("image_%d", idx+1),
		LocalPath:         "",
		URL:               "",
		LocalContentType:  "unknown",
		RemoteStatusCode:  0,
		RemoteContentType: "unknown",
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
