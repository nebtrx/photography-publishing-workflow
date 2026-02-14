# Prompt Template: Location Identification

## Purpose

Identify the location depicted in a photograph using AI vision. The result is used to populate the Instagram location field (the clickable location above the caption). This is the primary identification method; EXIF GPS is the fallback.

## System Prompt

```
You are a location identification assistant for a photography workflow. Given a photograph, identify the specific location depicted — the place, landmark, or area the camera is pointing at.

RULES:
- Identify the most specific recognizable location (landmark > neighborhood > city > region).
- If you recognize a specific landmark, building, bridge, park, or street: name it.
- If you can identify the city but not the specific place: name the city.
- If you cannot identify the location with reasonable confidence: say "unknown".
- Do not guess. If you're uncertain, say "unknown".
- Focus on what the camera is pointing AT, not where the photographer is standing (though they're often the same).
```

## User Prompt

```
Identify the location in this photograph.

CONTEXT (use as hints, not as answers):
- GPS coordinates from EXIF (if available): {gps_coordinates}
- City hint from GPS (if available): {gps_city_hint}
- Capture date: {capture_date}

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

If confidence is "none", respond:
{
  "location_name": null,
  "city": null,
  "country": null,
  "confidence": "none",
  "reasoning": "No recognizable landmarks or location indicators"
}
```

## Input Variables

| Variable | Source | Example |
|---|---|---|
| `{gps_coordinates}` | EXIF GPS from manifest, or `"not available"` | `"51.9094, 4.4864"` |
| `{gps_city_hint}` | Reverse-geocoded from GPS, or `"not available"` | `"Rotterdam, Netherlands"` |
| `{capture_date}` | EXIF capture date from manifest | `"2026-02-09"` |

## Image Attachment

- Attach the hero image only.
- The hero image typically shows the primary subject/location of the post.

## Expected Output Format

JSON object with fields: `location_name`, `city`, `country`, `confidence`, `reasoning`.

**Good outputs:**
```json
{
  "location_name": "Erasmusbrug, Rotterdam",
  "city": "Rotterdam",
  "country": "Netherlands",
  "confidence": "high",
  "reasoning": "The distinctive asymmetric pylon of the Erasmus Bridge is clearly visible spanning the Nieuwe Maas river"
}
```

```json
{
  "location_name": "Vondelpark",
  "city": "Amsterdam",
  "country": "Netherlands",
  "confidence": "medium",
  "reasoning": "Park setting with characteristic Dutch landscape design, possibly Vondelpark based on the bridge style"
}
```

```json
{
  "location_name": null,
  "city": null,
  "country": null,
  "confidence": "none",
  "reasoning": "Abstract architectural detail with no identifiable location markers"
}
```

## Validation Rules

1. **Parse JSON**: The response must be valid JSON. If not, attempt to extract JSON from the response (the AI may wrap it in markdown code blocks). If still unparseable, set confidence to `"error"`.
2. **Confidence threshold**: Only use the location if confidence is `"high"` or `"medium"`. For `"low"` or `"none"`, fall back to EXIF GPS.
3. **Location name**: Must be a real, searchable place name (not a description like "a bridge over a river").

## Fallback Pipeline

The enricher uses this result as follows:

1. If AI returns confidence `"high"` or `"medium"`: use `location_name` as `enrichment.location.name`, set `source: "ai_vision"`.
2. If AI returns `"low"` or `"none"`:
   a. Check EXIF GPS from the manifest.
   b. If GPS exists: reverse-geocode to a place name, set `source: "exif_gps"`, `fallback_used: true`.
   c. If no GPS: set `enrichment.location` to `null`. Not an error.
3. The `location.query_used` field (for Instagram location search during publishing) is set to the `location_name` value.

## Notes

- The GPS hint is provided as context to help the AI narrow its analysis, but the AI should primarily rely on visual recognition. GPS coordinates can be imprecise or refer to the photographer's position rather than the subject.
- This prompt template is loaded as a text file at runtime.
