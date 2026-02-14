package meta

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newTestServer creates an httptest server that routes requests to handler functions.
func newTestServer(t *testing.T, handlers map[string]http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for pattern, h := range handlers {
			if strings.HasPrefix(r.URL.Path, pattern) {
				h(w, r)
				return
			}
		}
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		http.Error(w, "not found", 404)
	}))
}

func TestEnsureLongLivedUserToken_Success(t *testing.T) {
	ts := newTestServer(t, map[string]http.HandlerFunc{
		"/oauth/access_token": func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("grant_type") != "fb_exchange_token" {
				t.Errorf("grant_type = %q", r.URL.Query().Get("grant_type"))
			}
			json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "LONG_LIVED_TOKEN_XYZ",
				"token_type":   "bearer",
				"expires_in":   5184000, // 60 days
			})
		},
	})
	defer ts.Close()

	tm := NewTokenManager(Config{
		AppID:        "test_app_id",
		AppSecret:    "test_app_secret",
		UserToken:    "short_lived_token",
		GraphAPIBase: ts.URL,
	})

	token, expiresAt, err := tm.EnsureLongLivedUserToken(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "LONG_LIVED_TOKEN_XYZ" {
		t.Errorf("token = %q, want LONG_LIVED_TOKEN_XYZ", token)
	}
	// Expires should be roughly 60 days from now
	days := int(time.Until(expiresAt).Hours() / 24)
	if days < 59 || days > 61 {
		t.Errorf("expiresAt ~%d days from now, want ~60", days)
	}
}

func TestEnsureLongLivedUserToken_MissingCredentials(t *testing.T) {
	tm := NewTokenManager(Config{UserToken: "tok"})
	_, _, err := tm.EnsureLongLivedUserToken(context.Background())
	if err == nil {
		t.Fatal("expected error for missing credentials")
	}
}

func TestEnsureLongLivedUserToken_APIError(t *testing.T) {
	ts := newTestServer(t, map[string]http.HandlerFunc{
		"/oauth/access_token": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(400)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": map[string]interface{}{
					"message":    "Invalid OAuth access token",
					"type":       "OAuthException",
					"code":       190,
					"fbtrace_id": "ABC123trace",
				},
			})
		},
	})
	defer ts.Close()

	tm := NewTokenManager(Config{
		AppID:        "app",
		AppSecret:    "secret",
		UserToken:    "bad_token",
		GraphAPIBase: ts.URL,
	})

	_, _, err := tm.EnsureLongLivedUserToken(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "ABC123trace") {
		t.Errorf("error should contain fbtrace_id, got: %v", err)
	}
	if !strings.Contains(err.Error(), "190") {
		t.Errorf("error should contain error code 190, got: %v", err)
	}
}

func TestGetPageAccessToken_Success(t *testing.T) {
	callCount := 0
	ts := newTestServer(t, map[string]http.HandlerFunc{
		"/page123": func(w http.ResponseWriter, r *http.Request) {
			callCount++
			json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "PAGE_TOKEN_ABC",
				"id":           "page123",
			})
		},
	})
	defer ts.Close()

	tm := NewTokenManager(Config{
		PageID:       "page123",
		UserToken:    "user_token",
		GraphAPIBase: ts.URL,
	})

	tok, err := tm.GetPageAccessToken(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok != "PAGE_TOKEN_ABC" {
		t.Errorf("token = %q, want PAGE_TOKEN_ABC", tok)
	}
	if callCount != 1 {
		t.Errorf("API calls = %d, want 1", callCount)
	}

	// Second call should use cache
	tok2, err := tm.GetPageAccessToken(context.Background())
	if err != nil {
		t.Fatalf("unexpected error on cached call: %v", err)
	}
	if tok2 != "PAGE_TOKEN_ABC" {
		t.Errorf("cached token = %q", tok2)
	}
	if callCount != 1 {
		t.Errorf("API calls after cache = %d, want 1 (should use cache)", callCount)
	}
}

func TestGetPageAccessToken_CacheInvalidation(t *testing.T) {
	callCount := 0
	ts := newTestServer(t, map[string]http.HandlerFunc{
		"/page123": func(w http.ResponseWriter, r *http.Request) {
			callCount++
			json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": fmt.Sprintf("PAGE_TOKEN_%d", callCount),
				"id":           "page123",
			})
		},
	})
	defer ts.Close()

	tm := NewTokenManager(Config{
		PageID:       "page123",
		UserToken:    "user_token",
		GraphAPIBase: ts.URL,
	})

	tok1, _ := tm.GetPageAccessToken(context.Background())
	if tok1 != "PAGE_TOKEN_1" {
		t.Errorf("first token = %q", tok1)
	}

	// Invalidate cache
	tm.InvalidatePageTokenCache()

	tok2, _ := tm.GetPageAccessToken(context.Background())
	if tok2 != "PAGE_TOKEN_2" {
		t.Errorf("second token after invalidation = %q, want PAGE_TOKEN_2", tok2)
	}
	if callCount != 2 {
		t.Errorf("API calls = %d, want 2", callCount)
	}
}

