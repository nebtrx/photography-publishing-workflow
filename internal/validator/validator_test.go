package validator

import (
	"testing"

	"photography-publishing-workflow/internal/manifest"
	"photography-publishing-workflow/internal/scanner"
	"photography-publishing-workflow/internal/testutil"
)

func TestValidate_SamplePost(t *testing.T) {
	dir := t.TempDir()
	if err := testutil.SetupSamplePost(dir); err != nil {
		t.Fatalf("setup: %v", err)
	}

	m, err := scanner.Scan(dir)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	v := Validate(m)

	if !v.Passed {
		t.Error("expected validation to pass")
	}
	if !v.ImageCountValid {
		t.Error("expected image count to be valid")
	}
	if v.ImageCount != 3 {
		t.Errorf("image count = %d, want 3", v.ImageCount)
	}
	if !v.AspectRatioConsistent {
		t.Error("expected aspect ratios to be consistent")
	}
	if v.ResolvedAspectRatio != "4:5" {
		t.Errorf("resolved aspect ratio = %q, want %q", v.ResolvedAspectRatio, "4:5")
	}
	if len(v.Issues) != 0 {
		t.Errorf("expected 0 issues, got %d: %v", len(v.Issues), v.Issues)
	}
}

func TestValidate_MixedRatios(t *testing.T) {
	dir := t.TempDir()
	if err := testutil.SetupMixedRatios(dir); err != nil {
		t.Fatalf("setup: %v", err)
	}

	m, err := scanner.Scan(dir)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	v := Validate(m)

	if !v.AspectRatioConsistent == true {
		// Should be inconsistent
	}
	if v.AspectRatioConsistent {
		t.Error("expected aspect ratio inconsistency")
	}

	// Should still pass overall (aspect ratio mismatch is a warning, not an error)
	if !v.Passed {
		t.Error("expected validation to pass (warnings only)")
	}

	// Should have a warning issue
	hasWarning := false
	for _, issue := range v.Issues {
		if issue.Severity == SeverityWarning {
			hasWarning = true
		}
	}
	if !hasWarning {
		t.Error("expected a warning issue for aspect ratio mismatch")
	}
}

func TestValidate_DuplicateSuffix(t *testing.T) {
	dir := t.TempDir()
	if err := testutil.SetupDuplicateSuffix(dir); err != nil {
		t.Fatalf("setup: %v", err)
	}

	m, err := scanner.Scan(dir)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	v := Validate(m)

	if v.Passed {
		t.Error("expected validation to fail for duplicate suffixes")
	}

	hasError := false
	for _, issue := range v.Issues {
		if issue.Severity == SeverityError {
			hasError = true
		}
	}
	if !hasError {
		t.Error("expected an error issue for duplicate suffixes")
	}
}

func TestValidate_TooManyImages(t *testing.T) {
	m := manifest.New("big-post", "/tmp/big-post")
	for i := 1; i <= 25; i++ {
		m.Images = append(m.Images, manifest.Image{
			Filename:    "img.jpg",
			Width:       800,
			Height:      1000,
			AspectRatio: "4:5",
			IsHero:      i == 1,
		})
	}

	v := Validate(m)

	if v.ImageCountValid {
		t.Error("expected image count to be invalid")
	}
	if v.Passed {
		t.Error("expected validation to fail for >20 images")
	}
}

func TestValidate_SingleImage(t *testing.T) {
	dir := t.TempDir()
	if err := testutil.CreateJPEG(dir+"/solo_1.jpg", 1000, 1000); err != nil {
		t.Fatalf("setup: %v", err)
	}

	m, err := scanner.Scan(dir)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	v := Validate(m)

	if !v.Passed {
		t.Error("expected validation to pass for single image")
	}
	if v.ImageCount != 1 {
		t.Errorf("image count = %d, want 1", v.ImageCount)
	}
}

func TestApply_TransitionsState(t *testing.T) {
	dir := t.TempDir()
	if err := testutil.SetupSamplePost(dir); err != nil {
		t.Fatalf("setup: %v", err)
	}

	m, err := scanner.Scan(dir)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	if err := Apply(m); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if m.State != manifest.StateValidated {
		t.Errorf("state = %q, want %q", m.State, manifest.StateValidated)
	}
	if m.Validation == nil {
		t.Fatal("validation section is nil")
	}
	if !m.Validation.Passed {
		t.Error("expected validation to pass")
	}
}

func TestApply_WrongState(t *testing.T) {
	m := manifest.New("test", "/tmp/test")
	m.State = manifest.StateValidated // not scanned

	if err := Apply(m); err == nil {
		t.Error("expected error for wrong initial state")
	}
}
