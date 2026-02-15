package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"photography-publishing-workflow/internal/authn"
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

// defaultDestinations chooses destinations from env.
// Priority:
// 1) PUBLISH_DESTINATIONS if set
// 2) legacy booleans SYNDICATE_FACEBOOK / SYNDICATE_THREADS
// 3) instagram only
func defaultDestinations() string {
	if v := strings.TrimSpace(os.Getenv("PUBLISH_DESTINATIONS")); v != "" {
		return v
	}

	dests := []string{"instagram"}
	if envBool("SYNDICATE_FACEBOOK") {
		dests = append(dests, "facebook")
	}
	if envBool("SYNDICATE_THREADS") {
		dests = append(dests, "threads")
	}
	return strings.Join(dests, ",")
}

func envBool(name string) bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(name)))
	switch v {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}

func buildSyndicationOptions(
	ctx context.Context,
	authMgr *authn.Manager,
	enableFacebook bool,
	enableThreads bool,
	strict bool,
	dryRun bool,
) (publisher.Options, error) {
	opts := publisher.Options{
		DryRun:            dryRun,
		EnableFacebook:    enableFacebook,
		EnableThreads:     enableThreads,
		StrictSyndication: strict,
	}

	if enableFacebook {
		pageID := strings.TrimSpace(os.Getenv("META_PAGE_ID"))
		if pageID == "" {
			return opts, fmt.Errorf("facebook syndication requires META_PAGE_ID")
		}
		legacyIGToken := strings.TrimSpace(os.Getenv("INSTAGRAM_ACCESS_TOKEN"))

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
		threadsID := strings.TrimSpace(os.Getenv("THREADS_USER_ID"))
		legacyThreadsToken := strings.TrimSpace(os.Getenv("THREADS_ACCESS_TOKEN"))
		if threadsID == "" && authMgr != nil {
			status, err := authMgr.Status()
			if err == nil {
				threadsID = strings.TrimSpace(status.Threads.UserID)
			}
		}
		if threadsID == "" {
			return opts, fmt.Errorf("threads syndication requires THREADS_USER_ID (or run `ppw auth login`)")
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
				return opts, fmt.Errorf("threads syndication requires THREADS_ACCESS_TOKEN (or run `ppw auth login`)")
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
