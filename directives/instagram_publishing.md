# Directive: Instagram Publishing

## Goal

Given an approved manifest, upload images to temporary hosting (Cloudflare R2), create the appropriate Instagram media containers via the Graph API, publish the post (and optionally a story), and record the result in the manifest. The post appears on the user's Instagram Creator account exactly as previewed during review.

## Context / Constraints

- Instagram Graph API uses a container-based publishing flow: upload images → create child containers → create carousel container → poll until ready → publish.
- Images must be at publicly accessible URLs. We use Cloudflare R2 as temporary hosting.
- Instagram Creator account, authenticated via long-lived access token.
- Story publishing: hero image only, toggleable via `review.story_enabled`.
- Location: mapped to an Instagram location ID using the Pages Search API.
- Scheduling: TBD — investigate Instagram's `publish_time` parameter (server-side scheduling) before building a client-side scheduler. This directive covers immediate publishing. Scheduled publishing is covered in `directives/scheduling.md`.
- Rate limits: Instagram Graph API has rate limits (varies by app tier). The tool must handle 429 responses gracefully.

## Inputs

- Required:
  - `--manifest <path>`: Path to an approved manifest (`state: "approved"`, `review.decision: "approved"`).
- Optional:
  - `--dry-run`: Log the full API call sequence without executing. No images uploaded, no API calls made.
- Environment variables:
  - `INSTAGRAM_USER_ID`: Instagram Business/Creator account user ID.
  - `INSTAGRAM_ACCESS_TOKEN`: Long-lived access token with `instagram_basic`, `instagram_content_publish`, `pages_read_engagement` permissions.
  - `R2_ACCESS_KEY_ID`: Cloudflare R2 access key.
  - `R2_SECRET_ACCESS_KEY`: Cloudflare R2 secret key.
  - `R2_BUCKET`: R2 bucket name.
  - `R2_ENDPOINT`: R2 S3-compatible endpoint URL.
  - `R2_PUBLIC_URL`: Public URL prefix for uploaded objects.

## Outputs

- Files updated: `manifest.json` (publishing section added).
- Manifest state transition: `approved` → `publishing` → `published`.
- External side effects: post (and optionally story) published to Instagram.

### Manifest Section Written

```json
{
  "publishing": {
    "instagram_post_id": "17895695668004550",
    "instagram_story_id": "17895695668004551",
    "permalink": "https://www.instagram.com/p/ABC123/",
    "published_at": "2026-02-10T18:36:00Z",
    "container_ids": {
      "children": ["17895695668004540", "17895695668004541"],
      "carousel": "17895695668004545"
    },
    "r2_keys": ["posts/erasmusbrug-sunset/img_1.jpg", "posts/erasmusbrug-sunset/img_2.jpg"],
    "r2_cleaned": true
  }
}
```

## Steps (high-level)

### 1. Pre-flight Checks

1. Validate manifest state is `approved`.
2. Validate all required environment variables are set.
3. Verify Instagram access token is valid (call `GET /me?fields=id,username`).
4. If `--dry-run`: log all subsequent steps as "[DRY RUN]" prefixed messages and exit.

### 2. Upload Images to R2

5. For each image in the manifest (in order):
   a. Generate an R2 object key: `posts/<post-id>/<filename>`.
   b. Upload the image to R2 using the S3-compatible API.
   c. Record the public URL: `<R2_PUBLIC_URL>/<key>`.
6. Set manifest state to `publishing`.

### 3. Resolve Instagram Location

7. If `enrichment.location` is not null:
   a. Search for the location using the Facebook Places Search API: `GET /search?type=place&q=<location.query_used>&fields=name,location`.
   b. If a match is found: record `instagram_location_id`.
   c. If no match: log a warning, proceed without location.
8. If `enrichment.location` is null: skip.

### 4. Create Media Containers

**Single image post** (1 image):

9. Create a media container:
   ```
   POST /{ig-user-id}/media
     image_url=<r2_url>
     caption=<review.final_caption>
     location_id=<instagram_location_id>  (if available)
   ```
10. Poll container status until `FINISHED` or error.

**Carousel post** (2–20 images):

9. For each image, create a child container:
   ```
   POST /{ig-user-id}/media
     image_url=<r2_url>
     is_carousel_item=true
   ```
