# Directive: AI Enrichment

## Goal

Given a validated manifest, generate two pieces of metadata using AI: a caption matching the user's personal writing style and a location identification for the Instagram location field. The manifest is updated in place with the enrichment results and transitioned to `pending_review`.

## Context / Constraints

- AI provider: Claude CLI subprocess (primary), behind a replaceable provider interface (OpenRouter-compatible for future expansion).
- Cost constraint: prefer local AI processing (Claude Code Pro subscription) over paid API keys.
- The style corpus (scraped past Instagram captions) must be loaded and injected into the caption prompt.
- Location identification uses AI vision first, EXIF GPS as fallback. If neither works, the location field is omitted.
- All AI calls are best-effort — enrichment should degrade gracefully, not block the pipeline.
- This stage reads images from disk to pass to the AI model (vision capabilities required).

## Inputs

- Required:
  - `--manifest <path>`: Path to a validated manifest (`state: "validated"`).
- Optional:
  - `--style-corpus <path>`: Path to the style corpus JSON file. Default: `config/style_corpus.json`.
  - `--skip-location`: Skip location identification.
  - `--dry-run`: Print what AI calls would be made without executing them.
- Environment variables:
  - `CLAUDE_CLI_PATH`: Path to the `claude` CLI binary (default: `claude` on PATH).
  - For future OpenRouter support: `OPENROUTER_API_KEY`.

## Outputs

- Files updated: `manifest.json` (enrichment section added).
- Manifest state transition: `validated` → `pending_review`.

### Manifest Section Written

```json
{
  "enrichment": {
    "caption": {
      "text": "That quiet moment when the bridge lights first catch the #mist rising off the Maas, and the city feels like it's holding its breath before #sunset fades behind the #erasmusbrug",
      "hashtags": ["#mist", "#sunset", "#erasmusbrug", "#nikon", "#Z8"],
      "hashtag_count": 5,
      "generated_at": "2026-02-10T18:32:00Z",
      "model": "claude-cli",
      "confidence": "high"
    },
    "location": {
      "name": "Erasmusbrug, Rotterdam",
      "source": "ai_vision",
      "query_used": "Erasmusbrug Rotterdam",
      "confidence": "high",
      "fallback_used": false
    }
  }
}
```

## Steps (high-level)

### 1. Caption Generation

1. Load the style corpus from the configured path.
2. Read the hero image (and optionally all images) from disk.
3. Assemble the caption generation prompt (see `directives/prompts/caption_generation.md`):
   - System prompt with style guidelines and examples from the corpus.
   - User prompt with: image(s), EXIF context (camera, lens, date, location hint from GPS if available), hashtag category rules.
4. Invoke the AI provider with the assembled prompt and image(s).
5. Parse the response: extract the caption text and identify hashtags within it.
6. Validate: caption is 1–2 sentences, contains ≤5 hashtags, hashtags are inline (not appended).
7. If validation fails, retry once with a corrective prompt. If still invalid, accept the best result and flag in `enrichment.caption.confidence: "low"`.

### 2. Location Identification

1. Send the hero image to the AI provider with the location identification prompt (see `directives/prompts/location_identification.md`).
2. Parse the response for: location name, city/region, and confidence level.
3. If AI returns a location with high confidence: use it as the `location.name` and `location.query_used`.
4. If AI is uncertain or returns no location:
   a. Check EXIF GPS data from the manifest.
   b. If GPS exists: reverse-geocode to a place name (using a free geocoding API or local lookup).
   c. Set `location.source: "exif_gps"`, `location.fallback_used: true`.
5. If neither AI nor EXIF yields a location: set `location` to `null`. This is not an error.
6. Note: mapping `location.name` to an Instagram location ID happens during the publish stage (requires Instagram API), not here.

### 3. Finalize

5. Write all enrichment results to the manifest.
6. Transition manifest state to `pending_review`.
7. Log a summary to stdout: caption preview (first 80 chars), location (or "none").

## Edge Cases / Failure Modes

### AI Provider Failures
- **Claude CLI not found**: Exit with error code 2 and message: `"Claude CLI not found at <path>. Set CLAUDE_CLI_PATH or ensure 'claude' is on PATH."`.
- **Claude CLI timeout**: Set a 60-second timeout per AI call. On timeout, log a warning and skip that enrichment component. Proceed with whatever succeeded.
- **Claude CLI returns unparseable output**: Log the raw output for debugging. Set the affected component's `confidence: "error"`. Proceed with other components.
- **All AI calls fail**: The manifest still transitions to `pending_review` with empty enrichment sections. The user can re-enrich from the review TUI.

### Style Corpus
- **Corpus file not found**: Log a warning. Proceed with caption generation without style examples (the prompt still works, just without style matching).
- **Corpus file empty or malformed**: Same as above — degrade gracefully.

### Image Reading
- **Image too large for AI context**: Resize in-memory before sending (if the AI provider has size limits). Do not modify the original file.
- **Hero image missing**: Use the first image in order. Log a warning.

### Enrichment Quality
- **Caption too long** (>2200 chars, Instagram limit): Truncate and set `confidence: "low"`.
- **More than 5 hashtags**: Strip excess hashtags from the end. Log which were removed.
- **Hashtags not inline** (appended as a block): Attempt to re-prompt once. If still appended, accept as-is and flag for user review.

## Acceptance Criteria

- [ ] Given a validated manifest with images, the tool generates a caption with ≤5 inline hashtags.
- [ ] Given an image of a recognizable landmark, the tool identifies the location.
- [ ] Given an image with GPS EXIF but no recognizable landmark, the tool falls back to GPS-based location.
- [ ] Given an image with no GPS and no recognizable landmark, the tool sets location to null without error.
- [ ] Given a missing style corpus file, the tool proceeds with caption generation (degraded quality) and logs a warning.
- [ ] Given `--dry-run`, the tool prints the prompts that would be sent but makes no AI calls.
- [ ] Given `--skip-location`, only caption generation runs.
- [ ] The manifest transitions from `validated` to `pending_review` after enrichment.
- [ ] All AI call failures are non-blocking: the pipeline continues with whatever enrichment succeeded.
- [ ] Caption hashtag categories include at least one from: location, weather/atmosphere, photography style, or camera hardware (when applicable).

## Safety Notes

- AI calls should have timeouts (60s per call) to prevent indefinite hangs.
- Never send the style corpus or user captions to a third-party API without the user's awareness. The provider interface should make clear which provider is being used.
- `--dry-run` must be supported. In dry-run mode, no AI calls are made and no files are modified.
- Atomic manifest writes (write to temp, then rename).

## Learnings (append-only)

- (None yet)
