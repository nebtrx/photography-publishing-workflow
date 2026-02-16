// Package pipeline chains multiple pipeline stages together, running
// scan → validate → enrich in a single operation.
package pipeline

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"photography-publishing-workflow/internal/ai"
	"photography-publishing-workflow/internal/enricher"
	"photography-publishing-workflow/internal/manifest"
	"photography-publishing-workflow/internal/obslog"
	"photography-publishing-workflow/internal/scanner"
	"photography-publishing-workflow/internal/validator"
)

// Options configures the pipeline.
type Options struct {
	CorpusPath   string
	SkipLocation bool
	SkipMusic    bool
	DryRun       bool
	LogOutput    io.Writer
}

// Pipeline chains scan → validate → enrich for a post directory.
type Pipeline struct {
	provider ai.Provider
	opts     Options
	logger   *log.Logger
}

// New creates a Pipeline.
func New(provider ai.Provider, opts Options) *Pipeline {
	logOutput := opts.LogOutput
	if logOutput == nil {
		logOutput = os.Stderr
	}

	return &Pipeline{
		provider: provider,
		opts:     opts,
		logger:   log.New(logOutput, "[pipeline] ", log.LstdFlags),
	}
}

// Result holds the outcome of a pipeline run.
type Result struct {
	PostID       string
	ManifestPath string
	FinalState   manifest.State
	Error        error
}

// Run executes the full scan → validate → enrich pipeline on a directory.
func (p *Pipeline) Run(ctx context.Context, dir string) *Result {
	result := &Result{}
	postHint := filepath.Base(dir)

	// 1. Scan
	scanStart := obslog.Intent(
		p.logger,
		"pipeline",
		postHint,
		postHint,
		"scan",
		"extract image metadata and create manifest",
		map[string]any{"source_dir": dir},
	)
	p.logger.Printf("Scanning %s...", dir)
	m, err := scanner.Scan(dir)
	if err != nil {
		obslog.Result(p.logger, "pipeline", postHint, postHint, "scan", scanStart, err, nil)
		result.Error = fmt.Errorf("scan: %w", err)
		return result
	}
	result.PostID = m.ID
	postHint = result.PostID

	manifestPath := manifest.ManifestPath(dir)
	result.ManifestPath = manifestPath

	if p.opts.DryRun {
		obslog.Result(
			p.logger,
			"pipeline",
			postHint,
			postHint,
			"scan",
			scanStart,
			nil,
			map[string]any{"image_count": len(m.Images), "dry_run": true},
		)
		p.logger.Printf("[DRY RUN] Scanned %s: %d images", m.ID, len(m.Images))
		p.dryRunValidate(m)
		p.logger.Printf("[DRY RUN] Would enrich with AI")
		result.FinalState = m.State
		return result
	}

	if err := m.Write(manifestPath); err != nil {
		obslog.Result(p.logger, "pipeline", postHint, postHint, "scan", scanStart, err, nil)
		result.Error = fmt.Errorf("write after scan: %w", err)
		return result
	}
	obslog.Result(
		p.logger,
		"pipeline",
		postHint,
		postHint,
		"scan",
		scanStart,
		nil,
		map[string]any{"image_count": len(m.Images), "manifest_path": manifestPath},
	)
	p.logger.Printf("Scanned %s: %d images", m.ID, len(m.Images))

	// 2. Validate
	validateStart := obslog.Intent(
		p.logger,
		"pipeline",
		postHint,
		postHint,
		"validate",
		"validate image set and post constraints",
		map[string]any{"image_count": len(m.Images)},
	)
	if err := validator.Apply(m); err != nil {
		m.RecordFailure(manifest.FailureStageValidate, err.Error(), manifest.StateScanned)
		_ = m.Write(manifestPath)
		obslog.Result(p.logger, "pipeline", postHint, postHint, "validate", validateStart, err, nil)
		result.Error = fmt.Errorf("validate: %w", err)
		result.FinalState = m.State
		return result
	}
	if m.State == manifest.StateError {
		validationErr := fmt.Errorf("%s", validationErrorSummary(m.Validation))
		m.RecordFailure(manifest.FailureStageValidate, validationErr.Error(), manifest.StateScanned)
		if err := m.Write(manifestPath); err != nil {
			obslog.Result(p.logger, "pipeline", postHint, postHint, "validate", validateStart, err, nil)
			result.Error = fmt.Errorf("write after validate failure: %w", err)
			result.FinalState = m.State
			return result
		}
		obslog.Result(p.logger, "pipeline", postHint, postHint, "validate", validateStart, validationErr, nil)
		result.Error = validationErr
		result.FinalState = m.State
		return result
	}
	if err := m.Write(manifestPath); err != nil {
		obslog.Result(p.logger, "pipeline", postHint, postHint, "validate", validateStart, err, nil)
		result.Error = fmt.Errorf("write after validate: %w", err)
		return result
	}
	obslog.Result(
		p.logger,
		"pipeline",
		postHint,
		postHint,
		"validate",
		validateStart,
		nil,
		map[string]any{
			"passed":             m.Validation != nil && m.Validation.Passed,
			"resolved_aspect":    validationAspect(m),
			"validation_message": validationSummary(m.Validation),
		},
	)
	p.logger.Printf("Validated %s: %s", m.ID, validationSummary(m.Validation))

	// 3. Enrich
	e := enricher.New(p.provider, enricher.Options{
		SkipLocation: p.opts.SkipLocation,
		SkipMusic:    p.opts.SkipMusic,
		CorpusPath:   p.opts.CorpusPath,
		LogOutput:    p.opts.LogOutput,
	})

	enrichStart := obslog.Intent(
		p.logger,
		"pipeline",
		postHint,
		postHint,
		"enrich",
		"generate caption, location, and music enrichment",
		map[string]any{
			"skip_location": p.opts.SkipLocation,
			"skip_music":    p.opts.SkipMusic,
		},
	)
	p.logger.Printf("Enriching %s...", m.ID)
	if err := e.Enrich(ctx, m); err != nil {
		m.RecordFailure(manifest.FailureStageEnrich, err.Error(), manifest.StateValidated)
		_ = m.Write(manifestPath)
		obslog.Result(p.logger, "pipeline", postHint, postHint, "enrich", enrichStart, err, nil)
		result.Error = fmt.Errorf("enrich: %w", err)
		result.FinalState = m.State
		return result
	}
	if err := m.Write(manifestPath); err != nil {
		obslog.Result(p.logger, "pipeline", postHint, postHint, "enrich", enrichStart, err, nil)
		result.Error = fmt.Errorf("write after enrich: %w", err)
		return result
	}
	obslog.Result(
		p.logger,
		"pipeline",
		postHint,
		postHint,
		"enrich",
		enrichStart,
		nil,
		map[string]any{
			"final_state":    m.State,
			"caption_exists": m.Enrichment != nil && m.Enrichment.Caption != nil,
			"location_exists": m.Enrichment != nil &&
				m.Enrichment.Location != nil,
			"music_exists": m.Enrichment != nil &&
				m.Enrichment.MusicSuggestion != nil,
		},
	)
	p.logger.Printf("Enriched %s → %s", m.ID, m.State)

	result.FinalState = m.State
	return result
}

