// Package enricher orchestrates AI-powered metadata generation for posts:
// caption generation, location identification, and music suggestion.
package enricher

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"strings"
	"time"

	"photography-publishing-workflow/internal/ai"
	"photography-publishing-workflow/internal/manifest"
	"photography-publishing-workflow/internal/obslog"
)

// Options controls which enrichment components to run.
type Options struct {
	SkipLocation bool
	SkipMusic    bool
	CorpusPath   string
	LogOutput    io.Writer
}

// Enricher orchestrates AI enrichment of a manifest.
type Enricher struct {
	provider ai.Provider
	opts     Options
	logger   *log.Logger
}

// New creates an Enricher with the given AI provider and options.
func New(provider ai.Provider, opts Options) *Enricher {
	logOutput := opts.LogOutput
	if logOutput == nil {
		logOutput = os.Stderr
	}
	return &Enricher{
		provider: provider,
		opts:     opts,
		logger:   log.New(logOutput, "", log.LstdFlags),
	}
}

// Enrich runs all enrichment components on a validated manifest.
// It degrades gracefully — individual component failures don't block others.
func (e *Enricher) Enrich(ctx context.Context, m *manifest.Manifest) error {
	if m.State != manifest.StateValidated {
		return fmt.Errorf("enricher: expected state %q, got %q", manifest.StateValidated, m.State)
	}

	hero := heroImage(m)
	if hero == nil {
		return fmt.Errorf("enricher: no hero image found in manifest")
	}

	enrichment := &manifest.Enrichment{}

	// 1. Caption generation (always runs)
	captionStep := obslog.Intent(
		e.logger,
		"enricher",
		m.ID,
		m.ID,
		"extract_caption",
		"extract description/caption from hero image with AI",
		map[string]any{"image_count": len(m.Images), "provider": e.providerName()},
	)
	caption, err := e.generateCaption(ctx, m, hero)
	if err != nil {
		obslog.Result(e.logger, "enricher", m.ID, m.ID, "extract_caption", captionStep, err, nil)
		e.logger.Printf("WARN: caption generation failed: %v", err)
	} else {
		enrichment.Caption = caption
		obslog.Result(
			e.logger,
			"enricher",
			m.ID,
			m.ID,
			"extract_caption",
			captionStep,
			nil,
			map[string]any{
				"caption_preview": previewText(caption.Text, 160),
				"hashtag_count":   caption.HashtagCount,
			},
		)
	}

	// 2. Location identification (unless skipped)
	if !e.opts.SkipLocation {
		locationStep := obslog.Intent(
			e.logger,
			"enricher",
			m.ID,
			m.ID,
			"extract_location",
			"identify location from hero image with AI/GPS fallback",
			map[string]any{"provider": e.providerName()},
		)
		loc, err := e.identifyLocation(ctx, m, hero)
		if err != nil {
			obslog.Result(e.logger, "enricher", m.ID, m.ID, "extract_location", locationStep, err, nil)
			e.logger.Printf("WARN: location identification failed: %v", err)
		} else {
			enrichment.Location = loc
			details := map[string]any{"location_found": loc != nil}
			if loc != nil {
				details["location_name"] = loc.Name
				details["source"] = loc.Source
				details["confidence"] = loc.Confidence
			}
			obslog.Result(
				e.logger,
				"enricher",
				m.ID,
				m.ID,
				"extract_location",
				locationStep,
				nil,
				details,
			)
		}
	}

	// 3. Music suggestion (unless skipped)
	if !e.opts.SkipMusic {
		musicStep := obslog.Intent(
			e.logger,
			"enricher",
			m.ID,
			m.ID,
			"extract_music",
			"suggest matching music based on hero image and context",
			map[string]any{"provider": e.providerName()},
		)
		music, err := e.suggestMusic(ctx, m, hero, enrichment.Location)
		if err != nil {
			obslog.Result(e.logger, "enricher", m.ID, m.ID, "extract_music", musicStep, err, nil)
			e.logger.Printf("WARN: music suggestion failed: %v", err)
		} else {
			enrichment.MusicSuggestion = music
			obslog.Result(
				e.logger,
				"enricher",
				m.ID,
				m.ID,
				"extract_music",
				musicStep,
				nil,
				map[string]any{"artist": music.Artist, "title": music.Title, "mood": music.Mood},
			)
		}
	}

	m.Enrichment = enrichment
	return m.Transition(manifest.StatePendingReview)
}

func heroImage(m *manifest.Manifest) *manifest.Image {
	for i := range m.Images {
		if m.Images[i].IsHero {
			return &m.Images[i]
		}
	}
	if len(m.Images) > 0 {
		return &m.Images[0]
	}
	return nil
}

func (e *Enricher) providerName() string {
	if e.provider == nil {
		return ""
	}
	return e.provider.Name()
}

