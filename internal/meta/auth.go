// Package meta provides Meta (Facebook) platform auth token management.
//
// Key design decisions (confirmed 2026-02-14 via live API probing):
//   - /me/accounts is intentionally bypassed — it can return {"data":[]} even
//     when publishing is correctly authorized (New Pages Experience / delegated pages).
//   - Page tokens are more reliable for IG publishing. Derived on-demand via
//     GET /{PAGE_ID}?fields=access_token using a valid User token.
//   - This behavior was confirmed empirically via live Graph API probes.
package meta

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	defaultGraphAPIBase = "https://graph.facebook.com/v21.0"
	pageTokenCacheTTL   = 30 * time.Minute
	expiryWarningDays   = 7
)

// Config holds Meta auth configuration, typically from environment variables.
type Config struct {
	AppID        string // META_APP_ID
	AppSecret    string // META_APP_SECRET
	PageID       string // META_PAGE_ID
	UserToken    string // INSTAGRAM_ACCESS_TOKEN (short or long-lived)
	GraphAPIBase string // override for testing; defaults to graph.facebook.com/v21.0
}

// TokenInfo holds the result of /debug_token inspection.
type TokenInfo struct {
	AppID       string    `json:"app_id"`
	Type        string    `json:"type"`
	Application string    `json:"application"`
	ExpiresAt   time.Time `json:"expires_at"`
	IsValid     bool      `json:"is_valid"`
	Scopes      []string  `json:"scopes"`
	UserID      string    `json:"user_id"`
}

// DoctorReport holds the full diagnostic output.
type DoctorReport struct {
	UserID       string   `json:"user_id"`
	UserName     string   `json:"user_name"`
	Permissions  []string `json:"permissions"`
	PageID       string   `json:"page_id"`
	PageName     string   `json:"page_name"`
	IGUserID     string   `json:"ig_user_id"`
	IGUsername   string   `json:"ig_username"`
	TokenDebug   TokenInfo `json:"token_debug"`
	TokenStatus  string   `json:"token_status"`
	DaysToExpiry int      `json:"days_to_expiry"`
	PageTokenOK  bool     `json:"page_token_ok"`
}

// TokenManager handles Meta token lifecycle: exchange, page token derivation, and diagnostics.
type TokenManager struct {
	cfg        Config
	httpClient *http.Client
	apiBase    string

	mu              sync.Mutex
	cachedPageToken string
	pageCacheExpiry time.Time
}

// NewTokenManager creates a TokenManager from the given config.
func NewTokenManager(cfg Config) *TokenManager {
	base := cfg.GraphAPIBase
	if base == "" {
		base = defaultGraphAPIBase
	}
	return &TokenManager{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: 15 * time.Second},
		apiBase:    strings.TrimRight(base, "/"),
	}
}

// EnsureLongLivedUserToken exchanges a short-lived User token for a long-lived one (~60 days).
// The caller is responsible for persisting the returned token.
func (tm *TokenManager) EnsureLongLivedUserToken(ctx context.Context) (token string, expiresAt time.Time, err error) {
	if tm.cfg.AppID == "" || tm.cfg.AppSecret == "" {
		return "", time.Time{}, fmt.Errorf("META_APP_ID and META_APP_SECRET are required for token exchange")
	}
	if tm.cfg.UserToken == "" {
		return "", time.Time{}, fmt.Errorf("INSTAGRAM_ACCESS_TOKEN (user token) is required for exchange")
	}

	u := fmt.Sprintf("%s/oauth/access_token?grant_type=fb_exchange_token&client_id=%s&client_secret=%s&fb_exchange_token=%s",
		tm.apiBase,
		url.QueryEscape(tm.cfg.AppID),
		url.QueryEscape(tm.cfg.AppSecret),
		url.QueryEscape(tm.cfg.UserToken),
	)

	body, err := tm.doGet(ctx, u)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("token exchange: %w", err)
	}

	var result struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int64  `json:"expires_in"` // seconds
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", time.Time{}, fmt.Errorf("parse exchange response: %w", err)
	}
	if result.AccessToken == "" {
		return "", time.Time{}, fmt.Errorf("exchange returned empty access_token")
	}

	expires := time.Now().Add(time.Duration(result.ExpiresIn) * time.Second)
	return result.AccessToken, expires, nil
}

