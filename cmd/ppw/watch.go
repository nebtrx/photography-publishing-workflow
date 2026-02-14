package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"photography-publishing-workflow/internal/ai"
	"photography-publishing-workflow/internal/pipeline"
	"photography-publishing-workflow/internal/watcher"
)

func watchCmd() *cobra.Command {
	var (
		dir          string
		corpusPath   string
		skipLocation bool
		skipMusic    bool
		debounce     time.Duration
	)

	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Watch a directory for new post directories",
		Long: `Monitors a directory for new subdirectories containing JPEG images.
When a new directory is detected, automatically runs the scan → validate → enrich pipeline.

Posts will end up in pending_review state, ready for ppw review.
Press Ctrl+C to stop watching.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if dir == "" {
				return fmt.Errorf("--dir is required")
			}

			cliPath := os.Getenv("CLAUDE_CLI_PATH")
			provider := ai.NewClaudeCLI(cliPath)

			p := pipeline.New(provider, pipeline.Options{
				CorpusPath:   corpusPath,
				SkipLocation: skipLocation,
				SkipMusic:    skipMusic,
			})

			handler := func(ctx context.Context, postDir string) {
				result := p.Run(ctx, postDir)
				if result.Error != nil {
					fmt.Fprintf(os.Stderr, "Pipeline error for %s: %v\n", result.PostID, result.Error)
				} else {
					fmt.Printf("Pipeline complete: %s → %s\n", result.PostID, result.FinalState)
				}
			}

			w, err := watcher.New(dir, handler, watcher.Options{
				Debounce: debounce,
			})
			if err != nil {
				return fmt.Errorf("create watcher: %w", err)
			}

			// Handle graceful shutdown
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			go func() {
				<-sigCh
				fmt.Println("\nShutting down watcher...")
				cancel()
			}()

			fmt.Printf("Watching %s for new posts (Ctrl+C to stop)\n", dir)
			return w.Watch(ctx)
		},
	}

	cmd.Flags().StringVar(&dir, "dir", "", "Directory to watch for new post subdirectories (required)")
	cmd.Flags().StringVar(&corpusPath, "corpus", "", "Path to style corpus JSON")
	cmd.Flags().BoolVar(&skipLocation, "skip-location", false, "Skip location identification")
	cmd.Flags().BoolVar(&skipMusic, "skip-music", false, "Skip music suggestion")
	cmd.Flags().DurationVar(&debounce, "debounce", 2*time.Second, "Wait time before processing a new directory")

	return cmd
}
