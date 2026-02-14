package main

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"photography-publishing-workflow/internal/manifest"
	"photography-publishing-workflow/internal/tui"
)

func reviewCmd() *cobra.Command {
	var (
		manifestPath string
		dir          string
	)

	cmd := &cobra.Command{
		Use:   "review",
		Short: "Review pending posts in a TUI",
		Long:  "Launch a LazyGit-style terminal UI to review, approve, reject, or edit posts before publishing.",
		RunE: func(cmd *cobra.Command, args []string) error {
			var posts []*manifest.Manifest
			var paths []string

			if manifestPath != "" {
				// Single manifest mode
				m, err := manifest.Read(manifestPath)
				if err != nil {
					return fmt.Errorf("read manifest: %w", err)
				}
				if m.State != manifest.StatePendingReview {
					return fmt.Errorf("manifest state is %q, expected %q", m.State, manifest.StatePendingReview)
				}
				posts = []*manifest.Manifest{m}
				paths = []string{manifestPath}
			} else if dir != "" {
				// Directory mode — find all pending_review manifests
				var err error
				posts, paths, err = tui.LoadPendingPosts(dir)
				if err != nil {
					return fmt.Errorf("load posts: %w", err)
				}
			} else {
				return fmt.Errorf("--manifest or --dir is required")
			}

			if len(posts) == 0 {
				fmt.Println("No posts pending review.")
				return nil
			}

			fmt.Printf("Found %d post(s) pending review.\n", len(posts))

			model := tui.New(posts, paths)
			p := tea.NewProgram(model, tea.WithAltScreen())
			if _, err := p.Run(); err != nil {
				return fmt.Errorf("TUI error: %w", err)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&manifestPath, "manifest", "", "Path to a single manifest.json to review")
	cmd.Flags().StringVar(&dir, "dir", "", "Watch directory to scan for pending posts")

	return cmd
}
