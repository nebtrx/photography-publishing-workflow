package main

import (
	"os"
	"strings"

	"photography-publishing-workflow/internal/authn"
)

func buildAuthManager() (*authn.Manager, error) {
	appID := normalizeEnvSecretLike(os.Getenv("META_APP_ID"))
	appSecret := normalizeEnvSecretLike(os.Getenv("META_APP_SECRET"))
	pageID := normalizeEnvSecretLike(os.Getenv("META_PAGE_ID"))

	// Not configured: caller can fall back to legacy token mode.
	if appID == "" || appSecret == "" {
		return nil, nil
	}

	return authn.NewManager(authn.Config{
		AppID:            appID,
		AppSecret:        appSecret,
		ThreadsAppID:     normalizeEnvSecretLike(os.Getenv("THREADS_APP_ID")),
		ThreadsAppSecret: normalizeEnvSecretLike(os.Getenv("THREADS_APP_SECRET")),
		PageID:           pageID,
	}, strings.TrimSpace(os.Getenv("PPW_TOKEN_STORE")))
}

func normalizeEnvSecretLike(v string) string {
	s := strings.TrimSpace(v)
	s = strings.Trim(s, `"'`)
	return strings.TrimSpace(s)
}
