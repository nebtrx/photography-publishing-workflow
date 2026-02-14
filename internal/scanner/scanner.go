// Package scanner detects JPEG images in a post directory, extracts EXIF
// metadata, determines image ordering, computes aspect ratios, and produces
// an initial manifest.
package scanner

import (
	"fmt"
	"image"
	_ "image/jpeg"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	goexif "github.com/rwcarlsen/goexif/exif"
	"github.com/rwcarlsen/goexif/tiff"

	"photography-publishing-workflow/internal/manifest"
)

// suffixRe matches a numeric suffix like _1, _02, _12 before the file extension.
var suffixRe = regexp.MustCompile(`_(\d+)\.[jJ][pP][eE]?[gG]$`)

// Scan reads a directory and produces a manifest with image inventory,
// ordering, EXIF metadata, and aspect ratios.
func Scan(dir string) (*manifest.Manifest, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("stat directory: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", dir)
	}

	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolve absolute path: %w", err)
	}

	entries, err := os.ReadDir(absDir)
	if err != nil {
		return nil, fmt.Errorf("read directory: %w", err)
	}

	var images []manifest.Image
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !isJPEG(e.Name()) {
			continue
		}

		imgPath := filepath.Join(absDir, e.Name())
		img, err := scanImage(imgPath, e.Name())
		if err != nil {
			// Include the image with degraded metadata rather than skipping
			img = &manifest.Image{
				Filename:    e.Name(),
				Path:        imgPath,
				AspectRatio: "unknown",
				EXIF:        manifest.EXIF{},
			}
		}
		images = append(images, *img)
	}

	if len(images) == 0 {
		return nil, fmt.Errorf("no JPEG images found in %s", absDir)
	}

	orderImages(images)

	postID := filepath.Base(absDir)
	m := manifest.New(postID, absDir)
	m.Images = images

	return m, nil
}

// isJPEG checks if a filename has a JPEG extension.
func isJPEG(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, ".jpg") || strings.HasSuffix(lower, ".jpeg")
}

// scanImage reads a single image file and extracts dimensions and EXIF metadata.
func scanImage(path, filename string) (*manifest.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", filename, err)
	}
	defer f.Close()

	// Decode image config for dimensions (doesn't decode full image)
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return nil, fmt.Errorf("decode image config %s: %w", filename, err)
	}

	// Seek back to start for EXIF
	if _, err := f.Seek(0, 0); err != nil {
		return nil, fmt.Errorf("seek %s: %w", filename, err)
	}

	exifData := extractEXIF(f)

	// Determine effective dimensions considering EXIF orientation
	width, height := cfg.Width, cfg.Height
	if exifData.orientation >= 5 && exifData.orientation <= 8 {
		// Orientations 5-8 swap width and height
		width, height = height, width
	}

	return &manifest.Image{
		Filename:    filename,
		Path:        path,
		Width:       width,
		Height:      height,
		AspectRatio: classifyAspectRatio(width, height),
		EXIF:        exifData.metadata,
	}, nil
}

type exifResult struct {
	metadata    manifest.EXIF
	orientation int
}

// extractEXIF reads EXIF metadata from an open file. Returns zero-value
// fields for any metadata that can't be read.
func extractEXIF(f *os.File) exifResult {
	result := exifResult{}

	x, err := goexif.Decode(f)
	if err != nil {
		return result
	}

	// Orientation
	if tag, err := x.Get(goexif.Orientation); err == nil {
		if v, err := tag.Int(0); err == nil {
			result.orientation = v
		}
	}

	// Camera model
	if tag, err := x.Get(goexif.Model); err == nil {
		result.metadata.Camera = cleanString(tag)
	}

	// Lens model
	if tag, err := x.Get(goexif.LensModel); err == nil {
		result.metadata.Lens = cleanString(tag)
	}

	// Focal length
	if tag, err := x.Get(goexif.FocalLength); err == nil {
		if num, den, err := tag.Rat2(0); err == nil && den != 0 {
			fl := float64(num) / float64(den)
			result.metadata.FocalLength = fmt.Sprintf("%.0fmm", fl)
		}
	}

	// Aperture (FNumber)
	if tag, err := x.Get(goexif.FNumber); err == nil {
		if num, den, err := tag.Rat2(0); err == nil && den != 0 {
			fnum := float64(num) / float64(den)
			result.metadata.Aperture = fmt.Sprintf("f/%.1f", fnum)
		}
	}

	// Shutter speed (ExposureTime)
	if tag, err := x.Get(goexif.ExposureTime); err == nil {
		if num, den, err := tag.Rat2(0); err == nil && den != 0 {
			if num == 1 {
				result.metadata.ShutterSpeed = fmt.Sprintf("1/%d", den)
			} else {
				speed := float64(num) / float64(den)
				if speed >= 1 {
					result.metadata.ShutterSpeed = fmt.Sprintf("%.1fs", speed)
				} else {
					result.metadata.ShutterSpeed = fmt.Sprintf("1/%.0f", 1.0/speed)
				}
			}
		}
	}

	// ISO
	if tag, err := x.Get(goexif.ISOSpeedRatings); err == nil {
		if v, err := tag.Int(0); err == nil {
			result.metadata.ISO = v
		}
	}

	// Capture date
	if tm, err := x.DateTime(); err == nil {
		utc := tm.UTC()
		result.metadata.CaptureDate = &utc
	}

	// GPS
	if lat, lon, err := x.LatLong(); err == nil {
		result.metadata.GPS = &manifest.GPS{
			Latitude:  lat,
			Longitude: lon,
		}
	}

	return result
}

