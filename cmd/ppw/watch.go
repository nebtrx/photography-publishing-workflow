package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"photography-publishing-workflow/internal/pipeline"
	"photography-publishing-workflow/internal/watcher"
)

func watchCmd() *cobra.Command {
	var (
		dir          string
		corpusPath   string
		skipLocation bool
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
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			if dir == "" {
				dir = cfg.Watch.Dir
			}
			if dir == "" {
				return fmt.Errorf("--dir is required (or set watch.dir in config)")
			}
			if corpusPath == "" {
				corpusPath = cfg.AI.CorpusPath
			}
			provider := providerForRun(cfg, false)
			session, err := openCommandLogSession("watch", cfg, os.Stderr)
			if err != nil {
				return err
			}
			success := false
			defer func() { _ = session.Close(success) }()
			writeLogLine(session.Writer, "[watcher] Log stream: %s", session.RuntimePath)
			writeLogLine(session.Writer, "[watcher] Job log: %s", session.JobPath)

			sweepCtx, stopSweep := context.WithCancel(context.Background())
			defer stopSweep()
			go startPeriodicLogSweep(sweepCtx, cfg, session.Writer)
			sweepLogsNow(cfg, session.Writer)

			p := pipeline.New(provider, pipeline.Options{
				CorpusPath:   corpusPath,
				SkipLocation: skipLocation,
				LogOutput:    session.Writer,
			})

			handler := func(ctx context.Context, postDir string) {
				result := p.Run(ctx, postDir)
				if result.Error != nil {
					writeLogLine(session.Writer, "[pipeline] ERROR %s: %v", result.PostID, result.Error)
				} else {
					writeLogLine(session.Writer, "[pipeline] complete: %s -> %s", result.PostID, result.FinalState)
				}
			}

			w, err := watcher.New(dir, handler, watcher.Options{
				Debounce:  debounce,
				LogOutput: session.Writer,
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
			err = w.Watch(ctx)
			success = err == nil
			return err
		},
	}

	cmd.Flags().StringVar(&dir, "dir", "", "Directory to watch for new post subdirectories (required)")
	cmd.Flags().StringVar(&corpusPath, "corpus", "", "Path to style corpus JSON")
	cmd.Flags().BoolVar(&skipLocation, "skip-location", false, "Skip location identification")
	cmd.Flags().DurationVar(&debounce, "debounce", 2*time.Second, "Wait time before processing a new directory")

	return cmd
}