// GetPageAccessToken derives a Page access token from the current User token.
// Results are cached with a conservative TTL (~30 min).
func (tm *TokenManager) GetPageAccessToken(ctx context.Context) (string, error) {
	tm.mu.Lock()
	if tm.cachedPageToken != "" && time.Now().Before(tm.pageCacheExpiry) {
		tok := tm.cachedPageToken
		tm.mu.Unlock()
		return tok, nil
	}
	tm.mu.Unlock()

	if tm.cfg.PageID == "" {
		return "", fmt.Errorf("META_PAGE_ID is required for page token derivation")
	}
	if tm.cfg.UserToken == "" {
		return "", fmt.Errorf("INSTAGRAM_ACCESS_TOKEN (user token) is required")
	}

	u := fmt.Sprintf("%s/%s?fields=access_token&access_token=%s",
		tm.apiBase,
		url.PathEscape(tm.cfg.PageID),
		url.QueryEscape(tm.cfg.UserToken),
	)

	body, err := tm.doGet(ctx, u)
	if err != nil {
		return "", fmt.Errorf("derive page token: %w", err)
	}

	var result struct {
		AccessToken string `json:"access_token"`
		ID          string `json:"id"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parse page token response: %w", err)
	}
	if result.AccessToken == "" {
		return "", fmt.Errorf("page %s returned empty access_token", tm.cfg.PageID)
	}

	tm.mu.Lock()
	tm.cachedPageToken = result.AccessToken
	tm.pageCacheExpiry = time.Now().Add(pageTokenCacheTTL)
	tm.mu.Unlock()

	return result.AccessToken, nil
}

// InvalidatePageTokenCache clears the cached page token, forcing re-derivation
// on the next call to GetPageAccessToken. Called on 401 errors.
func (tm *TokenManager) InvalidatePageTokenCache() {
	tm.mu.Lock()
	tm.cachedPageToken = ""
	tm.pageCacheExpiry = time.Time{}
	tm.mu.Unlock()
}

// DebugToken inspects a token via the /debug_token endpoint.
// Uses app-level access (APP_ID|APP_SECRET) to avoid needing user permissions.
func (tm *TokenManager) DebugToken(ctx context.Context, inputToken string) (TokenInfo, error) {
	if tm.cfg.AppID == "" || tm.cfg.AppSecret == "" {
		return TokenInfo{}, fmt.Errorf("META_APP_ID and META_APP_SECRET are required for debug_token")
	}

	appToken := tm.cfg.AppID + "|" + tm.cfg.AppSecret

	u := fmt.Sprintf("%s/debug_token?input_token=%s&access_token=%s",
		tm.apiBase,
		url.QueryEscape(inputToken),
		url.QueryEscape(appToken),
	)

	body, err := tm.doGet(ctx, u)
	if err != nil {
		return TokenInfo{}, fmt.Errorf("debug_token: %w", err)
	}

	var result struct {
		Data struct {
			AppID       string   `json:"app_id"`
			Type        string   `json:"type"`
			Application string   `json:"application"`
			ExpiresAt   int64    `json:"expires_at"`
			IsValid     bool     `json:"is_valid"`
			Scopes      []string `json:"scopes"`
			UserID      string   `json:"user_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return TokenInfo{}, fmt.Errorf("parse debug_token response: %w", err)
	}

	info := TokenInfo{
		AppID:       result.Data.AppID,
		Type:        result.Data.Type,
		Application: result.Data.Application,
		IsValid:     result.Data.IsValid,
		Scopes:      result.Data.Scopes,
		UserID:      result.Data.UserID,
	}
	if result.Data.ExpiresAt > 0 {
		info.ExpiresAt = time.Unix(result.Data.ExpiresAt, 0)
	}

	return info, nil
}

