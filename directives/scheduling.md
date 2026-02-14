# Directive: Scheduling

## Goal

Manage a posting schedule queue so that approved posts can be published at optimal times rather than immediately. Posts approved with `publish_mode: "queued"` are assigned to the next available schedule slot. The schedule respects configured time windows, batch limits, and timezone.

## Context / Constraints

- Two publish modes: `immediate` (handled by the publish stage directly) and `queued` (handled here).
- Schedule slots are pre-configured based on optimal posting times for the user's audience (Netherlands-based, architecture/urban/liminal photography enthusiasts).
- Batch size: 2–3 posts per schedule window. Overflow goes to the next window.
- **Open investigation**: Instagram Graph API may support server-side scheduling via a `publish_time` parameter on container creation (up to 75 days out). If confirmed, this is strongly preferred over a client-side scheduler because:
  - No need for the app to be running at publish time.
  - Instagram handles the timing server-side.
  - Simpler architecture.
- This directive covers both approaches. Implementation should investigate Instagram-side scheduling first and fall back to client-side only if needed.

## Inputs

- Required:
  - `--manifest <path>`: Path to a manifest with `review.publish_mode: "queued"`.
  - OR `--list`: Show the current queue.
  - OR `--next`: Show the next available slot.
- Optional:
  - `--dry-run`: Show what slot the post would be assigned to without modifying anything.
- Configuration file: `config/schedule.json` (or section in `config/ppw.toml`).
- Environment variables: same as publish stage (Instagram credentials needed for Instagram-side scheduling).

## Outputs

- Files updated: `manifest.json` (scheduling section added).
- Manifest state transition: `approved` → `scheduled`.
- Queue file updated: `state/schedule_queue.json`.

### Manifest Section Written

```json
{
  "scheduling": {
    "scheduled_for": "2026-02-12T18:00:00+01:00",
    "slot_id": "wednesday-18:00",
    "batch_id": "2026-02-12-evening",
    "queued_at": "2026-02-10T19:00:00Z",
    "method": "instagram_publish_time"
  }
}
```

## Steps (high-level)

### Approach A: Instagram-Side Scheduling (Preferred)

1. During implementation, verify that the Instagram Graph API `publish_time` parameter works for:
   - Single-image posts.
   - Carousel posts.
   - The user's Creator account tier.
2. If supported:
   a. Calculate the next available slot from the schedule configuration.
   b. Create media containers as in the publish stage (upload to R2, create containers).
   c. Pass `publish_time=<unix_timestamp>` on the final container creation call.
   d. Instagram schedules the post server-side. No need for client-side polling or cron.
   e. Record the scheduled time in the manifest.
   f. Clean up R2 images only after the scheduled publish time (or set R2 object TTL).
3. If not supported or unreliable: fall back to Approach B.

### Approach B: Client-Side Scheduling (Fallback)

1. Calculate the next available slot.
2. Add the post to the schedule queue (`state/schedule_queue.json`).
3. Set manifest state to `scheduled`.
4. At publish time, one of:
   a. The TUI (if running) checks for due posts and triggers `ppw publish`.
   b. A lightweight cron job / macOS launchd plist runs `ppw publish-scheduled` periodically.
5. Note: this requires either the app to be running or an external scheduler. Less ideal.

### Slot Assignment Logic

1. Load the schedule configuration (timezone, slots, batch size).
2. Load the current queue to see which slots are already occupied.
3. Find the next slot that:
   a. Is in the future (relative to now in the configured timezone).
   b. Has fewer than `batch_size` posts assigned.
4. Assign the post to that slot.
5. If all slots for the next 7 days are full, assign to the first available slot beyond that and log a warning.

## Schedule Configuration

```json
{
  "timezone": "Europe/Amsterdam",
  "slots": [
    { "day": "monday",    "time": "08:00" },
    { "day": "monday",    "time": "18:30" },
    { "day": "wednesday", "time": "12:00" },
    { "day": "wednesday", "time": "19:00" },
    { "day": "friday",    "time": "08:30" },
    { "day": "friday",    "time": "18:00" },
    { "day": "sunday",    "time": "10:00" }
  ],
  "batch_size": 3,
  "min_gap_hours": 4
}
```

- `min_gap_hours`: Minimum gap between consecutive posts in the same slot to avoid flooding.

## Edge Cases / Failure Modes

- **All near-term slots full**: Assign to the first available slot beyond the next 7 days. Log a warning: `"All slots in the next 7 days are full. Scheduled for <date>."`.
- **Schedule config missing or invalid**: Exit with error code 2. Do not guess defaults.
- **Instagram `publish_time` rejected**: If the API returns an error for the scheduled time (e.g., too far in the future, past time), fall back to the next valid slot or fail with a clear message.
- **Post needs rescheduling**: The user can re-queue a scheduled post via the TUI. The old slot assignment is freed.
- **Duplicate scheduling**: If a manifest already has a `scheduling` section, warn and ask for confirmation (or use `--force` flag).

## Acceptance Criteria

- [ ] Given an approved manifest with `publish_mode: "queued"`, the tool assigns it to the next available schedule slot.
- [ ] Given a full batch in the next slot, the tool assigns the post to the following slot.
- [ ] Given `--list`, the tool shows all currently scheduled posts with their times.
- [ ] Given `--next`, the tool shows the next available slot and how many openings remain.
- [ ] Given `--dry-run`, the tool shows the slot assignment without modifying any files.
- [ ] The schedule respects the configured timezone (`Europe/Amsterdam`).
- [ ] The queue file (`state/schedule_queue.json`) is updated atomically.
- [ ] If Instagram-side scheduling is available, `scheduling.method` is set to `"instagram_publish_time"`.
- [ ] If client-side scheduling is used, `scheduling.method` is set to `"client_queue"`.

## Safety Notes

- `--dry-run` must be supported.
- Never schedule a post in the past. Validate that the assigned slot is in the future.
- Queue file writes are atomic (temp file + rename).
- If the Instagram `publish_time` approach is used, ensure R2 images are not deleted before the scheduled publish time.

## Learnings (append-only)

- (None yet)
