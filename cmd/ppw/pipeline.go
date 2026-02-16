package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"photography-publishing-workflow/internal/pipeline"
)

func pipelineCmd() *cobra.Command {
	var (
		dir          string
		corpusPath   string
		skipLocation bool
		dryRun       bool
		timeout      time.Duration
	)

	cmd := &cobra.Command{
		Use:   "pipeline",
		Short: "Run scan → validate → enrich in one step",
		Long: `Chains the scan, validate, and enrich stages for a post directory.
The directory should contain JPEG images exported from Lightroom.

After running, the manifest will be in pending_review state, ready for ppw review.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if dir == "" {
				return fmt.Errorf("--dir is required")
			}

			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			if corpusPath == "" {
				corpusPath = cfg.AI.CorpusPath
			}
			provider := providerForRun(cfg, dryRun)

			session, err := openCommandLogSession("pipeline", cfg, os.Stderr)
			if err != nil {
				return err
			}
			success := false
			defer func() { _ = session.Close(success) }()
			sweepLogsNow(cfg, session.Writer)
			writeLogLine(session.Writer, "[pipeline] Log stream: %s", session.RuntimePath)
			writeLogLine(session.Writer, "[pipeline] Job log: %s", session.JobPath)

			p := pipeline.New(provider, pipeline.Options{
				CorpusPath:   corpusPath,
				SkipLocation: skipLocation,
				DryRun:       dryRun,
				LogOutput:    session.Writer,
			})

			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()

			result := p.Run(ctx, dir)
			if result.Error != nil {
				return fmt.Errorf("pipeline failed for %s: %w", result.PostID, result.Error)
			}

			fmt.Printf("Pipeline complete: %s → %s\n", result.PostID, result.FinalState)
			if result.ManifestPath != "" {
				fmt.Printf("Manifest: %s\n", result.ManifestPath)
			}

			success = true
			return nil
		},
	}

	cmd.Flags().StringVar(&dir, "dir", "", "Post directory containing JPEG images (required)")
	cmd.Flags().StringVar(&corpusPath, "corpus", "", "Path to style corpus JSON")
	cmd.Flags().BoolVar(&skipLocation, "skip-location", false, "Skip location identification")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print what would be done without executing")
	cmd.Flags().DurationVar(&timeout, "timeout", 5*time.Minute, "Overall timeout")

	return cmd
}
