# Directive: Archival

## Goal

After a post is successfully published to Instagram, move the source images to a date-organized archive directory, preserve the final manifest alongside them, and append a log entry to the publish log. The watch directory is kept clean (only unprocessed or in-progress posts remain), and a complete record of all published posts is maintained for reference.

## Context / Constraints

- Source images are moved, not copied. The goal is to keep the watch/export directory clean.
- The publish log is append-only JSONL (one JSON object per line). It serves as a quick-reference index of all published posts without needing to scan the archive.
- The manifest is copied (not moved) to the archive — the original is deleted after archival completes, since the post directory should be removed from the watch folder.
- No network calls. Purely local filesystem operations.

## Inputs

- Required:
  - `--manifest <path>`: Path to a published manifest (`state: "published"`).
- Optional:
  - `--archive-dir <path>`: Override the archive root directory (default: from config `archive.dir`).
  - `--dry-run`: Print what would be moved/written without doing it.
- Configuration:
  - `archive.dir`: Root archive directory (from `config/ppw.toml`).
  - `archive.log_file`: Path to the publish log file (from `config/ppw.toml`).

## Outputs

- Files moved: all images from the post directory → archive subdirectory.
- Files created: `manifest.json` in the archive subdirectory (copy of final manifest).
- Files appended: one line to the publish log (JSONL).
- Files removed: the original post directory (after all files are moved).
- Manifest state transition: `published` → `archived`.

### Archive Directory Structure

```
<archive-dir>/
  2026-02/
    erasmusbrug-sunset/
      manifest.json
      erasmusbrug-sunset_1.jpg
      erasmusbrug-sunset_2.jpg
      erasmusbrug-sunset_3.jpg
      erasmusbrug-sunset_4.jpg
  2026-01/
    canal-houses/
      manifest.json
      canal-houses_1.jpg
      ...
```

- Organized by `YYYY-MM` based on the publish date.
- Subdirectory name matches the post `id` (derived from the original directory name).

### Publish Log Entry (JSONL)

One line per published post, appended to the log file:

```json
{"id":"erasmusbrug-sunset","published_at":"2026-02-10T18:36:00Z","instagram_post_id":"17895695668004550","permalink":"https://www.instagram.com/p/ABC123/","caption":"That quiet moment when the bridge lights...","location":"Erasmusbrug, Rotterdam","image_count":4,"archive_path":"/Users/omar/Photos/Archive/2026-02/erasmusbrug-sunset","story_published":true}
```

### Manifest Section Written

```json
{
  "archival": {
    "archived_at": "2026-02-10T18:37:00Z",
    "archive_dir": "/Users/omar/Photos/Archive/2026-02/erasmusbrug-sunset",
    "log_entry_written": true
  }
}
```

## Steps (high-level)

1. Validate manifest state is `published`.
2. Determine the archive subdirectory: `<archive-dir>/<YYYY-MM>/<post-id>/`.
3. Create the archive subdirectory (including parent `YYYY-MM` if needed).
4. Copy the manifest to the archive subdirectory (update the `archival` section first).
5. Move each image file from the source directory to the archive subdirectory.
6. Verify all files arrived in the archive (compare file count, sizes).
7. Append a log entry to the publish log file.
8. Remove the original post directory (only after all files are verified in the archive).
9. Set manifest state to `archived` (in the archive copy).
10. Log: archive path, image count moved, log entry confirmation.

## Edge Cases / Failure Modes

- **Archive subdirectory already exists** (e.g., re-archival after failure): If the directory exists and contains files, check if they match the source. If they do, skip the move and proceed. If they differ, abort with an error: `"Archive directory already exists with different contents: <path>"`.
- **Partial move failure** (disk full, permission error): Leave the source directory intact. Log which files were moved and which failed. Set state to `error` with a descriptive message. The tool can be re-run to resume.
- **Publish log file doesn't exist**: Create it. No error.
- **Publish log file locked**: Retry once after a short delay. On second failure, log a warning but still complete the archival (the log entry can be added manually).
- **Source directory has non-image files** (e.g., `.DS_Store`): Move all files, not just images. The archive should be a complete copy of the source directory.
- **Post ID collision across months**: Unlikely (directory names are typically unique), but the `YYYY-MM` prefix provides natural namespacing.

## Acceptance Criteria

- [ ] Given a published manifest, all images are moved to `<archive-dir>/<YYYY-MM>/<post-id>/`.
- [ ] The archive directory contains a copy of the final manifest with `state: "archived"`.
- [ ] A log entry is appended to the publish log file with post ID, Instagram post ID, permalink, caption, location, image count, and archive path.
- [ ] The original post directory is removed after all files are verified in the archive.
- [ ] Given `--dry-run`, the tool prints what would be moved/written without performing any operations.
- [ ] Given a partial failure (some files moved, some not), the source directory remains intact and the error is reported clearly.
- [ ] The publish log file is created if it doesn't exist.
- [ ] Given a manifest not in `published` state, the tool exits with an error.

## Safety Notes

- **Move, not delete**: Files are moved to the archive, never deleted outright.
- **Verify before removing source**: The source directory is only removed after all files are confirmed present in the archive (count + size check).
- `--dry-run` must be supported. This is a destructive operation (moves files).
- Atomic log entry writes: the JSONL append should use file locking or atomic append to prevent corruption from concurrent writes (unlikely but defensive).

## Learnings (append-only)

- (None yet)
