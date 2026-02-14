# Directive: Style Corpus Setup

## Goal

Build a style reference corpus by extracting captions from the user's past Instagram posts (~200 posts). This corpus is used by the AI enrichment stage to match the user's personal writing style when generating new captions. This is a one-time setup tool with optional manual refresh.

## Context / Constraints

- The corpus is the foundation for style-matched caption generation. Without it, captions will be generic.
- The Instagram Graph API provides access to a user's own media and captions (with appropriate permissions).
- The Instagram handle is a config parameter (not hardcoded).
- This is a bootstrap/setup tool, not part of the regular pipeline.
- Refresh is manual (`ppw scrape --refresh`), not automated.
- The user has an Instagram Creator account with Graph API access.

## Inputs

- Required:
  - `--handle <instagram-handle>`: Instagram handle (with or without `@` prefix).
  - OR `--user-id <id>`: Instagram user ID (if already known from config).
- Optional:
  - `--count <N>`: Number of posts to scrape (default: 200).
  - `--out <path>`: Output file path (default: `config/style_corpus.json`).
  - `--refresh`: If corpus file exists, overwrite it with fresh data.
  - `--dry-run`: Show what would be fetched without making API calls.
- Environment variables:
  - `INSTAGRAM_ACCESS_TOKEN`: Long-lived access token with `instagram_basic` permission.

## Outputs

- File created: Style corpus JSON file.

### Corpus Format

```json
{
  "handle": "omarsphotography",
  "user_id": "17841405793187218",
  "scraped_at": "2026-02-10T10:00:00Z",
  "post_count": 200,
  "captions": [
    {
      "text": "Sometimes the city whispers back through the fog, and you catch a glimpse of something #liminal between the #architecture and the silence",
      "post_date": "2026-01-15T12:00:00Z",
      "hashtags": ["#liminal", "#architecture"],
      "media_type": "CAROUSEL_ALBUM",
      "permalink": "https://www.instagram.com/p/ABC123/"
    },
    {
      "text": "...",
      "post_date": "...",
      "hashtags": [],
      "media_type": "IMAGE",
      "permalink": "..."
    }
  ],
  "style_summary": {
    "avg_caption_length": 142,
    "avg_hashtag_count": 3.2,
    "common_hashtags": ["#architecture", "#liminal", "#rotterdam", "#nikon"],
    "common_themes": ["atmosphere", "light", "geometry", "solitude"]
  }
}
```

## Steps (high-level)

### 1. Authenticate and Resolve User

1. Validate the access token by calling `GET /me?fields=id,username`.
2. If `--handle` is provided, verify it matches the authenticated account. (The Graph API only allows reading your own media.)
3. Record `user_id` and `handle`.

### 2. Fetch Media

4. Call `GET /{user-id}/media?fields=id,caption,timestamp,media_type,permalink&limit=50`.
5. Paginate using the `next` cursor until either:
   a. `--count` posts have been collected, or
   b. No more pages are available.
6. For each post:
   a. Extract the caption text (may be null if no caption was written).
   b. Parse hashtags from the caption text.
   c. Record the post date, media type, and permalink.
7. Filter out posts with null/empty captions (they contribute nothing to style).

### 3. Compute Style Summary

8. Calculate aggregate statistics:
   - Average caption length (characters).
   - Average hashtag count per post.
   - Most common hashtags (top 20).
   - Common themes (derived from word frequency analysis, excluding common stop words and hashtags).
9. This summary helps the AI understand the user's general patterns without needing to read all 200 captions every time.

### 4. Write Corpus

10. Write the corpus file as formatted JSON.
11. Log: post count, average caption length, top 5 hashtags.

## Edge Cases / Failure Modes

- **Access token lacks permissions**: Exit with error: `"Access token does not have instagram_basic permission. Required for reading media."`.
- **Account has fewer posts than `--count`**: Scrape all available posts. Log: `"Found <N> posts (requested <count>)."`.
- **Posts with no captions**: Filtered out. If all posts have no captions, warn: `"No captions found. Style corpus will be empty."`.
- **API rate limit**: Respect rate limits. Use pagination delays (1 second between pages). On 429: wait and retry.
- **Corpus file already exists (without `--refresh`)**: Exit without overwriting. Message: `"Corpus file already exists at <path>. Use --refresh to overwrite."`.
- **Network failure mid-scrape**: Write whatever was collected so far as a partial corpus. Log: `"Partial corpus written (<N>/<count> posts). Re-run to complete."`. On re-run with `--refresh`, start fresh.

## Acceptance Criteria

- [ ] Given a valid access token and Instagram Creator account with 200+ posts, the tool produces a corpus file with ~200 caption entries.
- [ ] Given an account with fewer than the requested count, the tool scrapes all available posts and logs the actual count.
- [ ] Posts with null/empty captions are excluded from the corpus.
- [ ] The corpus includes a `style_summary` section with average caption length, hashtag frequency, and common themes.
- [ ] Given `--dry-run`, the tool shows the API calls that would be made without executing them.
- [ ] Given an existing corpus file without `--refresh`, the tool exits without overwriting.
- [ ] Given `--refresh`, the tool overwrites the existing corpus with fresh data.
- [ ] Hashtags are correctly extracted from caption text.

## Safety Notes

- This tool reads from the Instagram API. It does not post, modify, or delete anything.
- The access token should only be used with the minimum required permissions (`instagram_basic`).
- The corpus file may contain personal content. It should not be committed to version control (add to `.gitignore`).
- `--dry-run` must be supported.

## Learnings (append-only)

- (None yet)
