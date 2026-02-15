package authn

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"photography-publishing-workflow/internal/authstore"
)

func TestMetaAuthURL_DefaultScopes(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "tokens.json")
	m, err := NewManager(Config{
		AppID:     "123",
		AppSecret: "abc",
	}, storePath)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	rawURL, err := m.MetaAuthURL("http://127.0.0.1:8787/callback", "state123", nil)
	if err != nil {
		t.Fatalf("MetaAuthURL: %v", err)
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse URL: %v", err)
	}
	q := u.Query()
	if q.Get("client_id") != "123" {
		t.Fatalf("client_id = %q, want 123", q.Get("client_id"))
	}
	if q.Get("state") != "state123" {
		t.Fatalf("state = %q, want state123", q.Get("state"))
	}
	scope := q.Get("scope")
	if scope == "" || scope == "threads_basic" {
		t.Fatalf("unexpected scope %q", scope)
	}
}

func TestLoginMetaAndPageAccessToken(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "tokens.json")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/access_token":
			if r.Method == http.MethodPost {
				_ = r.ParseForm()
				if r.Form.Get("grant_type") == "authorization_code" {
					_, _ = w.Write([]byte(`{"access_token":"meta_short","expires_in":3600}`))
					return
				}
			}
			if r.Method == http.MethodGet && r.URL.Query().Get("grant_type") == "fb_exchange_token" {
				_, _ = w.Write([]byte(`{"access_token":"meta_long","expires_in":2592000}`))
				return
			}
			http.Error(w, "bad request", http.StatusBadRequest)
		case "/me":
			_, _ = w.Write([]byte(`{"id":"26600065222929008"}`))
		case "/debug_token":
			_, _ = w.Write([]byte(`{"data":{"scopes":["instagram_basic","instagram_content_publish"]}}`))
		case "/987405421123281":
			_, _ = w.Write([]byte(`{"id":"987405421123281","access_token":"page_token_1"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	m, err := NewManager(Config{
		AppID:            "123",
		AppSecret:        "abc",
		PageID:           "987405421123281",
		MetaGraphBase:    srv.URL,
		ThreadsGraphBase: srv.URL,
	}, storePath)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	ctx := context.Background()
	if err := m.LoginMeta(ctx, "auth_code", "http://127.0.0.1:8787/auth/meta/callback"); err != nil {
		t.Fatalf("LoginMeta: %v", err)
	}

	userTok, err := m.MetaUserToken(ctx)
	if err != nil {
		t.Fatalf("MetaUserToken: %v", err)
	}
	if userTok != "meta_long" {
		t.Fatalf("MetaUserToken = %q, want %q", userTok, "meta_long")
	}

	pageTok, err := m.PageAccessToken(ctx)
	if err != nil {
		t.Fatalf("PageAccessToken: %v", err)
	}
	if pageTok != "page_token_1" {
		t.Fatalf("PageAccessToken = %q, want %q", pageTok, "page_token_1")
	}

	status, err := m.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !status.Meta.Present {
		t.Fatalf("expected meta token to be present")
	}
	if status.Meta.UserID != "26600065222929008" {
		t.Fatalf("meta user_id = %q, want 26600065222929008", status.Meta.UserID)
	}
}

func TestThreadsAccessTokenRefresh(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "tokens.json")
	s, err := authstore.New(storePath)
	if err != nil {
		t.Fatalf("authstore.New: %v", err)
	}
	if err := s.Save(authstore.StoreData{
		Meta: authstore.MetaTokens{
			UserAccessToken: "meta_long",
			UserExpiresAt:   time.Now().UTC().Add(30 * 24 * time.Hour),
		},
		Threads: authstore.ThreadsTokens{
			UserID:      "th_user_1",
			AccessToken: "th_old",
			ExpiresAt:   time.Now().UTC().Add(1 * time.Hour),
		},
	}); err != nil {
		t.Fatalf("store save: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/refresh_access_token" || r.URL.Query().Get("grant_type") != "th_refresh_token" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("access_token") != "th_old" {
			http.Error(w, "wrong token", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"access_token":"th_new","expires_in":5184000}`))
	}))
	defer srv.Close()

	m, err := NewManager(Config{
		AppID:            "123",
		AppSecret:        "abc",
		MetaGraphBase:    srv.URL,
		ThreadsGraphBase: srv.URL,
	}, storePath)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	token, err := m.ThreadsAccessToken(context.Background())
	if err != nil {
		t.Fatalf("ThreadsAccessToken: %v", err)
	}
	if token != "th_new" {
		t.Fatalf("token = %q, want %q", token, "th_new")
	}

	reloaded, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if reloaded.Threads.AccessToken != "th_new" {
		t.Fatalf("stored token = %q, want %q", reloaded.Threads.AccessToken, "th_new")
	}
}