func TestDebugToken_Success(t *testing.T) {
	expiresAt := time.Now().Add(60 * 24 * time.Hour).Unix()
	ts := newTestServer(t, map[string]http.HandlerFunc{
		"/debug_token": func(w http.ResponseWriter, r *http.Request) {
			// Verify app token format
			appToken := r.URL.Query().Get("access_token")
			if appToken != "app_id|app_secret" {
				t.Errorf("access_token = %q, want app_id|app_secret", appToken)
			}
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"app_id":      "app_id",
					"type":        "USER",
					"application": "TestApp",
					"expires_at":  expiresAt,
					"is_valid":    true,
					"scopes":      []string{"pages_show_list", "instagram_basic"},
					"user_id":     "12345",
				},
			})
		},
	})
	defer ts.Close()

	tm := NewTokenManager(Config{
		AppID:        "app_id",
		AppSecret:    "app_secret",
		UserToken:    "some_token",
		GraphAPIBase: ts.URL,
	})

	info, err := tm.DebugToken(context.Background(), "some_token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !info.IsValid {
		t.Error("expected IsValid = true")
	}
	if info.Type != "USER" {
		t.Errorf("type = %q, want USER", info.Type)
	}
	if len(info.Scopes) != 2 {
		t.Errorf("scopes = %v, want 2 items", info.Scopes)
	}
	if info.UserID != "12345" {
		t.Errorf("user_id = %q, want 12345", info.UserID)
	}
}

func TestDoctor_FullFlow(t *testing.T) {
	expiresAt := time.Now().Add(60 * 24 * time.Hour).Unix()
	ts := newTestServer(t, map[string]http.HandlerFunc{
		"/me/permissions": func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []map[string]string{
					{"permission": "pages_show_list", "status": "granted"},
					{"permission": "instagram_basic", "status": "granted"},
					{"permission": "email", "status": "declined"},
				},
			})
		},
		"/me": func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"id":   "user_123",
				"name": "Omar N",
			})
		},
		"/page_456": func(w http.ResponseWriter, r *http.Request) {
			fields := r.URL.Query().Get("fields")
			if strings.Contains(fields, "instagram_business_account") {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"id":   "page_456",
					"name": "My Photo Page",
					"instagram_business_account": map[string]interface{}{
						"id":       "ig_789",
						"username": "nebtrx",
					},
				})
			} else {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"access_token": "derived_page_token",
					"id":           "page_456",
				})
			}
		},
		"/debug_token": func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"app_id":      "app_id",
					"type":        "USER",
					"application": "TestApp",
					"expires_at":  expiresAt,
					"is_valid":    true,
					"scopes":      []string{"pages_show_list", "instagram_basic"},
					"user_id":     "user_123",
				},
			})
		},
	})
	defer ts.Close()

	tm := NewTokenManager(Config{
		AppID:        "app_id",
		AppSecret:    "app_secret",
		PageID:       "page_456",
		UserToken:    "user_token",
		GraphAPIBase: ts.URL,
	})

	report, err := tm.Doctor(context.Background())
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}

	if report.UserID != "user_123" {
		t.Errorf("user_id = %q", report.UserID)
	}
	if report.UserName != "Omar N" {
		t.Errorf("user_name = %q", report.UserName)
	}
	if len(report.Permissions) != 2 {
		t.Errorf("permissions = %v, want 2 granted", report.Permissions)
	}
	if report.PageName != "My Photo Page" {
		t.Errorf("page_name = %q", report.PageName)
	}
	if report.IGUserID != "ig_789" {
		t.Errorf("ig_user_id = %q", report.IGUserID)
	}
	if report.IGUsername != "nebtrx" {
		t.Errorf("ig_username = %q", report.IGUsername)
	}
	if report.TokenStatus != "valid" {
		t.Errorf("token_status = %q, want valid", report.TokenStatus)
	}
	if !report.PageTokenOK {
		t.Error("page_token_ok should be true")
	}
}

func TestFbtraceID_InErrors(t *testing.T) {
	ts := newTestServer(t, map[string]http.HandlerFunc{
		"/page_bad": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(400)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": map[string]interface{}{
					"message":    "Unsupported get request",
					"type":       "GraphMethodException",
					"code":       100,
					"fbtrace_id": "TRACE_XYZ_123",
				},
			})
		},
	})
	defer ts.Close()

	tm := NewTokenManager(Config{
		PageID:       "page_bad",
		UserToken:    "tok",
		GraphAPIBase: ts.URL,
	})

	_, err := tm.GetPageAccessToken(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "TRACE_XYZ_123") {
		t.Errorf("error should contain fbtrace_id, got: %v", err)
	}
}

func TestGetPageAccessToken_MissingPageID(t *testing.T) {
	tm := NewTokenManager(Config{UserToken: "tok"})
	_, err := tm.GetPageAccessToken(context.Background())
	if err == nil {
		t.Fatal("expected error for missing page ID")
	}
	if !strings.Contains(err.Error(), "META_PAGE_ID") {
		t.Errorf("error should mention META_PAGE_ID, got: %v", err)
	}
}