// Doctor runs a full diagnostic check of the Meta auth configuration.
func (tm *TokenManager) Doctor(ctx context.Context) (DoctorReport, error) {
	var report DoctorReport

	// 1. /me — check user identity
	me, err := tm.fetchMe(ctx)
	if err != nil {
		return report, fmt.Errorf("/me: %w", err)
	}
	report.UserID = me.id
	report.UserName = me.name

	// 2. /me/permissions
	perms, err := tm.fetchPermissions(ctx)
	if err != nil {
		return report, fmt.Errorf("/me/permissions: %w", err)
	}
	report.Permissions = perms

	// 3. Page info + IG linkage
	pageInfo, err := tm.fetchPageInfo(ctx)
	if err != nil {
		return report, fmt.Errorf("page info: %w", err)
	}
	report.PageID = pageInfo.id
	report.PageName = pageInfo.name
	report.IGUserID = pageInfo.igID
	report.IGUsername = pageInfo.igUsername

	// 4. Debug token
	info, err := tm.DebugToken(ctx, tm.cfg.UserToken)
	if err != nil {
		return report, fmt.Errorf("debug_token: %w", err)
	}
	report.TokenDebug = info

	if info.IsValid {
		if info.ExpiresAt.IsZero() {
			report.TokenStatus = "valid (no expiry)"
			report.DaysToExpiry = -1
		} else {
			days := int(time.Until(info.ExpiresAt).Hours() / 24)
			report.DaysToExpiry = days
			if days <= expiryWarningDays {
				report.TokenStatus = "expiring_soon"
			} else {
				report.TokenStatus = "valid"
			}
		}
	} else {
		report.TokenStatus = "invalid"
		report.DaysToExpiry = 0
	}

	// 5. Page token derivation check
	_, err = tm.GetPageAccessToken(ctx)
	report.PageTokenOK = (err == nil)

	return report, nil
}

// --- internal helpers ---

type meInfo struct {
	id   string
	name string
}

func (tm *TokenManager) fetchMe(ctx context.Context) (meInfo, error) {
	u := fmt.Sprintf("%s/me?fields=id,name&access_token=%s",
		tm.apiBase, url.QueryEscape(tm.cfg.UserToken))

	body, err := tm.doGet(ctx, u)
	if err != nil {
		return meInfo{}, err
	}

	var result struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return meInfo{}, fmt.Errorf("parse /me: %w", err)
	}
	return meInfo{id: result.ID, name: result.Name}, nil
}

func (tm *TokenManager) fetchPermissions(ctx context.Context) ([]string, error) {
	u := fmt.Sprintf("%s/me/permissions?access_token=%s",
		tm.apiBase, url.QueryEscape(tm.cfg.UserToken))

	body, err := tm.doGet(ctx, u)
	if err != nil {
		return nil, err
	}

	var result struct {
		Data []struct {
			Permission string `json:"permission"`
			Status     string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse permissions: %w", err)
	}

	var granted []string
	for _, p := range result.Data {
		if p.Status == "granted" {
			granted = append(granted, p.Permission)
		}
	}
	return granted, nil
}

type pageInfoResult struct {
	id         string
	name       string
	igID       string
	igUsername  string
}

func (tm *TokenManager) fetchPageInfo(ctx context.Context) (pageInfoResult, error) {
	if tm.cfg.PageID == "" {
		return pageInfoResult{}, fmt.Errorf("META_PAGE_ID is required")
	}

	u := fmt.Sprintf("%s/%s?fields=id,name,instagram_business_account{id,username}&access_token=%s",
		tm.apiBase,
		url.PathEscape(tm.cfg.PageID),
		url.QueryEscape(tm.cfg.UserToken),
	)

	body, err := tm.doGet(ctx, u)
	if err != nil {
		return pageInfoResult{}, err
	}

	var result struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		IBA  *struct {
			ID       string `json:"id"`
			Username string `json:"username"`
		} `json:"instagram_business_account"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return pageInfoResult{}, fmt.Errorf("parse page info: %w", err)
	}

	info := pageInfoResult{id: result.ID, name: result.Name}
	if result.IBA != nil {
		info.igID = result.IBA.ID
		info.igUsername = result.IBA.Username
	}
	return info, nil
}

// doGet performs a GET request and returns the response body.
// On non-200 responses, parses the Meta API error format and includes fbtrace_id.
func (tm *TokenManager) doGet(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	resp, err := tm.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != 200 {
		return nil, parseMetaError(resp.StatusCode, body)
	}

	return body, nil
}

// parseMetaError extracts the error message and fbtrace_id from a Meta API error response.
func parseMetaError(statusCode int, body []byte) error {
	var errResp struct {
		Error struct {
			Message  string `json:"message"`
			Type     string `json:"type"`
			Code     int    `json:"code"`
			FbTrace  string `json:"fbtrace_id"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error.Message != "" {
		msg := fmt.Sprintf("Meta API %d: %s (type=%s, code=%d",
			statusCode, errResp.Error.Message, errResp.Error.Type, errResp.Error.Code)
		if errResp.Error.FbTrace != "" {
			msg += ", fbtrace_id=" + errResp.Error.FbTrace
		}
		msg += ")"
		return fmt.Errorf("%s", msg)
	}
	return fmt.Errorf("Meta API %d: %s", statusCode, string(body))
}
