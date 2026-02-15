package main

import (
	"photography-publishing-workflow/internal/authn"
	"photography-publishing-workflow/internal/config"
)

func buildAuthManager(cfg *config.Config) (*authn.Manager, error) {
	if cfg == nil {
		return nil, nil
	}
	appID := normalizeConfigSecretLike(cfg.Meta.AppID)
	appSecret := normalizeConfigSecretLike(cfg.Meta.AppSecret)
	pageID := normalizeConfigSecretLike(cfg.Meta.PageID)

	// Not configured: caller can fall back to legacy token mode.
	if appID == "" || appSecret == "" {
		return nil, nil
	}

	return authn.NewManager(authn.Config{
		AppID:            appID,
		AppSecret:        appSecret,
		ThreadsAppID:     normalizeConfigSecretLike(cfg.Threads.AppID),
		ThreadsAppSecret: normalizeConfigSecretLike(cfg.Threads.AppSecret),
		PageID:           pageID,
	}, normalizeConfigSecretLike(cfg.Auth.TokenStore))
}

func normalizeConfigSecretLike(v string) string {
	return config.NormalizeSecretLike(v)
}
