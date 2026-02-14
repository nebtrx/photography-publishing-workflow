package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"photography-publishing-workflow/internal/manifest"
	"photography-publishing-workflow/internal/validator"
)

func validateCmd() *cobra.Command {
	var (
		manifestPath string
		dryRun       bool
	)

	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate a scanned manifest",
		Long:  "Checks aspect ratio consistency, image count, ordering integrity, and hero image presence.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if manifestPath == "" {
				return fmt.Errorf("--manifest is required")
			}

			m, err := manifest.Read(manifestPath)
			if err != nil {
				return fmt.Errorf("read manifest: %w", err)
			}

			if dryRun {
				v := validator.Validate(m)
				printValidation(v)
				fmt.Println("\nDRY RUN: manifest not modified.")
				return nil
			}

			if err := validator.Apply(m); err != nil {
				return fmt.Errorf("validation: %w", err)
			}

			printValidation(m.Validation)

			if err := m.Write(manifestPath); err != nil {
				return fmt.Errorf("write manifest: %w", err)
			}

			fmt.Printf("\nWrote: %s (state: %s)\n", manifestPath, m.State)
			return nil
		},
	}

	cmd.Flags().StringVar(&manifestPath, "manifest", "", "Path to manifest.json (required)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print validation results without modifying the manifest")

	return cmd
}

func printValidation(v *manifest.Validation) {
	if v.Passed {
		fmt.Println("Validation: PASSED")
	} else {
		fmt.Println("Validation: FAILED")
	}

	fmt.Printf("  Images:       %d (valid: %v)\n", v.ImageCount, v.ImageCountValid)
	fmt.Printf("  Aspect ratio: %s (consistent: %v)\n", v.ResolvedAspectRatio, v.AspectRatioConsistent)

	if len(v.Issues) > 0 {
		fmt.Println("  Issues:")
		for _, issue := range v.Issues {
			fmt.Printf("    [%s] %s\n", issue.Severity, issue.Message)
		}
	}
}