10. Poll each child container until `FINISHED`.
11. Create a carousel container:
    ```
    POST /{ig-user-id}/media
      media_type=CAROUSEL
      children=<comma-separated child IDs>
      caption=<review.final_caption>
      location_id=<instagram_location_id>  (if available)
    ```
12. Poll carousel container until `FINISHED`.

### 5. Publish

13. Publish the container:
    ```
    POST /{ig-user-id}/media_publish
      creation_id=<container_id>
    ```
14. On success: record `instagram_post_id`.
15. Fetch the permalink: `GET /<post_id>?fields=permalink`.

### 6. Story (if enabled)

16. If `review.story_enabled` is true:
    a. Create a story container:
       ```
       POST /{ig-user-id}/media
         media_type=STORIES
         image_url=<hero_image_r2_url>
       ```
    b. Poll until `FINISHED`.
    c. Publish: `POST /{ig-user-id}/media_publish` with `creation_id`.
    d. Record `instagram_story_id`.

### 7. Cleanup

17. Delete all uploaded images from R2.
18. Set `publishing.r2_cleaned: true`.
19. Set manifest state to `published`.
20. Log: post ID, permalink, story ID (if applicable).

## Edge Cases / Failure Modes

### API Errors
- **401 Unauthorized**: Access token expired or invalid. Exit with error, do not retry. Message: `"Instagram access token is invalid or expired. Refresh token and retry."`.
- **429 Rate Limited**: Wait for the `Retry-After` header duration (or 60s default). Retry up to 3 times. If still rate-limited, exit with error and leave manifest in `publishing` state.
- **Container creation fails**: Log the error, set manifest state to `error`, record the error in `manifest.errors[]`. Do not attempt to publish a partial post.
- **Container polling timeout**: Poll every 5 seconds, up to 60 polls (5 minutes). If not `FINISHED`, log error and abort.
- **Publish call fails**: Log error, set state to `error`. Container IDs are recorded so a retry can skip container creation.

### R2 Upload Failures
- **Upload fails**: Retry once. On second failure, abort the entire publish operation. Clean up any already-uploaded images.
- **R2 credentials invalid**: Exit with error code 2 before any API calls.

### Partial Failure Recovery
- **Manifest in `publishing` state on re-run**: The tool checks for existing container IDs. If children are already created, skip to carousel creation. If the carousel exists, skip to publish. This makes the tool resumable.
- **R2 images still present after previous failure**: The tool uploads with overwrite semantics (same keys). Previous images are replaced.

### Location Resolution
- **No Instagram location match**: Proceed without location. Not an error.
- **Multiple location matches**: Use the first result. Log alternatives for debugging.

### Content Limits
- **Caption exceeds 2200 characters**: Truncate at 2200 and log a warning.
- **More than 20 images**: Should have been caught by validation. If somehow reached here, use only the first 20.

## Acceptance Criteria

- [ ] Given an approved single-image manifest, the tool publishes a single-image post to Instagram with the correct caption and location.
- [ ] Given an approved multi-image manifest, the tool publishes a carousel post with images in the correct order.
- [ ] Given `review.story_enabled: true`, the tool publishes the hero image as a story alongside the post.
- [ ] Given `review.story_enabled: false`, no story is created.
- [ ] Given `--dry-run`, the tool logs the full API call sequence (container creation, polling, publish) without executing any calls or uploading any images.
- [ ] After successful publish, all R2-uploaded images are deleted.
- [ ] After successful publish, the manifest contains `instagram_post_id`, `permalink`, and `published_at`.
- [ ] Given an expired access token, the tool exits with a clear error message and does not leave the manifest in an inconsistent state.
- [ ] Given a rate-limited response (429), the tool waits and retries before failing.
- [ ] Given a manifest in `publishing` state (previous partial failure), the tool resumes from where it left off (skips already-created containers).

## Safety Notes

- `--dry-run` is critical for this stage. It must simulate the full flow with realistic log output.
- Never log the full access token. Log only the last 4 characters for identification.
- R2 objects should have a TTL or be cleaned up explicitly. Do not leave images in R2 indefinitely.
- The manifest is written atomically at each sub-step (after R2 upload, after container creation, after publish, after cleanup) so that partial failures are recoverable.
- Instagram API calls are irreversible once `media_publish` is called. The dry-run and review stages are the safety nets.

## Learnings (append-only)

- (None yet)
