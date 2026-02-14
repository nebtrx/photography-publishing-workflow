package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"photography-publishing-workflow/internal/archiver"
	"photography-publishing-workflow/internal/manifest"
)

func archiveCmd() *cobra.Command {
	var (
		manifestPath string
		archiveDir   string
		logFile      string
		dryRun       bool
	)

	cmd := &cobra.Command{
		Use:   "archive",
		Short: "Archive a published post",
		Long: `Move published post images to a date-organized archive directory,
preserve the final manifest, and append a log entry to the publish log.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if manifestPath == "" {
				return fmt.Errorf("--manifest is required")
			}
			if archiveDir == "" {
				return fmt.Errorf("--archive-dir is required")
			}
			if logFile == "" {
				return fmt.Errorf("--log-file is required")
			}

			m, err := manifest.Read(manifestPath)
			if err != nil {
				return fmt.Errorf("read manifest: %w", err)
			}

			a, err := archiver.New(archiver.Options{
				ArchiveDir: archiveDir,
				LogFile:    logFile,
				DryRun:     dryRun,
			})
			if err != nil {
				return fmt.Errorf("create archiver: %w", err)
			}

			if dryRun {
				fmt.Println("[DRY RUN] Simulating archive...")
			}

			if err := a.Archive(m, manifestPath); err != nil {
				return fmt.Errorf("archive: %w", err)
			}

			if dryRun {
				fmt.Println("[DRY RUN] Archive simulation complete.")
			} else {
				fmt.Printf("Archived: %s → %s\n", m.ID, m.Archival.ArchiveDir)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&manifestPath, "manifest", "", "Path to the manifest.json to archive")
	cmd.Flags().StringVar(&archiveDir, "archive-dir", "", "Root archive directory")
	cmd.Flags().StringVar(&logFile, "log-file", "", "Path to the JSONL publish log")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print what would be done without doing it")

	return cmd
}
