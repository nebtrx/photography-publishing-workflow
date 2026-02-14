// Package pipeline chains multiple pipeline stages together, running
// scan → validate → enrich in a single operation.
package pipeline

import (
	"context"
	"fmt"
	"log"
	"os"

	"photography-publishing-workflow/internal/ai"
	"photography-publishing-workflow/internal/enricher"
	"photography-publishing-workflow/internal/manifest"
	"photography-publishing-workflow/internal/scanner"
	"photography-publishing-workflow/internal/validator"
)

// Options configures the pipeline.
type Options struct {
	CorpusPath   string
	SkipLocation bool
	SkipMusic    bool
	DryRun       bool
}

// Pipeline chains scan → validate → enrich for a post directory.
type Pipeline struct {
	provider ai.Provider
	opts     Options
	logger   *log.Logger
}

// New creates a Pipeline.
func New(provider ai.Provider, opts Options) *Pipeline {
	return &Pipeline{
		provider: provider,
		opts:     opts,
		logger:   log.New(os.Stderr, "[pipeline] ", log.LstdFlags),
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

	// 1. Scan
	p.logger.Printf("Scanning %s...", dir)
	m, err := scanner.Scan(dir)
	if err != nil {
		result.Error = fmt.Errorf("scan: %w", err)
		return result
	}
	result.PostID = m.ID

	manifestPath := manifest.ManifestPath(dir)
	result.ManifestPath = manifestPath

	if p.opts.DryRun {
		p.logger.Printf("[DRY RUN] Scanned %s: %d images", m.ID, len(m.Images))
		p.dryRunValidate(m)
		p.logger.Printf("[DRY RUN] Would enrich with AI")
		result.FinalState = m.State
		return result
	}

	if err := m.Write(manifestPath); err != nil {
		result.Error = fmt.Errorf("write after scan: %w", err)
		return result
	}
	p.logger.Printf("Scanned %s: %d images", m.ID, len(m.Images))

	// 2. Validate
	if err := validator.Apply(m); err != nil {
		result.Error = fmt.Errorf("validate: %w", err)
		result.FinalState = m.State
		return result
	}
	if err := m.Write(manifestPath); err != nil {
		result.Error = fmt.Errorf("write after validate: %w", err)
		return result
	}
	p.logger.Printf("Validated %s: %s", m.ID, validationSummary(m.Validation))

	// 3. Enrich
	e := enricher.New(p.provider, enricher.Options{
		SkipLocation: p.opts.SkipLocation,
		SkipMusic:    p.opts.SkipMusic,
		CorpusPath:   p.opts.CorpusPath,
	})

	p.logger.Printf("Enriching %s...", m.ID)
	if err := e.Enrich(ctx, m); err != nil {
		result.Error = fmt.Errorf("enrich: %w", err)
		result.FinalState = m.State
		return result
	}
	if err := m.Write(manifestPath); err != nil {
		result.Error = fmt.Errorf("write after enrich: %w", err)
		return result
	}
	p.logger.Printf("Enriched %s → %s", m.ID, m.State)

	result.FinalState = m.State
	return result
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