// cleanString extracts a clean string from an EXIF tag value.
func cleanString(tag *tiff.Tag) string {
	s := tag.String()
	// EXIF strings are often quoted
	s = strings.Trim(s, "\"")
	return strings.TrimSpace(s)
}

// classifyAspectRatio maps pixel dimensions to the nearest Instagram-accepted ratio.
func classifyAspectRatio(width, height int) string {
	if height == 0 {
		return "unknown"
	}
	ratio := float64(width) / float64(height)

	switch {
	case ratio < 0.85:
		return "4:5" // portrait
	case ratio >= 0.85 && ratio < 1.15:
		return "1:1" // square
	default:
		return "1.91:1" // landscape
	}
}

// orderImages determines the display order of images. It tries numeric suffix
// ordering first, falling back to EXIF capture date, then filename sort.
func orderImages(images []manifest.Image) {
	// Try numeric suffix ordering
	allHaveSuffix := true
	for i := range images {
		n, ok := parseSuffix(images[i].Filename)
		if ok {
			images[i].Order = n
		} else {
			allHaveSuffix = false
		}
	}

	if allHaveSuffix {
		sort.Slice(images, func(i, j int) bool {
			return images[i].Order < images[j].Order
		})
	} else {
		// Fallback: EXIF capture date, then filename
		sort.Slice(images, func(i, j int) bool {
			di := images[i].EXIF.CaptureDate
			dj := images[j].EXIF.CaptureDate
			if di != nil && dj != nil && !di.Equal(*dj) {
				return di.Before(*dj)
			}
			return images[i].Filename < images[j].Filename
		})
		// Assign sequential order numbers
		for i := range images {
			images[i].Order = i + 1
		}
	}

	// Mark hero
	if len(images) > 0 {
		images[0].IsHero = true
	}
}

// parseSuffix extracts the numeric suffix from a filename (e.g., "photo_3.jpg" → 3).
func parseSuffix(filename string) (int, bool) {
	matches := suffixRe.FindStringSubmatch(filename)
	if len(matches) < 2 {
		return 0, false
	}
	n, err := strconv.Atoi(matches[1])
	if err != nil {
		return 0, false
	}
	return n, true
}

// HasMixedOrdering returns true if some images have numeric suffixes and some don't.
func HasMixedOrdering(images []manifest.Image) bool {
	has, missing := 0, 0
	for _, img := range images {
		if _, ok := parseSuffix(img.Filename); ok {
			has++
		} else {
			missing++
		}
	}
	return has > 0 && missing > 0
}

// HasDuplicateSuffixes returns filenames that share the same numeric suffix.
func HasDuplicateSuffixes(images []manifest.Image) map[int][]string {
	seen := map[int][]string{}
	for _, img := range images {
		if n, ok := parseSuffix(img.Filename); ok {
			seen[n] = append(seen[n], img.Filename)
		}
	}
	dupes := map[int][]string{}
	for n, files := range seen {
		if len(files) > 1 {
			dupes[n] = files
		}
	}
	return dupes
}

// UniqueAspectRatios returns the distinct aspect ratios found in the image set.
func UniqueAspectRatios(images []manifest.Image) []string {
	seen := map[string]bool{}
	var ratios []string
	for _, img := range images {
		if !seen[img.AspectRatio] && img.AspectRatio != "unknown" {
			seen[img.AspectRatio] = true
			ratios = append(ratios, img.AspectRatio)
		}
	}
	return ratios
}

// MajorityAspectRatio returns the most common aspect ratio in the image set.
func MajorityAspectRatio(images []manifest.Image) string {
	counts := map[string]int{}
	for _, img := range images {
		if img.AspectRatio != "unknown" {
			counts[img.AspectRatio]++
		}
	}
	best := ""
	bestCount := 0
	for r, c := range counts {
		if c > bestCount {
			best = r
			bestCount = c
		}
	}
	return best
}
