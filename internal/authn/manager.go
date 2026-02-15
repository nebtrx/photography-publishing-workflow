// Package authn manages OAuth login, persisted tokens, and refresh lifecycle.
package authn

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"photography-publishing-workflow/internal/authstore"
	"photography-publishing-workflow/internal/meta"
)

const (
	defaultMetaGraphBase    = "https://graph.facebook.com/v21.0"
	defaultThreadsGraphBase = "https://graph.threads.net/v1.0"
	defaultMetaAuthBase     = "https://www.facebook.com/v21.0"
	defaultThreadsAuthBase  = "https://threads.net"
	defaultRefreshWindow    = 7 * 24 * time.Hour
)

// Default scopes used for one-time OAuth login.
var (
	DefaultMetaScopes = []string{
		"pages_show_list",
		"pages_read_engagement",
		"instagram_basic",
		"instagram_content_publish",
		"public_profile",
	}
	DefaultThreadsScopes = []string{
		"threads_basic",
		"threads_content_publish",
	}
)

// Config configures auth behavior and endpoints.
type Config struct {
	AppID            string
	AppSecret        string
	ThreadsAppID     string
	ThreadsAppSecret string
	PageID           string
	HTTPClient       *http.Client

	MetaGraphBase    string
	ThreadsGraphBase string
	MetaAuthBase     string
	ThreadsAuthBase  string

	RefreshWindow time.Duration
}

// Manager owns token lifecycle operations.
type Manager struct {
	cfg   Config
	store *authstore.Store
}

// Status describes persisted token health.
type Status struct {
	StorePath string
	Meta      TokenStatus
	Threads   TokenStatus
}

// TokenStatus is a generic status view for one provider token.
type TokenStatus struct {
	Present    bool
	UserID     string
	Scopes     []string
	ExpiresAt  time.Time
	DaysToLive int
	Expiring   bool
}

// NewManager creates a manager and its backing store.
func NewManager(cfg Config, storePath string) (*Manager, error) {
	if strings.TrimSpace(cfg.AppID) == "" {
		return nil, fmt.Errorf("meta.app_id is required")
	}
	if strings.TrimSpace(cfg.AppSecret) == "" {
		return nil, fmt.Errorf("meta.app_secret is required")
	}

	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 20 * time.Second}
	}
	if cfg.MetaGraphBase == "" {
		cfg.MetaGraphBase = defaultMetaGraphBase
	}
	if cfg.ThreadsGraphBase == "" {
		cfg.ThreadsGraphBase = defaultThreadsGraphBase
	}
	if cfg.MetaAuthBase == "" {
		cfg.MetaAuthBase = defaultMetaAuthBase
	}
	if cfg.ThreadsAuthBase == "" {
		cfg.ThreadsAuthBase = defaultThreadsAuthBase
	}
	if cfg.RefreshWindow <= 0 {
		cfg.RefreshWindow = defaultRefreshWindow
	}

	s, err := authstore.New(storePath)
	if err != nil {
		return nil, err
	}
	return &Manager{cfg: cfg, store: s}, nil
}

// StorePath returns resolved token store path.
func (m *Manager) StorePath() string {
	return m.store.Path()
}

// Status reads persisted token status without network calls.
func (m *Manager) Status() (Status, error) {
	data, err := m.store.Load()
	if err != nil {
		if err == authstore.ErrNotFound {
			return Status{StorePath: m.store.Path()}, nil
		}
		return Status{}, err
	}

	now := time.Now().UTC()
	s := Status{StorePath: m.store.Path()}

	if data.Meta.UserAccessToken != "" {
		s.Meta.Present = true
		s.Meta.UserID = data.Meta.UserID
		s.Meta.Scopes = data.Meta.Scopes
		s.Meta.ExpiresAt = data.Meta.UserExpiresAt
		if !data.Meta.UserExpiresAt.IsZero() {
			days := int(time.Until(data.Meta.UserExpiresAt).Hours() / 24)
			s.Meta.DaysToLive = days
			s.Meta.Expiring = data.Meta.UserExpiresAt.Before(now.Add(m.cfg.RefreshWindow))
		}
	}
	if data.Threads.AccessToken != "" {
		s.Threads.Present = true
		s.Threads.UserID = data.Threads.UserID
		s.Threads.Scopes = data.Threads.Scopes
		s.Threads.ExpiresAt = data.Threads.ExpiresAt
		if !data.Threads.ExpiresAt.IsZero() {
			days := int(time.Until(data.Threads.ExpiresAt).Hours() / 24)
			s.Threads.DaysToLive = days
			s.Threads.Expiring = data.Threads.ExpiresAt.Before(now.Add(m.cfg.RefreshWindow))
		}
	}

	return s, nil
}

