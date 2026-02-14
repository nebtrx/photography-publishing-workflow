# Prompt Template: Music Suggestion

## Purpose

Suggest a music track (artist + title) that matches the mood and atmosphere of the photograph(s). This is a nice-to-have feature — the suggested track is displayed during review, but actual music attachment to the Instagram post is manual (Instagram API does not support it).

## System Prompt

```
You are a music curator for a photography Instagram account. Given a photograph, suggest a single music track that matches its mood, atmosphere, and emotional tone.

RULES:
- Suggest exactly one track (artist + title).
- The track should be findable on major streaming platforms (Spotify, Apple Music) and ideally available in Instagram's music library.
- Prefer ambient, instrumental, post-classical, electronic, or atmospheric music. The account focuses on architecture, urban landscapes, and liminal spaces.
- Match the energy: contemplative images get quiet tracks, dramatic images get more dynamic ones.
- Avoid mainstream pop unless the image has unusually energetic or vibrant energy.
- Include a short mood description (2–4 words) explaining the match.
```

## User Prompt

```
Suggest a music track for this photograph.

CONTEXT:
- Capture time: {capture_time_of_day} ({capture_date})
- Location: {location_hint}
- Photography style: {style_hints}

Respond in this exact JSON format:
{
  "artist": "Olafur Arnalds",
  "title": "Near Light",
  "mood": "contemplative, atmospheric",
  "reasoning": "The misty bridge scene at dusk matches the quiet tension in this piece"
}

If you cannot suggest a fitting track, respond:
{
  "artist": null,
  "title": null,
  "mood": null,
  "reasoning": "Unable to determine a fitting track for this image"
}
```

## Input Variables

| Variable | Source | Example |
|---|---|---|
| `{capture_time_of_day}` | Derived from EXIF capture date | `"evening (17:45)"` |
| `{capture_date}` | EXIF capture date | `"2026-02-09"` |
| `{location_hint}` | From enrichment location (if available) or `"unknown"` | `"Erasmusbrug, Rotterdam"` |
| `{style_hints}` | Derived from image analysis or EXIF | `"black and white, long exposure, architecture"` |

## Image Attachment

- Attach the hero image only.

## Expected Output Format

JSON object with fields: `artist`, `title`, `mood`, `reasoning`.

**Good outputs:**
```json
{
  "artist": "Olafur Arnalds",
  "title": "Near Light",
  "mood": "contemplative, atmospheric",
  "reasoning": "The misty bridge scene at dusk matches the quiet tension and spacious ambience of this piece"
}
```

```json
{
  "artist": "Nils Frahm",
  "title": "Says",
  "mood": "expansive, meditative",
  "reasoning": "The geometric patterns and stark contrasts in this architectural shot align with the repetitive, building textures of this composition"
}
```

## Validation Rules

1. **Parse JSON**: Response must be valid JSON. If not, attempt extraction from markdown code blocks. On failure, set music suggestion to null.
2. **Required fields**: `artist` and `title` must both be non-null strings, or both null.
3. **No validation of track existence**: We don't verify the track exists on streaming platforms at this stage. The user can verify during review.

## Notes

- This is the lowest-priority enrichment component. If it fails, the pipeline continues without it.
- Future enhancement: validate the suggestion against Instagram's music library API (if one becomes available) or Spotify's search API.
- This prompt template is loaded as a text file at runtime.
