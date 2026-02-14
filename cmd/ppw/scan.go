package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"photography-publishing-workflow/internal/manifest"
	"photography-publishing-workflow/internal/scanner"
)

func scanCmd() *cobra.Command {
	var (
		dir    string
		out    string
		dryRun bool
	)

	cmd := &cobra.Command{
		Use:   "scan",
		Short: "Scan a post directory and create a manifest",
		Long:  "Reads JPEG images, extracts EXIF metadata, determines ordering, computes aspect ratios, and writes manifest.json.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if dir == "" {
				return fmt.Errorf("--dir is required")
			}

			m, err := scanner.Scan(dir)
			if err != nil {
				return fmt.Errorf("scan failed: %w", err)
			}

			outPath := out
			if outPath == "" {
				outPath = manifest.ManifestPath(dir)
			}

			if dryRun {
				fmt.Printf("DRY RUN: would write %s\n", outPath)
				fmt.Printf("  Post ID:    %s\n", m.ID)
				fmt.Printf("  Images:     %d\n", len(m.Images))
				for _, img := range m.Images {
					hero := ""
					if img.IsHero {
						hero = " [hero]"
					}
					fmt.Printf("    #%d %s (%dx%d, %s)%s\n", img.Order, img.Filename, img.Width, img.Height, img.AspectRatio, hero)
				}
				return nil
			}

			if err := m.Write(outPath); err != nil {
				return fmt.Errorf("write manifest: %w", err)
			}

			fmt.Printf("Wrote: %s (%d images, post: %s)\n", outPath, len(m.Images), m.ID)
			return nil
		},
	}

	cmd.Flags().StringVar(&dir, "dir", "", "Post directory containing JPEG images (required)")
	cmd.Flags().StringVar(&out, "out", "", "Output manifest path (default: <dir>/manifest.json)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print what would be written without creating files")

	return cmd
}