func previewText(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	if max < 4 {
		max = 4
	}
	return s[:max-3] + "..."
}

// --- Caption Generation ---

func (e *Enricher) generateCaption(ctx context.Context, m *manifest.Manifest, hero *manifest.Image) (*manifest.Caption, error) {
	corpus, err := LoadCorpus(e.opts.CorpusPath)
	if err != nil {
		e.logger.Printf("WARN: could not load style corpus: %v", err)
	}

	systemPrompt := buildCaptionSystemPrompt(corpus)
	userPrompt := buildCaptionUserPrompt(m, hero)

	// Attach hero image + up to 4 more
	imagePaths := collectImagePaths(m, 5)

	resp, err := e.provider.Generate(ctx, ai.Request{
		SystemPrompt: systemPrompt,
		UserPrompt:   userPrompt,
		ImagePaths:   imagePaths,
	})
	if err != nil {
		return nil, fmt.Errorf("AI caption call: %w", err)
	}

	caption := parseCaption(resp.Text)
	caption.GeneratedAt = time.Now().UTC()
	caption.Model = resp.Model

	// Validate and fix
	caption = validateCaption(caption)

	return caption, nil
}

func buildCaptionSystemPrompt(corpus *StyleCorpus) string {
	styleExamples := "(No style corpus available — generate in a contemplative, understated voice)"
	if samples := SampleCaptions(corpus, 12); len(samples) > 0 {
		var lines []string
		for _, s := range samples {
			lines = append(lines, fmt.Sprintf("- %s", s))
		}
		styleExamples = strings.Join(lines, "\n")
	}

	return fmt.Sprintf(`You are a caption writer for an Instagram photography account. You write short, evocative captions that capture a feeling, memory, or moment — not descriptions of what's in the image.

STYLE RULES:
- Write 1–2 sentences only. Never more.
- Embed hashtags inline within the sentence as natural parts of the text. Never append hashtags as a separate block.
- Use at most 5 hashtags.
- Hashtags should come from these categories:
  - Location or landmark (e.g., #erasmusbrug, #rotterdam, #vondelpark)
  - Weather or atmosphere (e.g., #misty, #goldenlight, #overcast)
  - Photography style (e.g., #blackandwhite, #liminal, #architecture, #geometry)
  - Camera hardware (e.g., #nikon, #Z8, #fujifilm, #fujigfx100rf)
- The tone is contemplative, understated, and personal — never promotional, excited, or generic.
- Avoid clichés like "captured this moment", "love this view", "what a day".
- Do not describe the image literally. Instead, express what it evokes.

STYLE REFERENCE — here are examples of the user's past captions. Match this voice:

%s`, styleExamples)
}

func buildCaptionUserPrompt(m *manifest.Manifest, hero *manifest.Image) string {
	camera := valueOr(hero.EXIF.Camera, "unknown")
	lens := valueOr(hero.EXIF.Lens, "unknown")
	captureDate := "unknown"
	if hero.EXIF.CaptureDate != nil {
		captureDate = hero.EXIF.CaptureDate.Format("2006-01-02")
	}
	gpsHint := "unknown"
	if hero.EXIF.GPS != nil {
		gpsHint = fmt.Sprintf("%.4f, %.4f", hero.EXIF.GPS.Latitude, hero.EXIF.GPS.Longitude)
	}

	hardwareTag := deriveHardwareHashtag(hero.EXIF.Camera)

	return fmt.Sprintf(`Write an Instagram caption for the following photograph(s).

CONTEXT:
- Camera: %s
- Lens: %s
- Capture date: %s
- Location hint (from GPS): %s
- Number of images in this post: %d

HASHTAG BUDGET: Use at most 5 hashtags from these categories:
- Location: (derive from the image)
- Atmosphere: (derive from the image mood)
- Style: (derive from the visual style)
- Hardware: %s

Respond with ONLY the caption text. No explanations, no alternatives, no quotation marks.`,
		camera, lens, captureDate, gpsHint, len(m.Images), hardwareTag)
}

func deriveHardwareHashtag(camera string) string {
	lower := strings.ToLower(camera)
	switch {
	case strings.Contains(lower, "nikon"):
		// Extract model like Z8, Z6, Z7
		if strings.Contains(lower, "z8") || strings.Contains(lower, "z 8") {
			return "#nikon #Z8"
		}
		return "#nikon"
	case strings.Contains(lower, "fuji"):
		if strings.Contains(lower, "gfx") {
			return "#fujifilm #fujigfx100rf"
		}
		return "#fujifilm"
	case strings.Contains(lower, "sony"):
		return "#sony"
	case strings.Contains(lower, "canon"):
		return "#canon"
	default:
		return "(derive from camera hardware)"
	}
}

