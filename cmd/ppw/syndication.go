package main

import (
	"context"
	"fmt"
	"strings"

	"photography-publishing-workflow/internal/authn"
	"photography-publishing-workflow/internal/config"
	"photography-publishing-workflow/internal/publisher"
)

// parseDestinations parses a comma-separated destination list.
// Supported: instagram, facebook, threads.
func parseDestinations(raw string) (facebook bool, threads bool, err error) {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return false, false, nil
	}

	parts := strings.Split(raw, ",")
	for _, part := range parts {
		dest := strings.TrimSpace(part)
		switch dest {
		case "", "instagram":
			// baseline destination; no option flag needed here
		case "facebook":
			facebook = true
		case "threads":
			threads = true
		default:
			return false, false, fmt.Errorf("unsupported destination %q (allowed: instagram,facebook,threads)", dest)
		}
	}
	return facebook, threads, nil
}

// defaultDestinations chooses destinations from config.
func defaultDestinations(cfg *config.Config) string {
	if cfg == nil || len(cfg.Publishing.Destinations) == 0 {
		return "instagram"
	}
	parts := make([]string, 0, len(cfg.Publishing.Destinations))
	for _, v := range cfg.Publishing.Destinations {
		p := strings.TrimSpace(strings.ToLower(v))
		if p != "" {
			parts = append(parts, p)
		}
	}
	if len(parts) == 0 {
		return "instagram"
	}
	return strings.Join(parts, ",")
}

func buildSyndicationOptions(
	ctx context.Context,
	cfg *config.Config,
	authMgr *authn.Manager,
	enableFacebook bool,
	enableThreads bool,
	strict bool,
	dryRun bool,
) (publisher.Options, error) {
	if cfg == nil {
		cfg = &config.Config{}
	}
	opts := publisher.Options{
		DryRun:            dryRun,
		EnableFacebook:    enableFacebook,
		EnableThreads:     enableThreads,
		StrictSyndication: strict,
	}

	if enableFacebook {
		pageID := strings.TrimSpace(cfg.Meta.PageID)
		if pageID == "" {
			return opts, fmt.Errorf("facebook syndication requires meta.page_id")
		}
		legacyIGToken := config.NormalizeSecretLike(cfg.Meta.LegacyAccessToken)

		var tokenFn publisher.AccessTokenSource
		if authMgr != nil {
			tokenFn = func(ctx context.Context) (string, error) {
				token, err := authMgr.PageAccessToken(ctx)
				if err == nil && strings.TrimSpace(token) != "" {
					return token, nil
				}
				if legacyIGToken != "" {
					return legacyIGToken, nil
				}
				return "", err
			}
		} else {
			if legacyIGToken == "" {
				return opts, fmt.Errorf("facebook syndication requires a usable token source")
			}
			tokenFn = func(context.Context) (string, error) { return legacyIGToken, nil }
		}

		fb, err := publisher.NewFacebookLinkClient(pageID, tokenFn)
		if err != nil {
			return opts, err
		}
		opts.Facebook = fb
	}

	if enableThreads {
		threadsID := strings.TrimSpace(cfg.Threads.UserID)
		legacyThreadsToken := config.NormalizeSecretLike(cfg.Threads.LegacyAccessToken)
		if threadsID == "" && authMgr != nil {
			status, err := authMgr.Status()
			if err == nil {
				threadsID = strings.TrimSpace(status.Threads.UserID)
			}
		}
		if threadsID == "" {
			return opts, fmt.Errorf("threads syndication requires threads.user_id (or run `ppw auth login`)")
		}

		var tokenFn publisher.AccessTokenSource
		if authMgr != nil {
			tokenFn = func(ctx context.Context) (string, error) {
				token, err := authMgr.ThreadsAccessToken(ctx)
				if err == nil && strings.TrimSpace(token) != "" {
					return token, nil
				}
				if legacyThreadsToken != "" {
					return legacyThreadsToken, nil
				}
				return "", err
			}
		} else {
			if legacyThreadsToken == "" {
				return opts, fmt.Errorf("threads syndication requires threads.legacy_access_token (or run `ppw auth login`)")
			}
			tokenFn = func(context.Context) (string, error) { return legacyThreadsToken, nil }
		}

		th, err := publisher.NewThreadsClient(threadsID, tokenFn)
		if err != nil {
			return opts, err
		}
		opts.Threads = th
	}

	// Best effort preflight: verify facebook token can be derived now when using managed auth.
	// This surfaces auth issues early in CLI mode.
	if enableFacebook && authMgr != nil && !dryRun {
		if _, err := authMgr.PageAccessToken(ctx); err != nil {
			return opts, fmt.Errorf("facebook page token preflight failed: %w", err)
		}
	}

	return opts, nil
}