func TestThreadsAuthURL_UsesThreadsOverrideAppID(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "tokens.json")
	m, err := NewManager(Config{
		AppID:        "meta_app_id",
		AppSecret:    "meta_secret",
		ThreadsAppID: "threads_app_id",
	}, storePath)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	rawURL, err := m.ThreadsAuthURL("http://localhost:8787/auth/threads/callback", "abc", nil)
	if err != nil {
		t.Fatalf("ThreadsAuthURL: %v", err)
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse URL: %v", err)
	}
	if got := u.Query().Get("client_id"); got != "threads_app_id" {
		t.Fatalf("threads client_id = %q, want %q", got, "threads_app_id")
	}
}

func TestExchangeThreadsCode_UsesThreadsOverrideCredentials(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "tokens.json")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth/access_token" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm: %v", err)
		}
		if r.Form.Get("client_id") != "threads_app_id" {
			t.Fatalf("client_id = %q, want threads_app_id", r.Form.Get("client_id"))
		}
		if r.Form.Get("client_secret") != "threads_secret" {
			t.Fatalf("client_secret = %q, want threads_secret", r.Form.Get("client_secret"))
		}
		_, _ = w.Write([]byte(`{"access_token":"th_short","expires_in":3600}`))
	}))
	defer srv.Close()

	m, err := NewManager(Config{
		AppID:            "meta_app_id",
		AppSecret:        "meta_secret",
		ThreadsAppID:     "threads_app_id",
		ThreadsAppSecret: "threads_secret",
		ThreadsGraphBase: srv.URL,
	}, storePath)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	tok, _, err := m.exchangeThreadsCode(context.Background(), "abc_code", "http://localhost:8787/auth/threads/callback")
	if err != nil {
		t.Fatalf("exchangeThreadsCode: %v", err)
	}
	if tok != "th_short" {
		t.Fatalf("token = %q, want th_short", tok)
	}
}

func TestLoginMeta_FallsBackToDebugTokenExpiryWhenExpiresInMissing(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "tokens.json")
	expiresAt := time.Now().UTC().Add(45 * 24 * time.Hour).Unix()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/access_token":
			if r.Method == http.MethodPost {
				_, _ = w.Write([]byte(`{"access_token":"meta_short","expires_in":3600}`))
				return
			}
			// Intentionally missing expires_in.
			_, _ = w.Write([]byte(`{"access_token":"meta_long"}`))
		case "/me":
			_, _ = w.Write([]byte(`{"id":"26600065222929008"}`))
		case "/debug_token":
			_, _ = w.Write([]byte(`{"data":{"is_valid":true,"expires_at":` + fmt.Sprintf("%d", expiresAt) + `,"scopes":["instagram_basic"]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	m, err := NewManager(Config{
		AppID:            "meta_app",
		AppSecret:        "meta_secret",
		MetaGraphBase:    srv.URL,
		ThreadsGraphBase: srv.URL,
	}, storePath)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if err := m.LoginMeta(context.Background(), "code", "http://localhost:8787/auth/meta/callback"); err != nil {
		t.Fatalf("LoginMeta: %v", err)
	}

	status, err := m.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.Meta.ExpiresAt.IsZero() {
		t.Fatalf("expected resolved expiry, got zero")
	}
	if status.Meta.DaysToLive < 30 {
		t.Fatalf("days_to_live = %d, expected >= 30", status.Meta.DaysToLive)
	}
}

func TestStatusWhenStoreMissing(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "tokens.json")
	m, err := NewManager(Config{
		AppID:     "123",
		AppSecret: "abc",
	}, storePath)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	status, err := m.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.StorePath == "" {
		t.Fatal("StorePath should be populated")
	}
	if status.Meta.Present || status.Threads.Present {
		t.Fatal("expected empty status when store does not exist")
	}
}
