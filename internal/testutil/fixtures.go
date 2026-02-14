// Package testutil provides helpers for generating test JPEG fixtures.
package testutil

import (
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
)

// CreateJPEG creates a minimal JPEG file with the given dimensions.
// No EXIF data is embedded (plain JPEG).
func CreateJPEG(path string, width, height int) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	// Fill with a solid color so it's a valid image
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: 100, G: 100, B: 100, A: 255})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return jpeg.Encode(f, img, &jpeg.Options{Quality: 50})
}

// SetupSamplePost creates a sample post directory with numbered portrait images.
func SetupSamplePost(dir string) error {
	// 4:5 portrait images (800x1000)
	for _, name := range []string{"photo_1.jpg", "photo_2.jpg", "photo_3.jpg"} {
		if err := CreateJPEG(filepath.Join(dir, name), 800, 1000); err != nil {
			return err
		}
	}
	return nil
}

// SetupMixedRatios creates a post directory with mismatched aspect ratios.
func SetupMixedRatios(dir string) error {
	if err := CreateJPEG(filepath.Join(dir, "img_1.jpg"), 800, 1000); err != nil { // 4:5
		return err
	}
	if err := CreateJPEG(filepath.Join(dir, "img_2.jpg"), 1000, 1000); err != nil { // 1:1
		return err
	}
	return nil
}

// SetupNoSuffix creates a post directory with images that have no numeric suffix.
func SetupNoSuffix(dir string) error {
	for _, name := range []string{"morning.jpg", "afternoon.jpg", "evening.jpg"} {
		if err := CreateJPEG(filepath.Join(dir, name), 1000, 1000); err != nil {
			return err
		}
	}
	return nil
}

// SetupDuplicateSuffix creates a post directory with duplicate numeric suffixes.
func SetupDuplicateSuffix(dir string) error {
	if err := CreateJPEG(filepath.Join(dir, "a_1.jpg"), 800, 1000); err != nil {
		return err
	}
	if err := CreateJPEG(filepath.Join(dir, "b_1.jpg"), 800, 1000); err != nil {
		return err
	}
	return nil
}