// Clear removes persisted tokens from the store path.
func (m *Manager) Clear() error {
	return m.store.Save(authstore.StoreData{Version: authstore.SchemaVersion})
}

// MetaAuthURL returns the Meta OAuth authorize URL.
func (m *Manager) MetaAuthURL(redirectURI, state string, scopes []string) (string, error) {
	if strings.TrimSpace(m.cfg.AppID) == "" {
		return "", fmt.Errorf("meta.app_id is required")
	}
	if strings.TrimSpace(redirectURI) == "" {
		return "", fmt.Errorf("redirect URI is required")
	}
	if len(scopes) == 0 {
		scopes = DefaultMetaScopes
	}
	q := url.Values{
		"client_id":     {m.cfg.AppID},
		"redirect_uri":  {redirectURI},
		"response_type": {"code"},
		"scope":         {strings.Join(scopes, ",")},
	}
	if state != "" {
		q.Set("state", state)
	}
	u := strings.TrimRight(m.cfg.MetaAuthBase, "/") + "/dialog/oauth?" + q.Encode()
	return u, nil
}

// ThreadsAuthURL returns the Threads OAuth authorize URL.
func (m *Manager) ThreadsAuthURL(redirectURI, state string, scopes []string) (string, error) {
	appID := m.threadsAppID()
	if strings.TrimSpace(appID) == "" {
		return "", fmt.Errorf("threads.app_id or meta.app_id is required")
	}
	if strings.TrimSpace(redirectURI) == "" {
		return "", fmt.Errorf("redirect URI is required")
	}
	if len(scopes) == 0 {
		scopes = DefaultThreadsScopes
	}
	q := url.Values{
		"client_id":     {appID},
		"redirect_uri":  {redirectURI},
		"response_type": {"code"},
		"scope":         {strings.Join(scopes, ",")},
	}
	if state != "" {
		q.Set("state", state)
	}
	u := strings.TrimRight(m.cfg.ThreadsAuthBase, "/") + "/oauth/authorize?" + q.Encode()
	return u, nil
}

// LoginMeta exchanges a code for tokens and persists Meta token state.
func (m *Manager) LoginMeta(ctx context.Context, code, redirectURI string) error {
	if err := m.requireAppCredentials(); err != nil {
		return err
	}
	code = strings.TrimSpace(code)
	if code == "" {
		return fmt.Errorf("meta auth code is required")
	}

	shortTok, _, err := m.exchangeMetaCode(ctx, code, redirectURI)
	if err != nil {
		return err
	}

	longTok, expiresAt, err := m.exchangeMetaLongLived(ctx, shortTok)
	if err != nil {
		return err
	}

	userID, err := m.fetchMetaUserID(ctx, longTok)
	if err != nil {
		return err
	}
	scopes := m.fetchMetaScopesBestEffort(ctx, longTok)

	return m.store.Update(func(in authstore.StoreData) (authstore.StoreData, error) {
		in.Meta.UserID = userID
		in.Meta.Scopes = scopes
		in.Meta.UserAccessToken = longTok
		in.Meta.UserExpiresAt = expiresAt
		in.Meta.PageAccessToken = ""
		in.Meta.PageDerivedAt = time.Time{}
		return in, nil
	})
}

