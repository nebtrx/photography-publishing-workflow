# Directive: Scan and Validate Post Directory

## Goal

Given a directory of exported JPEG images, produce a manifest that describes the post: image inventory with ordering, EXIF metadata, aspect ratios, and a validation verdict (pass/fail with actionable issues). The manifest is the single data contract consumed by all downstream pipeline stages.

## Context / Constraints

- Images arrive Instagram-ready from Lightroom. No processing, resizing, or format conversion.
- One directory = one post. All images for a single post reside in the same directory.
- EXIF metadata is preserved and must be extracted (GPS, camera, lens, capture date).
- This stage is the pipeline entry point — it creates the manifest from scratch.
- No network calls. Purely local filesystem + EXIF reading.

## Inputs

- Required:
  - `--dir <path>`: Path to a post directory containing JPEG images.
- Optional:
  - `--out <path>`: Override manifest output path (default: `<dir>/manifest.json`).
  - `--dry-run`: Print what would be written without creating the manifest.

## Outputs

- Files created: `manifest.json` inside the post directory.
- Manifest state after scan: `scanned`.
- Manifest state after validation: `validated` (if passed) or `error` (if failed with blocking issues).

### Manifest Sections Written

**`images[]`** — one entry per JPEG found:
```json
{
  "filename": "erasmusbrug-sunset_1.jpg",
  "path": "/absolute/path/to/erasmusbrug-sunset_1.jpg",
  "order": 1,
  "is_hero": true,
  "width": 4000,
  "height": 5000,
  "aspect_ratio": "4:5",
  "exif": {
    "camera": "Nikon Z8",
    "lens": "NIKKOR Z 24-70mm f/2.8 S",
    "focal_length": "35mm",
    "aperture": "f/2.8",
    "shutter_speed": "1/250",
    "iso": 400,
    "capture_date": "2026-02-09T17:45:00Z",
    "gps": {
      "latitude": 51.9094,
      "longitude": 4.4864
    }
  }
}
```

**`validation`**:
```json
{
  "passed": true,
  "issues": [],
  "aspect_ratio_consistent": true,
  "resolved_aspect_ratio": "4:5",
  "image_count_valid": true,
  "image_count": 4
}
```

## Steps (high-level)

### Scan

1. List all files in the directory. Filter to JPEG files (`.jpg`, `.jpeg`, case-insensitive).
2. For each JPEG:
   a. Read image dimensions (width, height).
   b. Compute aspect ratio and map to nearest Instagram-accepted ratio (see rules below).
   c. Extract EXIF metadata: camera model, lens, focal length, aperture, shutter speed, ISO, capture date, GPS coordinates.
   d. Parse the `_<N>` numeric suffix from the filename to determine ordering.
3. Sort images by ordering number (ascending). If no numeric suffixes are found, fall back to EXIF capture date (ascending).
4. Mark the first image in the sorted order as `is_hero: true`.
5. Generate a post `id` from the directory name.
6. Write the manifest with `state: "scanned"`.

### Validate

7. Check aspect ratio consistency: all images must share the same resolved aspect ratio.
8. Check image count: at least 1, at most 20 (Instagram carousel maximum).
   - 1 image = single post.
   - 2–20 images = carousel.
9. Check ordering: no duplicate order numbers, no gaps if using suffix convention.
10. Check hero image: exactly one image with `is_hero: true`.
11. If all checks pass: set `state: "validated"`, `validation.passed: true`.
12. If any check fails: set `validation.passed: false`, populate `validation.issues[]` with actionable descriptions. Set `state: "error"` only for blocking issues (e.g., 0 images).

## Edge Cases / Failure Modes

### Image Ordering
- **No numeric suffixes**: Fall back to EXIF capture date. If EXIF dates are also missing, fall back to filesystem modification time with a warning in `validation.issues`.
- **Duplicate suffixes** (e.g., two `_1` files): Validation fails. Issue: `"Duplicate ordering suffix _1 found on: file_a.jpg, file_b.jpg"`.
- **Gaps in suffixes** (e.g., `_1`, `_3` but no `_2`): Non-blocking warning. Images are ordered by their suffix values, gaps are tolerated.
- **Mixed conventions** (some files have suffixes, some don't): Validation warning. Files with suffixes are ordered first; files without suffixes are appended, ordered by EXIF date.

### Aspect Ratios
- **Mapping rule**: Map actual pixel ratio to the nearest Instagram ratio:
  - Width/Height ≤ 0.85 → `4:5` (portrait)
  - 0.85 < W/H < 1.15 → `1:1` (square)
  - W/H ≥ 1.15 → `1.91:1` (landscape)
- **Tolerance**: Two images match if they map to the same Instagram ratio (not pixel-exact).
- **Mismatch**: Non-blocking validation issue. Caption: `"Aspect ratio mismatch: images map to [4:5, 1:1]. All images in a post must share the same ratio."` The post can proceed to review where the user decides.

### File Issues
- **Empty directory**: Validation error. `"No JPEG images found in directory."` State: `error`.
- **Non-JPEG files present**: Ignored silently. Only `.jpg`/`.jpeg` files are processed.
- **Corrupted JPEG** (can't read dimensions/EXIF): Include in manifest with `width: 0, height: 0`, EXIF fields null. Validation warning: `"Could not read image metadata for: corrupt_file.jpg"`.
- **Directory does not exist**: Exit with error code 2, message to stderr.

### EXIF Edge Cases
- **No GPS data**: `exif.gps` is `null`. Not a validation issue — location identification falls back during enrichment.
- **No capture date**: `exif.capture_date` is `null`. Warning if this image is needed for date-based ordering fallback.
- **Minimal EXIF** (e.g., phone camera): Extract whatever is available, leave missing fields as `null`.

## Acceptance Criteria

- [ ] Given a directory with `img_1.jpg`, `img_2.jpg`, `img_3.jpg`, the manifest orders them 1, 2, 3 and marks `img_1.jpg` as hero.
- [ ] Given images without numeric suffixes, the manifest orders by EXIF capture date.
- [ ] Given images with mismatched aspect ratios (e.g., one 4:5, one 1:1), validation reports the mismatch as a non-blocking issue.
- [ ] Given an empty directory, the tool exits with a clear error and does not create a manifest.
- [ ] Given `--dry-run`, the tool prints what it would write but creates no files.
- [ ] Given a directory with 21+ images, validation flags `"Image count exceeds Instagram carousel maximum (20)"`.
- [ ] Given a single image, the manifest has `image_count: 1` and validation passes.
- [ ] EXIF GPS, camera model, lens, and capture date are extracted when present and `null` when absent.
- [ ] The manifest includes a `version` field for future schema evolution.
- [ ] Atomic file writes: manifest is written to a temp file then renamed, preventing partial writes.

## Safety Notes

- `--dry-run` must be supported. In dry-run mode, no files are created or modified.
- Never modify or delete source images. This tool is read-only with respect to images.
- Atomic writes: write to `manifest.json.tmp` then rename to `manifest.json`.

## Learnings (append-only)

- (None yet)
