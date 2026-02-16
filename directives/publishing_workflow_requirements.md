# Publishing Workflow Requirements

## Goals

- Automate the photography publishing pipeline from Lightroom export to Instagram publication.
- Minimize manual effort (clicks, tabs, interactions) in the publishing process.
- Maintain creative control through a review step before publishing.
- Generate AI-powered captions matching the user's personal writing style.
- Reduce the backlog of unpublished edited photos.

## Non-Goals

Strict whitelist: the system ONLY posts. Everything else is out of scope.

- No engagement automation (replying to comments, liking, following).
- No cross-posting to other platforms.
- No image editing or processing (images arrive Instagram-ready from Lightroom).
- No analytics tracking.
- No follower management.
- No actions beyond posting content and experimentally posting stories.

---

## Supported Content Types

### Post (Must-have)

- Single-image or multi-image (carousel) post.
- Carousel: swipeable images in one post. Typical size 4-8 images, but can vary.
- Must respect Instagram's maximum images-per-post limit.
- All images in a post must share the same aspect ratio.

### Story (Experimental / Toggleable)

- One story per post, using the hero image (the image with `_1` suffix).
- Toggleable at three levels: per-post on/off, global always-on, global always-off.
- Published alongside the post when enabled.
- Scope may be refined or removed based on results.

### Not in Scope

- Reels.
- Grid/matrix layout strategies.

---

## Input Assumptions and Sources of Truth

### Image Source

- Exported JPEGs from Adobe Lightroom.
- Instagram-ready: no further processing, resizing, or format conversion required.
- EXIF metadata preserved (GPS, camera model, lens info, capture date).
- Exported to a designated local folder.

### Directory Convention

- One directory = one post.
- All images for a single post reside in the same directory.

### Image Ordering Convention

- Suffix `_1` designates the hero/anchor image (displayed first).
- Suffix `_<N>` (e.g., `_2`, `_3`, `_4`) determines display order.
- Fallback: if numeric suffix pattern is absent, order by EXIF capture date.

### Trigger

- Folder watching: automation monitors the designated folder while the application is running.
- Processing begins when new directories are detected.
- No background daemon; only active while the app is open.

---

## Output Constraints

### Aspect Ratios

- All images in a post must share the same aspect ratio.
- Standardized to Instagram-accepted ratios (1:1, 4:5, 1.91:1).
- If images in a directory have mismatched ratios: flag for user review. Do NOT auto-crop.

### Borders

None. White border workflow is deprecated.

### Compression and Color Profile

- No additional compression or color profile conversion required.
- Images are exported Instagram-ready from Lightroom.
- EXIF metadata is preserved and uploaded as-is (no privacy stripping).

---

## Metadata Requirements

### Caption

- AI-generated, 1-2 sentences.
- Captures an emotional feeling, memory, or moment evoked by the image.
- Must match the user's personal writing style (learned from ~200 scraped past Instagram captions).
- Hashtags embedded inline within the sentence (not appended as a separate block).
- Maximum 5 hashtags per post (user-stated current Instagram limit -- to be verified during implementation; historically the limit was 30).
- Hashtag categories:
  - Location/landmark (e.g., `#erasmusbrug`)
  - Weather/atmosphere (e.g., `#misty`)
  - Photography style (e.g., `#blackandwhite`, `#liminal`, `#architecture`)
  - Camera hardware (e.g., `#nikon`, `#Z8`, `#fujifilm`, `#fujigfx100rf`)

### Location

- Instagram location field (clickable location above the caption).
- Identification pipeline:
  1. AI vision analysis of the image.
  2. Fallback to EXIF GPS data.
  3. If neither yields a location: omit the field.
- Map identified location to Instagram's location database via Instagram API.

### Style Reference

- Scrape ~200 past posts from user's Instagram profile (configured via handle as a config parameter).
- Extract caption text to build a style reference corpus.
- One-time setup with potential periodic refresh.

---

## Publishing Constraints

### Automatable