var hashtagRe = regexp.MustCompile(`#\w+`)

func parseCaption(text string) *manifest.Caption {
	// Clean up common AI artifacts
	text = strings.TrimSpace(text)
	text = strings.Trim(text, "\"")
	// Remove "Here is your caption:" or similar preamble
	if idx := strings.Index(text, "\n\n"); idx > 0 && idx < 50 {
		text = strings.TrimSpace(text[idx+2:])
	}

	hashtags := hashtagRe.FindAllString(text, -1)

	return &manifest.Caption{
		Text:         text,
		Hashtags:     hashtags,
		HashtagCount: len(hashtags),
		Confidence:   "high",
	}
}

func validateCaption(c *manifest.Caption) *manifest.Caption {
	// Enforce max 5 hashtags
	if c.HashtagCount > 5 {
		// Remove excess hashtags from the text (from the end)
		kept := 0
		c.Text = hashtagRe.ReplaceAllStringFunc(c.Text, func(tag string) string {
			kept++
			if kept > 5 {
				return strings.TrimPrefix(tag, "#") // remove the # to de-hashtag it
			}
			return tag
		})
		c.Hashtags = c.Hashtags[:5]
		c.HashtagCount = 5
		c.Confidence = "medium"
	}

	// Enforce max 2200 chars
	if len(c.Text) > 2200 {
		c.Text = c.Text[:2200]
		c.Confidence = "low"
	}

	return c
}

// --- Location Identification ---

// locationResponse matches the expected JSON from the AI.
type locationResponse struct {
	LocationName string `json:"location_name"`
	City         string `json:"city"`
	Country      string `json:"country"`
	Confidence   string `json:"confidence"`
	Reasoning    string `json:"reasoning"`
}

func (e *Enricher) identifyLocation(ctx context.Context, m *manifest.Manifest, hero *manifest.Image) (*manifest.Location, error) {
	gpsCoords := "not available"
	gpsCity := "not available"
	if hero.EXIF.GPS != nil {
		gpsCoords = fmt.Sprintf("%.4f, %.4f", hero.EXIF.GPS.Latitude, hero.EXIF.GPS.Longitude)
		// Basic city hint — in production this would use reverse geocoding
		gpsCity = fmt.Sprintf("near coordinates %s", gpsCoords)
	}

	captureDate := "unknown"
	if hero.EXIF.CaptureDate != nil {
		captureDate = hero.EXIF.CaptureDate.Format("2006-01-02")
	}

	systemPrompt := `You are a location identification assistant for a photography workflow. Given a photograph, identify the specific location depicted — the place, landmark, or area the camera is pointing at.

RULES:
- Identify the most specific recognizable location (landmark > neighborhood > city > region).
- If you recognize a specific landmark, building, bridge, park, or street: name it.
- If you can identify the city but not the specific place: name the city.
- If you cannot identify the location with reasonable confidence: say "unknown".
- Do not guess. If you're uncertain, say "unknown".
- Focus on what the camera is pointing AT, not where the photographer is standing (though they're often the same).`

	userPrompt := fmt.Sprintf(`Identify the location in this photograph.

CONTEXT (use as hints, not as answers):
- GPS coordinates from EXIF (if available): %s
- City hint from GPS (if available): %s
- Capture date: %s

Respond in this exact JSON format:
{
  "location_name": "Erasmusbrug, Rotterdam",
  "city": "Rotterdam",
  "country": "Netherlands",
  "confidence": "high",
  "reasoning": "The distinctive asymmetric pylon of the Erasmus Bridge is clearly visible"
}

Confidence levels:
- "high": Specific landmark or place clearly recognizable
- "medium": City or area identifiable but specific place uncertain
- "low": Best guess, not confident
- "none": Cannot identify the location

If confidence is "none", respond with location_name, city, and country as null.`, gpsCoords, gpsCity, captureDate)

	resp, err := e.provider.Generate(ctx, ai.Request{
		SystemPrompt: systemPrompt,
		UserPrompt:   userPrompt,
		ImagePaths:   []string{hero.Path},
	})
	if err != nil {
		return nil, fmt.Errorf("AI location call: %w", err)
	}

	loc, err := parseLocationResponse(resp.Text)
	if err != nil {
		return nil, fmt.Errorf("parse location response: %w", err)
	}

	// Only accept high/medium confidence from AI vision
	if loc.Confidence == "high" || loc.Confidence == "medium" {
		return &manifest.Location{
			Name:         loc.LocationName,
			Source:       "ai_vision",
			QueryUsed:    loc.LocationName,
			Confidence:   loc.Confidence,
			FallbackUsed: false,
		}, nil
	}

	// Fall back to EXIF GPS
	if hero.EXIF.GPS != nil {
		return &manifest.Location{
			Name:         gpsCity,
			Source:       "exif_gps",
			QueryUsed:    gpsCity,
			Confidence:   "medium",
			FallbackUsed: true,
		}, nil
	}

	// Neither AI nor GPS
	return nil, nil
}

