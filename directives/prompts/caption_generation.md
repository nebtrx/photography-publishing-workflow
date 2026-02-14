# Prompt Template: Caption Generation

## Purpose

Generate a 1–2 sentence Instagram caption for a set of photographs, matching the user's personal writing style. Hashtags are embedded inline within the text, not appended as a block.

## System Prompt

```
You are a caption writer for an Instagram photography account. You write short, evocative captions that capture a feeling, memory, or moment — not descriptions of what's in the image.

STYLE RULES:
- Write 1–2 sentences only. Never more.
- Embed hashtags inline within the sentence as natural parts of the text. Never append hashtags as a separate block.
- Use exactly {hashtag_count} hashtags (maximum 5).
- Hashtags should come from these categories:
  - Location or landmark (e.g., #erasmusbrug, #rotterdam, #vondelpark)
  - Weather or atmosphere (e.g., #misty, #goldenlight, #overcast)
  - Photography style (e.g., #blackandwhite, #liminal, #architecture, #geometry)
  - Camera hardware (e.g., #nikon, #Z8, #fujifilm, #fujigfx100rf)
- The tone is contemplative, understated, and personal — never promotional, excited, or generic.
- Avoid clichés like "captured this moment", "love this view", "what a day".
- Do not describe the image literally. Instead, express what it evokes.

STYLE REFERENCE — here are examples of the user's past captions. Match this voice:

{style_examples}
```

## User Prompt

```
Write an Instagram caption for the following photograph(s).

CONTEXT:
- Camera: {camera_model}
- Lens: {lens}
- Capture date: {capture_date}
- Location hint (from GPS): {gps_location_hint}
- Number of images in this post: {image_count}

HASHTAG BUDGET: Use exactly {hashtag_count} hashtags from these categories:
- Location: {location_hashtag_hint}
- Atmosphere: (derive from the image mood)
- Style: (derive from the visual style)
- Hardware: {hardware_hashtag}

Respond with ONLY the caption text. No explanations, no alternatives, no quotation marks.
```

## Input Variables

| Variable | Source | Example |
|---|---|---|
| `{hashtag_count}` | Calculated: min(5, relevant categories available) | `5` |
| `{style_examples}` | 10–15 random captions from the style corpus | `"Sometimes the city whispers back..."` |
| `{camera_model}` | EXIF from hero image | `"Nikon Z8"` |
| `{lens}` | EXIF from hero image | `"NIKKOR Z 24-70mm f/2.8 S"` |
| `{capture_date}` | EXIF from hero image | `"2026-02-09"` |
| `{gps_location_hint}` | Reverse-geocoded from EXIF GPS, or `"unknown"` | `"Rotterdam, Netherlands"` |
| `{image_count}` | Number of images in the post | `4` |
| `{location_hashtag_hint}` | Derived from GPS location or AI location identification | `"#rotterdam or #erasmusbrug"` |
| `{hardware_hashtag}` | Derived from EXIF camera model | `"#nikon #Z8"` |

## Image Attachment

- Attach the hero image (image with `_1` suffix) to the prompt.
- If the AI provider supports multiple images: attach all images (up to 5) for better context.
- If the provider has image size limits: resize in-memory before sending (do not modify source files).

## Expected Output Format

A single string — the caption text with inline hashtags. No JSON, no markdown, no quotation marks.

**Good output examples:**
```
That quiet moment when the bridge lights first catch the #mist rising off the Maas, and the city feels like it's holding its breath before #sunset fades behind the #erasmusbrug
```

```
There's a geometry to #Rotterdam that only reveals itself in the early hours, when the angles are sharp and the streets belong to no one #architecture #Z8
```

**Bad output examples (should trigger retry):**
```
A beautiful photo of the Erasmus Bridge at sunset.
#erasmusbrug #rotterdam #sunset #architecture #nikon
```
(Hashtags appended as a block, caption is a literal description)

```
Here is your caption: "The light falls differently here."
```
(Includes meta-text and quotation marks)

## Validation Rules

After receiving the AI response:

1. **Length check**: Caption must be ≤2200 characters (Instagram limit). If over, truncate and flag.
2. **Sentence count**: 1–2 sentences. If more, accept but flag `confidence: "low"`.
3. **Hashtag count**: Must have ≤5 hashtags. If more, strip excess from the end.
4. **Hashtag placement**: Hashtags must be inline (part of the sentence flow). If they appear as a separate block at the end, retry once with a corrective prompt: `"The hashtags must be embedded naturally within the sentence, not appended at the end. Try again."`.
5. **No meta-text**: Response must not contain explanatory text like "Here is your caption:" or quotation marks wrapping the caption. Strip if present.

## Style Corpus Injection

- Select 10–15 captions randomly from the style corpus.
- Prefer recent captions (last 6 months) for style freshness, but include some older ones for range.
- If the corpus has fewer than 10 entries, use all of them.
- If the corpus is empty or missing, omit the style examples section from the system prompt. The AI will still generate a caption, just without style matching.

## Notes

- This prompt template is loaded as a text file at runtime. The `{variables}` are replaced by the enricher before sending to the AI provider.
- The template should be editable by the user without recompiling the Go binary.
