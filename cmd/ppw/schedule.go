package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"photography-publishing-workflow/internal/manifest"
	"photography-publishing-workflow/internal/scheduler"
)

func scheduleCmd() *cobra.Command {
	var (
		manifestPath string
		configPath   string
		queuePath    string
		list         bool
		next         bool
		dryRun       bool
	)

	cmd := &cobra.Command{
		Use:   "schedule",
		Short: "Manage the posting schedule queue",
		Long: `Assign approved posts to schedule slots, view the queue, or check the next available slot.

Examples:
  ppw schedule --manifest post/manifest.json --config config/schedule.json --queue state/schedule_queue.json
  ppw schedule --list --config config/schedule.json --queue state/schedule_queue.json
  ppw schedule --next --config config/schedule.json --queue state/schedule_queue.json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if configPath == "" {
				return fmt.Errorf("--config is required")
			}
			if queuePath == "" {
				return fmt.Errorf("--queue is required")
			}

			s, err := scheduler.New(scheduler.Options{
				ConfigPath: configPath,
				QueuePath:  queuePath,
				DryRun:     dryRun,
			})
			if err != nil {
				return fmt.Errorf("create scheduler: %w", err)
			}

			if list {
				return showList(s)
			}
			if next {
				return showNext(s)
			}

			// Schedule a post
			if manifestPath == "" {
				return fmt.Errorf("--manifest, --list, or --next is required")
			}

			m, err := manifest.Read(manifestPath)
			if err != nil {
				return fmt.Errorf("read manifest: %w", err)
			}

			entry, err := s.Schedule(m, manifestPath)
			if err != nil {
				return fmt.Errorf("schedule: %w", err)
			}

			if dryRun {
				fmt.Printf("[DRY RUN] Would schedule %s for %s (slot: %s)\n",
					entry.PostID, entry.ScheduledFor.Format("2006-01-02 15:04 MST"), entry.SlotID)
			} else {
				fmt.Printf("Scheduled %s for %s (slot: %s)\n",
					entry.PostID, entry.ScheduledFor.Format("2006-01-02 15:04 MST"), entry.SlotID)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&manifestPath, "manifest", "", "Path to manifest.json to schedule")
	cmd.Flags().StringVar(&configPath, "config", "", "Path to schedule config JSON")
	cmd.Flags().StringVar(&queuePath, "queue", "", "Path to schedule_queue.json")
	cmd.Flags().BoolVar(&list, "list", false, "Show all currently scheduled posts")
	cmd.Flags().BoolVar(&next, "next", false, "Show the next available slot")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would happen without modifying anything")

	return cmd
}

func showList(s *scheduler.Scheduler) error {
	entries, err := s.ListQueue()
	if err != nil {
		return err
	}

	if len(entries) == 0 {
		fmt.Println("No posts currently scheduled.")
		return nil
	}

	fmt.Printf("Scheduled posts (%d):\n\n", len(entries))
	for _, e := range entries {
		fmt.Printf("  %s  %-30s  slot: %s  batch: %s\n",
			e.ScheduledFor.Format("2006-01-02 15:04 MST"),
			e.PostID, e.SlotID, e.BatchID)
	}
	return nil
}

func showNext(s *scheduler.Scheduler) error {
	slotTime, slotID, openings, err := s.NextSlot()
	if err != nil {
		return err
	}

	fmt.Printf("Next available slot: %s\n", slotTime.Format("2006-01-02 15:04 MST"))
	fmt.Printf("Slot: %s\n", slotID)
	fmt.Printf("Openings: %d\n", openings)
	return nil
}