func parseLocationResponse(text string) (*locationResponse, error) {
	text = strings.TrimSpace(text)

	// Try to extract JSON from possible markdown code blocks
	text = extractJSON(text)

	var loc locationResponse
	if err := json.Unmarshal([]byte(text), &loc); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w\nraw response: %s", err, text)
	}
	return &loc, nil
}

// --- Music Suggestion ---

type musicResponse struct {
	Artist    string `json:"artist"`
	Title     string `json:"title"`
	Mood      string `json:"mood"`
	Reasoning string `json:"reasoning"`
}

func (e *Enricher) suggestMusic(ctx context.Context, m *manifest.Manifest, hero *manifest.Image, location *manifest.Location) (*manifest.MusicSuggestion, error) {
	captureTime := "unknown"
	captureDate := "unknown"
	if hero.EXIF.CaptureDate != nil {
		captureTime = hero.EXIF.CaptureDate.Format("15:04")
		captureDate = hero.EXIF.CaptureDate.Format("2006-01-02")
		hour := hero.EXIF.CaptureDate.Hour()
		switch {
		case hour < 6:
			captureTime = fmt.Sprintf("night (%s)", captureTime)
		case hour < 12:
			captureTime = fmt.Sprintf("morning (%s)", captureTime)
		case hour < 17:
			captureTime = fmt.Sprintf("afternoon (%s)", captureTime)
		case hour < 21:
			captureTime = fmt.Sprintf("evening (%s)", captureTime)
		default:
			captureTime = fmt.Sprintf("night (%s)", captureTime)
		}
	}

	locationHint := "unknown"
	if location != nil {
		locationHint = location.Name
	}

	systemPrompt := `You are a music curator for a photography Instagram account. Given a photograph, suggest a single music track that matches its mood, atmosphere, and emotional tone.

RULES:
- Suggest exactly one track (artist + title).
- The track should be findable on major streaming platforms (Spotify, Apple Music) and ideally available in Instagram's music library.
- Prefer ambient, instrumental, post-classical, electronic, or atmospheric music. The account focuses on architecture, urban landscapes, and liminal spaces.
- Match the energy: contemplative images get quiet tracks, dramatic images get more dynamic ones.
- Avoid mainstream pop unless the image has unusually energetic or vibrant energy.
- Include a short mood description (2–4 words) explaining the match.`

	userPrompt := fmt.Sprintf(`Suggest a music track for this photograph.

CONTEXT:
- Capture time: %s (%s)
- Location: %s
- Photography style: architecture, urban, liminal

Respond in this exact JSON format:
{
  "artist": "Olafur Arnalds",
  "title": "Near Light",
  "mood": "contemplative, atmospheric",
  "reasoning": "The misty bridge scene at dusk matches the quiet tension in this piece"
}

If you cannot suggest a fitting track, set artist, title, and mood to null.`, captureTime, captureDate, locationHint)

	resp, err := e.provider.Generate(ctx, ai.Request{
		SystemPrompt: systemPrompt,
		UserPrompt:   userPrompt,
		ImagePaths:   []string{hero.Path},
	})
	if err != nil {
		return nil, fmt.Errorf("AI music call: %w", err)
	}

	music, err := parseMusicResponse(resp.Text)
	if err != nil {
		return nil, fmt.Errorf("parse music response: %w", err)
	}

	if music.Artist == "" || music.Title == "" {
		return nil, nil
	}

	return &manifest.MusicSuggestion{
		Artist:      music.Artist,
		Title:       music.Title,
		Mood:        music.Mood,
		GeneratedAt: time.Now().UTC(),
	}, nil
}

func parseMusicResponse(text string) (*musicResponse, error) {
	text = strings.TrimSpace(text)
	text = extractJSON(text)

	var music musicResponse
	if err := json.Unmarshal([]byte(text), &music); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w\nraw response: %s", err, text)
	}
	return &music, nil
}

// --- Helpers ---

func collectImagePaths(m *manifest.Manifest, max int) []string {
	var paths []string
	for _, img := range m.Images {
		if len(paths) >= max {
			break
		}
		paths = append(paths, img.Path)
	}
	return paths
}

func valueOr(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// extractJSON tries to pull a JSON object from text that may be wrapped
// in markdown code blocks or have preamble text.
func extractJSON(text string) string {
	// Remove markdown code fences
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	text = strings.TrimSpace(text)

	// Try to find JSON object boundaries
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start >= 0 && end > start {
		return text[start : end+1]
	}
	return text
}
