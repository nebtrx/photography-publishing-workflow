# Directive: Cross-Platform Syndication (Facebook Page + Threads)

## Goal

After a successful Instagram publish, optionally syndicate the same post to a connected Facebook Page and/or Threads account. Instagram remains the primary publish target. Syndication is explicit, opt-in, and non-blocking per destination.

## Context / Constraints

- Current implementation publishes to Instagram only.
- Instagram Graph API publish flow does not guarantee automatic cross-post behavior to Facebook Page or Threads.
- This directive intentionally extends a previously stated non-goal (`no cross-posting`) and should be treated as an explicit, opt-in product change.
- Destination failures must not silently lose data; each destination status is recorded.
- Existing safety model remains:
  - human review before publish
  - dry-run support for mutating flows
  - atomic manifest writes

## Inputs

- Required:
  - `--manifest <path>` in `approved` state for initial publish.
- Optional:
  - `--destinations instagram,facebook,threads` (default: `instagram`)
  - `--dry-run`
  - `--strict-syndication` (if set, destination failure marks run as failed)
- Environment variables:
  - Instagram (existing): `INSTAGRAM_USER_ID`, `INSTAGRAM_ACCESS_TOKEN`
  - Meta/Page (existing managed auth): `META_APP_ID`, `META_APP_SECRET`, `META_PAGE_ID`
  - Threads (new): `THREADS_USER_ID`, `THREADS_ACCESS_TOKEN`
- Config (suggested):
  - `syndication.facebook.enabled`
  - `syndication.threads.enabled`
  - `syndication.default_destinations`

## Outputs

- Files updated:
  - `manifest.json`
- Manifest state:
  - Primary flow unchanged: `approved` -> `publishing` -> `published`
  - Destination outcomes recorded independently.

### Manifest Section Written (example)

```json
{
  "publishing": {
    "instagram_post_id": "17900000000000001",
    "permalink": "https://www.instagram.com/p/ABC123/",
    "syndication": {
      "facebook": {
        "enabled": true,
        "status": "published",
        "post_id": "987405421123281_123456789012345",
        "published_at": "2026-02-14T16:20:00Z",
        "mode": "link_share"
      },
      "threads": {
        "enabled": true,
        "status": "failed",
        "error": "threads publish failed: rate limited",
        "attempted_at": "2026-02-14T16:20:05Z"
      }
    }
  }
}
```

## Steps (high-level)

1. Publish to Instagram as primary destination (existing behavior).
2. If Instagram publish succeeds, execute selected syndication destinations:
   - Facebook Page (optional)
   - Threads (optional)
3. Record per-destination results in manifest.
4. Continue archival flow after primary publish; destination failures are:
   - warnings by default
   - fatal only with `--strict-syndication`

### Facebook Page Syndication

Recommended v1 mode: `link_share`

1. Create a Page post on `/{META_PAGE_ID}/feed` with:
   - `message` = final caption (or shortened variant)
   - `link` = Instagram permalink
2. Record returned Page post ID in manifest.

Optional v2 mode: `native_media`

1. For single-image posts, publish photo directly via `/{META_PAGE_ID}/photos`.
2. For carousels, either:
   - create album/photo sequence if supported in target API version, or
   - fall back to `link_share`.

### Threads Syndication

1. Create Threads media container on `/{THREADS_USER_ID}/threads`.
2. Publish via `/{THREADS_USER_ID}/threads_publish`.
3. For unsupported media cases, fall back to text post:
   - caption + Instagram permalink.
4. Record Threads post/container IDs and permalink (if available).

## Edge Cases / Failure Modes

- **Instagram succeeds, destination fails**: primary publish remains `published`; destination marked failed with error details.
- **Destination token expired**: retry once after token refresh if available; otherwise mark destination failed.
- **Rate limit (429)**: exponential backoff; cap retries; record final error.
- **Duplicate syndication attempts on rerun**: detect existing destination IDs and skip (idempotent resume behavior).
- **Caption too long for destination**: truncate per platform limits with warning.
- **Threads media constraints mismatch**: fallback to text+link post.
- **Facebook Page inaccessible**: mark facebook destination failed; do not roll back Instagram post.

## Acceptance Criteria

- [ ] Given destination set to `instagram` only, behavior is unchanged from current publish flow.
- [ ] Given destination includes `facebook`, a Page post is created and ID is saved in manifest.
- [ ] Given destination includes `threads`, a Threads post is created and ID is saved in manifest.
- [ ] Given one destination fails, Instagram post remains published and failure is recorded.
- [ ] Given `--strict-syndication`, any destination failure marks run as failed.
- [ ] Given rerun after partial success, already-published destinations are skipped (idempotent).
- [ ] Given `--dry-run`, no network mutations occur; planned calls are logged.

## Safety Notes

- Cross-platform publishing is irreversible per destination once publish call succeeds.
- Keep destination toggles explicit and opt-in.
- Never log full tokens or app secrets.
- Preserve primary publishing correctness: destination extension must not break Instagram core flow.

## Learnings (append-only)

- API-level publishing should not assume product-level cross-post settings from consumer apps.
