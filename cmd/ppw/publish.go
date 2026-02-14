package main

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"photography-publishing-workflow/internal/hosting"
	"photography-publishing-workflow/internal/manifest"
	"photography-publishing-workflow/internal/meta"
	"photography-publishing-workflow/internal/publisher"
)

func publishCmd() *cobra.Command {
	var (
		manifestPath string
		dryRun       bool
		timeout      time.Duration
		destinations string
		strictSyn    bool
	)

	cmd := &cobra.Command{
		Use:   "publish",
		Short: "Publish an approved post to Instagram",
		Long: `Upload images to Cloudflare R2, create Instagram media containers,
publish the post (and optionally story), and record the result.

Requires environment variables:
  INSTAGRAM_USER_ID, INSTAGRAM_ACCESS_TOKEN
  R2_ACCESS_KEY_ID, R2_SECRET_ACCESS_KEY, R2_BUCKET, R2_ENDPOINT, R2_PUBLIC_URL

Optional (for managed auth with Page token):
  META_APP_ID, META_APP_SECRET, META_PAGE_ID

Optional (for syndication):
  PUBLISH_DESTINATIONS (instagram,facebook,threads)
  STRICT_SYNDICATION
  THREADS_USER_ID, THREADS_ACCESS_TOKEN`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if manifestPath == "" {
				return fmt.Errorf("--manifest is required")
			}

			m, err := manifest.Read(manifestPath)
			if err != nil {
				return fmt.Errorf("read manifest: %w", err)
			}

			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()

			// Set up hosting
			var host hosting.Host
			if dryRun {
				host = &hosting.DryRunHost{}
			} else {
				r2Cfg, err := hosting.R2ConfigFromEnv()
				if err != nil {
					return fmt.Errorf("R2 config: %w", err)
				}
				r2, err := hosting.NewR2(ctx, r2Cfg)
				if err != nil {
					return fmt.Errorf("create R2 client: %w", err)
				}
				host = r2
			}

			// Set up Instagram client
			var ig publisher.InstagramAPI
			var tm *meta.TokenManager
			if dryRun {
				ig = &dryRunInstagram{}
			} else {
				igClient, tokenManager := buildInstagramClientWithMeta()
				if igClient == nil {
					return fmt.Errorf("Instagram client: INSTAGRAM_USER_ID and INSTAGRAM_ACCESS_TOKEN are required")
				}
				ig = igClient
				tm = tokenManager
			}

			enableFacebook, enableThreads, err := parseDestinations(destinations)
			if err != nil {
				return err
			}

			opts, err := buildSyndicationOptions(ctx, tm, enableFacebook, enableThreads, strictSyn, dryRun)
			if err != nil {
				return fmt.Errorf("syndication options: %w", err)
			}
			opts.DryRun = dryRun

			pub := publisher.New(host, ig, opts)

			if dryRun {
				fmt.Println("[DRY RUN] Simulating publish flow...")
			}

			if err := pub.Publish(ctx, m, manifestPath); err != nil {
				return fmt.Errorf("publish: %w", err)
			}

			if dryRun {
				fmt.Println("[DRY RUN] Publish simulation complete.")
			} else {
				fmt.Printf("Published: %s\n", m.Publishing.Permalink)
				if m.Publishing.InstagramStoryID != "" {
					fmt.Println("Story also published.")
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&manifestPath, "manifest", "", "Path to the manifest.json to publish")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Log the full API call sequence without executing")
	cmd.Flags().DurationVar(&timeout, "timeout", 10*time.Minute, "Overall timeout for the publish operation")
	cmd.Flags().StringVar(&destinations, "destinations", defaultDestinations(), "Comma-separated destinations: instagram,facebook,threads")
	cmd.Flags().BoolVar(&strictSyn, "strict-syndication", envBool("STRICT_SYNDICATION"), "Fail the run if any enabled syndication destination fails")

	return cmd
}

// dryRunInstagram is a no-op Instagram client for dry-run mode.
type dryRunInstagram struct{}

func (d *dryRunInstagram) VerifyToken() (string, error) {
	return "dry_run_user", nil
}

func (d *dryRunInstagram) CreateChildContainer(imageURL string) (string, error) {
	return "dry_run_child_" + imageURL, nil
}

func (d *dryRunInstagram) CreateSingleContainer(imageURL, caption, locationID string) (string, error) {
	return "dry_run_single", nil
}

func (d *dryRunInstagram) CreateCarouselContainer(childIDs []string, caption, locationID string) (string, error) {
	return "dry_run_carousel", nil
}

func (d *dryRunInstagram) CreateStoryContainer(imageURL string) (string, error) {
	return "dry_run_story", nil
}

func (d *dryRunInstagram) PollContainer(containerID string, pollInterval time.Duration, maxPolls int) error {
	return nil
}

func (d *dryRunInstagram) Publish(containerID string) (string, error) {
	return "dry_run_media_id", nil
}

func (d *dryRunInstagram) GetPermalink(mediaID string) (string, error) {
	return "https://www.instagram.com/p/DRY_RUN/", nil
}

func (d *dryRunInstagram) SearchLocation(query string) (string, string, error) {
	return "dry_run_loc_id", query, nil
}