func validationAspect(m *manifest.Manifest) string {
	if m == nil || m.Validation == nil {
		return ""
	}
	return m.Validation.ResolvedAspectRatio
}

func (p *Pipeline) dryRunValidate(m *manifest.Manifest) {
	v := validator.Validate(m)
	if v.Passed {
		p.logger.Printf("[DRY RUN] Validation: PASSED (%s)", validationSummary(v))
	} else {
		p.logger.Printf("[DRY RUN] Validation: FAILED")
		for _, issue := range v.Issues {
			p.logger.Printf("[DRY RUN]   [%s] %s", issue.Severity, issue.Message)
		}
	}
}

func validationSummary(v *manifest.Validation) string {
	if v == nil {
		return "no validation"
	}
	return fmt.Sprintf("%d images, ratio=%s, passed=%v", v.ImageCount, v.ResolvedAspectRatio, v.Passed)
}

func validationErrorSummary(v *manifest.Validation) string {
	if v == nil {
		return "validation failed"
	}
	var issues []string
	for _, issue := range v.Issues {
		if strings.EqualFold(issue.Severity, "error") {
			msg := strings.TrimSpace(issue.Message)
			if msg != "" {
				issues = append(issues, msg)
			}
		}
	}
	if len(issues) == 0 {
		return "validation failed"
	}
	return "validation failed: " + strings.Join(issues, "; ")
}
