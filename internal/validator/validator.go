// Package validator checks a scanned manifest for issues:
// aspect ratio consistency, image count limits, ordering integrity,
// and hero image presence. It writes validation results to the manifest.
package validator

import (
	"fmt"

	"photography-publishing-workflow/internal/manifest"
	"photography-publishing-workflow/internal/scanner"
)

const (
	// Instagram carousel limits.
	MinImages = 1
	MaxImages = 10

	SeverityError   = "error"
	SeverityWarning = "warning"
)

// Validate runs all validation checks on a scanned manifest and populates
// the Validation section. Returns the validation result.
func Validate(m *manifest.Manifest) *manifest.Validation {
	v := &manifest.Validation{
		Passed:     true,
		Issues:     []manifest.ValidationIssue{},
		ImageCount: len(m.Images),
	}

	checkImageCount(m, v)
	checkAspectRatios(m, v)
	checkOrdering(m, v)
	checkHeroImage(m, v)

	// Determine overall pass/fail: any error-severity issue fails
	for _, issue := range v.Issues {
		if issue.Severity == SeverityError {
			v.Passed = false
			break
		}
	}

	return v
}

// Apply runs validation and writes results to the manifest, transitioning
// state to validated (or error if blocking issues exist).
func Apply(m *manifest.Manifest) error {
	if m.State != manifest.StateScanned {
		return fmt.Errorf("validator: expected state %q, got %q", manifest.StateScanned, m.State)
	}

	v := Validate(m)
	m.Validation = v

	if v.Passed {
		return m.Transition(manifest.StateValidated)
	}

	// Check if there are any error-severity issues (blocking)
	hasBlockingError := false
	for _, issue := range v.Issues {
		if issue.Severity == SeverityError {
			hasBlockingError = true
			break
		}
	}

	if hasBlockingError {
		return m.Transition(manifest.StateError)
	}

	// Warnings only — still pass
	return m.Transition(manifest.StateValidated)
}

func checkImageCount(m *manifest.Manifest, v *manifest.Validation) {
	count := len(m.Images)
	if count == 0 {
		v.ImageCountValid = false
		v.Issues = append(v.Issues, manifest.ValidationIssue{
			Severity: SeverityError,
			Message:  "No JPEG images found in directory.",
		})
		return
	}
	if count > MaxImages {
		v.ImageCountValid = false
		v.Issues = append(v.Issues, manifest.ValidationIssue{
			Severity: SeverityError,
			Message:  fmt.Sprintf("Image count (%d) exceeds Instagram carousel maximum (%d).", count, MaxImages),
		})
		return
	}
	v.ImageCountValid = true
}

func checkAspectRatios(m *manifest.Manifest, v *manifest.Validation) {
	ratios := scanner.UniqueAspectRatios(m.Images)

	switch len(ratios) {
	case 0:
		v.AspectRatioConsistent = false
		v.Issues = append(v.Issues, manifest.ValidationIssue{
			Severity: SeverityWarning,
			Message:  "Could not determine aspect ratio for any image.",
		})
	case 1:
		v.AspectRatioConsistent = true
		v.ResolvedAspectRatio = ratios[0]
	default:
		v.AspectRatioConsistent = false
		v.ResolvedAspectRatio = scanner.MajorityAspectRatio(m.Images)
		v.Issues = append(v.Issues, manifest.ValidationIssue{
			Severity: SeverityWarning,
			Message:  fmt.Sprintf("Aspect ratio mismatch: images map to %v. All images in a post must share the same ratio.", ratios),
		})
	}
}

func checkOrdering(m *manifest.Manifest, v *manifest.Validation) {
	// Check for duplicate suffixes
	dupes := scanner.HasDuplicateSuffixes(m.Images)
	for n, files := range dupes {
		v.Issues = append(v.Issues, manifest.ValidationIssue{
			Severity: SeverityError,
			Message:  fmt.Sprintf("Duplicate ordering suffix _%d found on: %v", n, files),
		})
	}

	// Check for mixed ordering conventions
	if scanner.HasMixedOrdering(m.Images) {
		v.Issues = append(v.Issues, manifest.ValidationIssue{
			Severity: SeverityWarning,
			Message:  "Mixed ordering conventions: some images have numeric suffixes, some do not. Images with suffixes are ordered first.",
		})
	}
}

func checkHeroImage(m *manifest.Manifest, v *manifest.Validation) {
	heroCount := 0
	for _, img := range m.Images {
		if img.IsHero {
			heroCount++
		}
	}
	if heroCount == 0 {
		v.Issues = append(v.Issues, manifest.ValidationIssue{
			Severity: SeverityError,
			Message:  "No hero image found. The first image in order should be marked as hero.",
		})
	}
	if heroCount > 1 {
		v.Issues = append(v.Issues, manifest.ValidationIssue{
			Severity: SeverityWarning,
			Message:  fmt.Sprintf("Multiple hero images found (%d). Only the first should be hero.", heroCount),
		})
	}
}