// LoginThreads exchanges a code for tokens and persists Threads token state.
func (m *Manager) LoginThreads(ctx context.Context, code, redirectURI string) error {
	if err := m.requireThreadsAppCredentials(); err != nil {
		return err
	}
	code = strings.TrimSpace(code)
	if code == "" {
		return fmt.Errorf("threads auth code is required")
	}

	shortTok, _, err := m.exchangeThreadsCode(ctx, code, redirectURI)
	if err != nil {
		return err
	}

	longTok, expiresAt, err := m.exchangeThreadsLongLived(ctx, shortTok)
	if err != nil {
		return err
	}

	userID, err := m.fetchThreadsUserID(ctx, longTok)
	if err != nil {
		return err
	}

	return m.store.Update(func(in authstore.StoreData) (authstore.StoreData, error) {
		in.Threads.UserID = userID
		in.Threads.AccessToken = longTok
		in.Threads.ExpiresAt = expiresAt
		if len(in.Threads.Scopes) == 0 {
			in.Threads.Scopes = append([]string{}, DefaultThreadsScopes...)
		}
		return in, nil
	})
}

// MetaUserToken returns a current Meta user token, refreshing when near expiry.
func (m *Manager) MetaUserToken(ctx context.Context) (string, error) {
	data, err := m.store.Load()
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(data.Meta.UserAccessToken)
	if token == "" {
		return "", fmt.Errorf("meta user token not found in %s; run `ppw auth login`", m.store.Path())
	}

	if data.Meta.UserExpiresAt.IsZero() || data.Meta.UserExpiresAt.After(time.Now().UTC().Add(m.cfg.RefreshWindow)) {
		return token, nil
	}

	refreshed, expiresAt, err := m.exchangeMetaLongLived(ctx, token)
	if err != nil {
		return token, fmt.Errorf("meta token refresh failed: %w", err)
	}

	if err := m.store.Update(func(in authstore.StoreData) (authstore.StoreData, error) {
		in.Meta.UserAccessToken = refreshed
		in.Meta.UserExpiresAt = expiresAt
		return in, nil
	}); err != nil {
		return "", err
	}
	return refreshed, nil
}

// ThreadsAccessToken returns a current Threads access token, refreshing when near expiry.
func (m *Manager) ThreadsAccessToken(ctx context.Context) (string, error) {
	data, err := m.store.Load()
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(data.Threads.AccessToken)
	if token == "" {
		return "", fmt.Errorf("threads access token not found in %s; run `ppw auth login`", m.store.Path())
	}

	if data.Threads.ExpiresAt.IsZero() || data.Threads.ExpiresAt.After(time.Now().UTC().Add(m.cfg.RefreshWindow)) {
		return token, nil
	}

	refreshed, expiresAt, err := m.refreshThreadsToken(ctx, token)
	if err != nil {
		return token, fmt.Errorf("threads token refresh failed: %w", err)
	}

	if err := m.store.Update(func(in authstore.StoreData) (authstore.StoreData, error) {
		in.Threads.AccessToken = refreshed
		in.Threads.ExpiresAt = expiresAt
		return in, nil
	}); err != nil {
		return "", err
	}
	return refreshed, nil
}

// PageAccessToken derives a Page token from the current Meta user token.
func (m *Manager) PageAccessToken(ctx context.Context) (string, error) {
	if strings.TrimSpace(m.cfg.PageID) == "" {
		return "", fmt.Errorf("meta.page_id is required")
	}

	userTok, err := m.MetaUserToken(ctx)
	if err != nil {
		return "", err
	}

	tm := meta.NewTokenManager(meta.Config{
		AppID:        m.cfg.AppID,
		AppSecret:    m.cfg.AppSecret,
		PageID:       m.cfg.PageID,
		UserToken:    userTok,
		GraphAPIBase: m.cfg.MetaGraphBase,
	})
	pageTok, err := tm.GetPageAccessToken(ctx)
	if err != nil {
		return "", err
	}

	_ = m.store.Update(func(in authstore.StoreData) (authstore.StoreData, error) {
		in.Meta.PageAccessToken = pageTok
		in.Meta.PageDerivedAt = time.Now().UTC()
		return in, nil
	})
	return pageTok, nil
}

