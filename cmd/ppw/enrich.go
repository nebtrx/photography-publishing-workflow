package main

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"photography-publishing-workflow/internal/enricher"
	"photography-publishing-workflow/internal/manifest"
)

func enrichCmd() *cobra.Command {
	var (
		manifestPath string
		corpusPath   string
		skipLocation bool
		skipMusic    bool
		dryRun       bool
		timeout      time.Duration
	)

	cmd := &cobra.Command{
		Use:   "enrich",
		Short: "AI-enrich a validated manifest with caption, location, and music",
		Long:  "Generates caption (with inline hashtags), identifies location, and suggests music using AI.\nProvider is configured in ppw.toml under [ai].",
		RunE: func(cmd *cobra.Command, args []string) error {
			if manifestPath == "" {
				return fmt.Errorf("--manifest is required")
			}
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			if corpusPath == "" {
				corpusPath = cfg.AI.CorpusPath
			}

			m, err := manifest.Read(manifestPath)
			if err != nil {
				return fmt.Errorf("read manifest: %w", err)
			}

			if dryRun {
				fmt.Println("DRY RUN: enrichment would be performed on:", manifestPath)
				fmt.Printf("  Post:          %s\n", m.ID)
				fmt.Printf("  Images:        %d\n", len(m.Images))
				fmt.Printf("  Corpus:        %s\n", valueOrDefault(corpusPath, "(none)"))
				fmt.Printf("  Skip location: %v\n", skipLocation)
				fmt.Printf("  Skip music:    %v\n", skipMusic)
				fmt.Println("\n  AI calls that would be made:")
				fmt.Println("    1. Caption generation (with hero image + style corpus)")
				if !skipLocation {
					fmt.Println("    2. Location identification (with hero image)")
				}
				if !skipMusic {
					fmt.Println("    3. Music suggestion (with hero image)")
				}
				return nil
			}

			// Set up AI provider
			provider := buildAIProvider(cfg)

			e := enricher.New(provider, enricher.Options{
				SkipLocation: skipLocation,
				SkipMusic:    skipMusic,
				CorpusPath:   corpusPath,
			})

			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()

			fmt.Printf("Enriching %s...\n", m.ID)
			if err := e.Enrich(ctx, m); err != nil {
				return fmt.Errorf("enrichment: %w", err)
			}

			// Print results summary
			printEnrichmentSummary(m)

			if err := m.Write(manifestPath); err != nil {
				return fmt.Errorf("write manifest: %w", err)
			}

			fmt.Printf("\nWrote: %s (state: %s)\n", manifestPath, m.State)
			return nil
		},
	}

	cmd.Flags().StringVar(&manifestPath, "manifest", "", "Path to manifest.json (required)")
	cmd.Flags().StringVar(&corpusPath, "corpus", "", "Path to style corpus JSON (default: config/style_corpus.json)")
	cmd.Flags().BoolVar(&skipLocation, "skip-location", false, "Skip location identification")
	cmd.Flags().BoolVar(&skipMusic, "skip-music", false, "Skip music suggestion")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print what would be done without making AI calls")
	cmd.Flags().DurationVar(&timeout, "timeout", 3*time.Minute, "Overall timeout for all AI calls")

	return cmd
}

func printEnrichmentSummary(m *manifest.Manifest) {
	if m.Enrichment == nil {
		fmt.Println("  No enrichment data generated.")
		return
	}

	if c := m.Enrichment.Caption; c != nil {
		preview := c.Text
		if len(preview) > 80 {
			preview = preview[:80] + "..."
		}
		fmt.Printf("  Caption:  %s\n", preview)
		fmt.Printf("  Hashtags: %v (%d)\n", c.Hashtags, c.HashtagCount)
	} else {
		fmt.Println("  Caption:  (not generated)")
	}

	if l := m.Enrichment.Location; l != nil {
		fmt.Printf("  Location: %s (source: %s)\n", l.Name, l.Source)
	} else {
		fmt.Println("  Location: (none)")
	}

	if ms := m.Enrichment.MusicSuggestion; ms != nil {
		fmt.Printf("  Music:    %s — %s (%s)\n", ms.Artist, ms.Title, ms.Mood)
	} else {
		fmt.Println("  Music:    (none)")
	}
}

func valueOrDefault(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
