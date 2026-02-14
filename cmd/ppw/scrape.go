package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"photography-publishing-workflow/internal/scraper"
)

func scrapeCmd() *cobra.Command {
	var (
		handle  string
		userID  string
		count   int
		outPath string
		refresh bool
		dryRun  bool
	)

	cmd := &cobra.Command{
		Use:   "scrape",
		Short: "Scrape past Instagram captions to build a style corpus",
		Long:  "Extracts captions from the user's Instagram posts via the Graph API. One-time setup tool for style-matched caption generation.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if handle == "" && userID == "" {
				return fmt.Errorf("--handle or --user-id is required")
			}

			accessToken := os.Getenv("INSTAGRAM_ACCESS_TOKEN")
			if accessToken == "" {
				return fmt.Errorf("INSTAGRAM_ACCESS_TOKEN environment variable is required")
			}

			if outPath == "" {
				outPath = "config/style_corpus.json"
			}

			// Check if output already exists
			if !refresh {
				if _, err := os.Stat(outPath); err == nil {
					return fmt.Errorf("corpus file already exists at %s — use --refresh to overwrite", outPath)
				}
			}

			if dryRun {
				fmt.Println("DRY RUN: would scrape Instagram captions")
				fmt.Printf("  Handle:  %s\n", handle)
				fmt.Printf("  User ID: %s\n", userID)
				fmt.Printf("  Count:   %d\n", count)
				fmt.Printf("  Output:  %s\n", outPath)
				fmt.Printf("  Refresh: %v\n", refresh)
				fmt.Println("\n  API calls that would be made:")
				fmt.Println("    1. GET /me (resolve user)")
				fmt.Printf("    2. GET /{user-id}/media (paginated, up to %d posts)\n", count)
				return nil
			}

			cfg := scraper.Config{
				AccessToken: accessToken,
				UserID:      userID,
				Count:       count,
				OutputPath:  outPath,
			}

			fmt.Printf("Scraping up to %d captions...\n", count)
			corpus, err := scraper.Scrape(cfg)
			if err != nil {
				return fmt.Errorf("scrape: %w", err)
			}

			if err := corpus.Write(outPath); err != nil {
				return fmt.Errorf("write corpus: %w", err)
			}

			fmt.Printf("Wrote: %s (%d captions from @%s)\n", outPath, corpus.PostCount, corpus.Handle)
			if corpus.StyleSummary != nil {
				fmt.Printf("  Avg caption length: %d chars\n", corpus.StyleSummary.AvgCaptionLength)
				fmt.Printf("  Avg hashtags/post:  %.1f\n", corpus.StyleSummary.AvgHashtagCount)
				if len(corpus.StyleSummary.CommonHashtags) > 5 {
					fmt.Printf("  Top hashtags:       %v\n", corpus.StyleSummary.CommonHashtags[:5])
				}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&handle, "handle", "", "Instagram handle (e.g., @username)")
	cmd.Flags().StringVar(&userID, "user-id", "", "Instagram user ID (alternative to --handle)")
	cmd.Flags().IntVar(&count, "count", 200, "Number of posts to scrape")
	cmd.Flags().StringVar(&outPath, "out", "", "Output corpus file path (default: config/style_corpus.json)")
	cmd.Flags().BoolVar(&refresh, "refresh", false, "Overwrite existing corpus file")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be done without making API calls")

	return cmd
}
