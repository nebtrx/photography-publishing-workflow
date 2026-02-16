package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"photography-publishing-workflow/internal/authn"
	"photography-publishing-workflow/internal/hosting"
	"photography-publishing-workflow/internal/manifest"
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

Configuration is read from ppw.toml:
  [meta] instagram_user_id, app_id, app_secret, page_id
  [r2] access_key_id, secret_access_key, bucket, endpoint, public_url
  [publishing] destinations, strict_syndication
  [threads] user_id (for threads syndication)

Managed auth flow:
  ppw auth login
Tokens are stored in the configured auth token store.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if manifestPath == "" {
				return fmt.Errorf("--manifest is required")
			}
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			if !cmd.Flags().Lookup("destinations").Changed {
				destinations = defaultDestinations(cfg)
			}
			if !cmd.Flags().Lookup("strict-syndication").Changed {
				strictSyn = cfg.Publishing.StrictSyndication
			}

			m, err := manifest.Read(manifestPath)
			if err != nil {
				return fmt.Errorf("read manifest: %w", err)
			}

			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()

			session, err := openCommandLogSession("publish", cfg, os.Stderr)
			if err != nil {
				return err
			}
			success := false
			defer func() { _ = session.Close(success) }()
			sweepLogsNow(cfg, session.Writer)
			writeLogLine(session.Writer, "[publish] Log stream: %s", session.RuntimePath)
			writeLogLine(session.Writer, "[publish] Job log: %s", session.JobPath)

			// Set up hosting
			var host hosting.Host
			if dryRun {
				host = &hosting.DryRunHost{}
			} else {
				r2Cfg, err := hosting.R2ConfigFromValues(
					cfg.R2.AccessKeyID,
					cfg.R2.SecretAccessKey,
					cfg.R2.Bucket,
					cfg.R2.Endpoint,
					cfg.R2.PublicURL,
				)
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
			var authMgr *authn.Manager
			if dryRun {
				ig = &dryRunInstagram{}
			} else {
				igClient, tokenManager := buildInstagramClientWithMeta(cfg, session.Writer)
				if igClient == nil {
					return fmt.Errorf("Instagram client: set meta.instagram_user_id and run `ppw auth login` (or provide meta.legacy_access_token)")
				}
				ig = igClient
				authMgr = tokenManager
			}

			enableFacebook, enableThreads, err := parseDestinations(destinations)
			if err != nil {
				return err
			}

			opts, err := buildSyndicationOptions(ctx, cfg, authMgr, enableFacebook, enableThreads, strictSyn, dryRun)
			if err != nil {
				return fmt.Errorf("syndication options: %w", err)
			}
			opts.DryRun = dryRun
			opts.LogOutput = session.Writer
			opts.CleanupOnFailure = cfg.Publishing.CleanupOnFailure == "always"

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
				printSyndicationSummary(os.Stdout, m)
			}

			success = true
			return nil
		},
	}

	cmd.Flags().StringVar(&manifestPath, "manifest", "", "Path to the manifest.json to publish")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Log the full API call sequence without executing")
	cmd.Flags().DurationVar(&timeout, "timeout", 10*time.Minute, "Overall timeout for the publish operation")
	cmd.Flags().StringVar(&destinations, "destinations", "instagram", "Comma-separated destinations: instagram,facebook,threads")
	cmd.Flags().BoolVar(&strictSyn, "strict-syndication", false, "Fail the run if any enabled syndication destination fails")

	return cmd
}

func printSyndicationSummary(out io.Writer, m *manifest.Manifest) {
	if m == nil || m.Publishing == nil || m.Publishing.Syndication == nil {
		return
	}

	printTarget := func(name string, t *manifest.SyndicationTarget) {
		if t == nil || !t.Enabled {
			return
		}
		status := t.Status
		if status == "" {
			status = "unknown"
		}
		line := fmt.Sprintf("%s: %s", name, status)
		if t.Error != "" {
			line += " (" + t.Error + ")"
		} else if t.Permalink != "" {
			line += " (" + t.Permalink + ")"
		} else if t.PostID != "" {
			line += " (post_id=" + t.PostID + ")"
		}
		fmt.Fprintf(out, "Syndication %s\n", line)
	}

	printTarget("facebook", m.Publishing.Syndication.Facebook)
	printTarget("threads", m.Publishing.Syndication.Threads)
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