func (m *Manager) exchangeMetaCode(ctx context.Context, code, redirectURI string) (string, time.Time, error) {
	if err := m.requireAppCredentials(); err != nil {
		return "", time.Time{}, err
	}
	u := strings.TrimRight(m.cfg.MetaGraphBase, "/") + "/oauth/access_token"
	form := url.Values{
		"client_id":     {m.cfg.AppID},
		"client_secret": {m.cfg.AppSecret},
		"grant_type":    {"authorization_code"},
		"redirect_uri":  {redirectURI},
		"code":          {code},
	}
	body, err := m.doPOSTForm(ctx, u, form)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("meta code exchange: %w", err)
	}
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", time.Time{}, fmt.Errorf("parse meta exchange response: %w", err)
	}
	if out.AccessToken == "" {
		return "", time.Time{}, fmt.Errorf("meta exchange returned empty access_token")
	}
	expiresAt := expiryFromExpiresIn(out.ExpiresIn)
	if expiresAt.IsZero() {
		expiresAt = m.lookupMetaTokenExpiryBestEffort(ctx, out.AccessToken)
	}
	return out.AccessToken, expiresAt, nil
}

func (m *Manager) exchangeMetaLongLived(ctx context.Context, userToken string) (string, time.Time, error) {
	if err := m.requireAppCredentials(); err != nil {
		return "", time.Time{}, err
	}
	u := strings.TrimRight(m.cfg.MetaGraphBase, "/") + "/oauth/access_token"
	params := url.Values{
		"grant_type":        {"fb_exchange_token"},
		"client_id":         {m.cfg.AppID},
		"client_secret":     {m.cfg.AppSecret},
		"fb_exchange_token": {userToken},
	}
	body, err := m.doGET(ctx, u+"?"+params.Encode())
	if err != nil {
		return "", time.Time{}, fmt.Errorf("meta long-lived exchange: %w", err)
	}
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", time.Time{}, fmt.Errorf("parse meta long-lived response: %w", err)
	}
	if out.AccessToken == "" {
		return "", time.Time{}, fmt.Errorf("meta long-lived exchange returned empty access_token")
	}
	expiresAt := expiryFromExpiresIn(out.ExpiresIn)
	if expiresAt.IsZero() {
		expiresAt = m.lookupMetaTokenExpiryBestEffort(ctx, out.AccessToken)
	}
	return out.AccessToken, expiresAt, nil
}

func (m *Manager) exchangeThreadsCode(ctx context.Context, code, redirectURI string) (string, time.Time, error) {
	if err := m.requireThreadsAppCredentials(); err != nil {
		return "", time.Time{}, err
	}
	appID := m.threadsAppID()
	appSecret := m.threadsAppSecret()
	u := strings.TrimRight(m.cfg.ThreadsGraphBase, "/") + "/oauth/access_token"
	form := url.Values{
		"client_id":     {appID},
		"client_secret": {appSecret},
		"grant_type":    {"authorization_code"},
		"redirect_uri":  {redirectURI},
		"code":          {code},
	}
	body, err := m.doPOSTForm(ctx, u, form)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("threads code exchange: %w", err)
	}
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", time.Time{}, fmt.Errorf("parse threads exchange response: %w", err)
	}
	if out.AccessToken == "" {
		return "", time.Time{}, fmt.Errorf("threads exchange returned empty access_token")
	}
	return out.AccessToken, expiryFromExpiresIn(out.ExpiresIn), nil
}

func (m *Manager) exchangeThreadsLongLived(ctx context.Context, shortToken string) (string, time.Time, error) {
	if err := m.requireThreadsAppCredentials(); err != nil {
		return "", time.Time{}, err
	}
	appSecret := m.threadsAppSecret()
	u := strings.TrimRight(m.cfg.ThreadsGraphBase, "/") + "/access_token"
	params := url.Values{
		"grant_type":    {"th_exchange_token"},
		"client_secret": {appSecret},
		"access_token":  {shortToken},
	}
	body, err := m.doGET(ctx, u+"?"+params.Encode())
	if err != nil {
		return "", time.Time{}, fmt.Errorf("threads long-lived exchange: %w", err)
	}
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", time.Time{}, fmt.Errorf("parse threads long-lived response: %w", err)
	}
	if out.AccessToken == "" {
		return "", time.Time{}, fmt.Errorf("threads long-lived exchange returned empty access_token")
	}
	return out.AccessToken, expiryFromExpiresIn(out.ExpiresIn), nil
}