- Image uploading to Instagram (via Instagram Graph API, Creator account).
- Caption text generation and attachment.
- Location field setting (via Instagram location search API).
- Scheduling posts for future publication.
- Story publishing (hero image only, toggleable).
- Post-publish archival and logging.

### Requires Manual Intervention

- Review and approval of generated content before publishing.
- Resolution of flagged issues (e.g., aspect ratio mismatches).

### Scheduling

- Two modes: publish immediately upon approval OR queue to next available schedule slot.
- Schedule slots: pre-configured based on optimal posting times for:
  - Location: Netherlands.
  - Audience: architecture, urban, geometry, liminal photography enthusiasts.
- Batch size: 2-3 posts per schedule window.
- If queue exceeds batch capacity, overflow goes to the next schedule window.

### Account

- Instagram Creator account, authenticated via Instagram Graph API.

### Cost Constraints

- Prefer local AI processing (Claude Code Pro subscription) over paid API keys.
- Free external APIs are acceptable.
- Instagram's own API usage is acceptable.

---

## Review Workflow

### What Is Reviewed

- Generated caption (with hashtags).
- Image preview (all images in order, as they would appear in the post).

### Review Interface

- Primary preference: TUI (terminal UI) application inspired by LazyGit aesthetics.
  - Panel-based navigation.
  - Minimal keystrokes to approve/reject/edit.
  - Keyboard-driven (not mouse-dependent).
- Alternative: local web page.
- Core UX requirement: minimize clicks, tabs, and interactions.

### Review Actions

- Approve (publish or queue).
- Reject (discard or return to pending).
- Edit caption before approving.

---

## Post-Publish Behavior

- Move source images to an archive directory.
- Maintain a log of published posts with metadata (caption, location, timestamp, image paths, Instagram post ID).

---

## Must-Haves vs Nice-to-Haves

### Must-Haves

- Folder watching for new post directories.
- Numeric suffix ordering with EXIF date fallback.
- AI caption generation matching user's personal style.
- Inline hashtags (max 5, covering: location, weather, style, hardware).
- Instagram location field via AI vision + EXIF fallback.
- Review step with caption + image preview before publishing.
- Immediate publish or schedule-to-queue option.
- Post-publish archival + logging.
- Toggleable story publishing (hero image).
- Aspect ratio mismatch detection and flagging.

### Nice-to-Haves

- LazyGit-style TUI for review (vs simpler web UI).
- Scraping past captions for style matching (vs manual export).

---

## Acceptance Criteria

1. **Image ingestion**: Given a new directory with numbered JPEG images in the watched folder, the system detects it and begins processing within a reasonable time while the app is running.
2. **Image ordering**: Given images with `_1`, `_2`, `_3` suffixes, the system orders them correctly. Given images without numeric suffixes, the system falls back to EXIF capture date ordering.
3. **Aspect ratio validation**: Given a directory where images have different aspect ratios, the system flags the mismatch for user review and does not proceed to publish.
4. **Caption generation**: Given an image set, the system generates a 1-2 sentence caption with <=5 inline hashtags that reads in the user's established style.
5. **Location identification**: Given an image with recognizable landmarks, the system identifies the location and maps it to an Instagram location. Given an image with GPS EXIF, it falls back to that. Given neither, it omits the location field.
6. **Review workflow**: The user can see all images in order and the generated caption, and can approve, reject, or edit the caption before any publishing occurs.
7. **Publishing**: Given an approved post, the system publishes it to Instagram via the Graph API with the correct images, caption, and location.
8. **Scheduling**: Given a "queue" action, the post is placed in the next available schedule slot. The schedule respects configured optimal times and batch limits.
9. **Story publishing**: Given story toggle is enabled, the system publishes the hero image as a story alongside the post. Given toggle is disabled, no story is created.
10. **Archival**: After successful publish, source images are moved to an archive directory and a log entry is created with full metadata.
11. **No prohibited actions**: The system never performs engagement automation, cross-posting, image editing, analytics, or follower management.