func (m *Manager) refreshThreadsToken(ctx context.Context, current string) (string, time.Time, error) {
	u := strings.TrimRight(m.cfg.ThreadsGraphBase, "/") + "/refresh_access_token"
	params := url.Values{
		"grant_type":   {"th_refresh_token"},
		"access_token": {current},
	}
	body, err := m.doGET(ctx, u+"?"+params.Encode())
	if err != nil {
		return "", time.Time{}, fmt.Errorf("threads refresh: %w", err)
	}
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", time.Time{}, fmt.Errorf("parse threads refresh response: %w", err)
	}
	if out.AccessToken == "" {
		return "", time.Time{}, fmt.Errorf("threads refresh returned empty access_token")
	}
	return out.AccessToken, expiryFromExpiresIn(out.ExpiresIn), nil
}

func expiryFromExpiresIn(expiresIn int64) time.Time {
	if expiresIn <= 0 {
		return time.Time{}
	}
	return time.Now().UTC().Add(time.Duration(expiresIn) * time.Second)
}

func (m *Manager) requireAppCredentials() error {
	if strings.TrimSpace(m.cfg.AppID) == "" || strings.TrimSpace(m.cfg.AppSecret) == "" {
		return fmt.Errorf("meta.app_id and meta.app_secret are required")
	}
	return nil
}

func (m *Manager) requireThreadsAppCredentials() error {
	if strings.TrimSpace(m.threadsAppID()) == "" || strings.TrimSpace(m.threadsAppSecret()) == "" {
		return fmt.Errorf("threads.app_id/threads.app_secret (or meta.app_id/meta.app_secret fallback) are required")
	}
	return nil
}

func (m *Manager) threadsAppID() string {
	if v := strings.TrimSpace(m.cfg.ThreadsAppID); v != "" {
		return v
	}
	return strings.TrimSpace(m.cfg.AppID)
}

func (m *Manager) threadsAppSecret() string {
	if v := strings.TrimSpace(m.cfg.ThreadsAppSecret); v != "" {
		return v
	}
	return strings.TrimSpace(m.cfg.AppSecret)
}

func (m *Manager) lookupMetaTokenExpiryBestEffort(ctx context.Context, token string) time.Time {
	tm := meta.NewTokenManager(meta.Config{
		AppID:        m.cfg.AppID,
		AppSecret:    m.cfg.AppSecret,
		GraphAPIBase: m.cfg.MetaGraphBase,
	})
	info, err := tm.DebugToken(ctx, token)
	if err != nil {
		return time.Time{}
	}
	return info.ExpiresAt
}

func (m *Manager) fetchMetaUserID(ctx context.Context, token string) (string, error) {
	u := strings.TrimRight(m.cfg.MetaGraphBase, "/") + "/me?fields=id&access_token=" + url.QueryEscape(token)
	body, err := m.doGET(ctx, u)
	if err != nil {
		return "", fmt.Errorf("fetch meta /me: %w", err)
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("parse meta /me response: %w", err)
	}
	if out.ID == "" {
		return "", fmt.Errorf("meta /me returned empty id")
	}
	return out.ID, nil
}

func (m *Manager) fetchThreadsUserID(ctx context.Context, token string) (string, error) {
	u := strings.TrimRight(m.cfg.ThreadsGraphBase, "/") + "/me?fields=id&access_token=" + url.QueryEscape(token)
	body, err := m.doGET(ctx, u)
	if err != nil {
		return "", fmt.Errorf("fetch threads /me: %w", err)
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("parse threads /me response: %w", err)
	}
	if out.ID == "" {
		return "", fmt.Errorf("threads /me returned empty id")
	}
	return out.ID, nil
}

func (m *Manager) fetchMetaScopesBestEffort(ctx context.Context, token string) []string {
	tm := meta.NewTokenManager(meta.Config{
		AppID:        m.cfg.AppID,
		AppSecret:    m.cfg.AppSecret,
		GraphAPIBase: m.cfg.MetaGraphBase,
	})
	info, err := tm.DebugToken(ctx, token)
	if err != nil {
		return nil
	}
	return append([]string{}, info.Scopes...)
}

func (m *Manager) doGET(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := m.cfg.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return body, nil
}

func (m *Manager) doPOSTForm(ctx context.Context, rawURL string, form url.Values) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := m.cfg.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return body, nil
}
