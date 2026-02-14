package instagram

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

// patchGraphAPIBase temporarily overrides graphAPIBase for testing and returns a cleanup func.
// Since graphAPIBase is a const, we use a test-scoped client with an httptest server instead.

func TestClient_TokenFn_Used(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := r.URL.Query().Get("access_token")
		if tok != "page_token_xyz" {
			t.Errorf("expected page_token_xyz, got %q", tok)
			w.WriteHeader(401)
			json.NewEncoder(w).Encode(map[string]interface{}{"error": map[string]interface{}{"message": "bad token", "code": 190}})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"id": "17841442845568912"})
	}))
	defer ts.Close()

	client := &Client{
		UserID: "17841442845568912",
		TokenFn: func(ctx context.Context) (string, error) {
			return "page_token_xyz", nil
		},
		HTTPClient: &http.Client{Timeout: 5 * time.Second},
	}

	// Call doGet directly against the test server — we can't override the const graphAPIBase,
	// so we test the token flow via the getToken/doWithRetry mechanism.
	tok, err := client.getToken()
	if err != nil {
		t.Fatalf("getToken: %v", err)
	}
	if tok != "page_token_xyz" {
		t.Errorf("token = %q, want page_token_xyz", tok)
	}
}

func TestClient_StaticToken_Fallback(t *testing.T) {
	client := &Client{
		UserID:      "12345",
		AccessToken: "static_token_abc",
		HTTPClient:  &http.Client{Timeout: 5 * time.Second},
	}

	tok, err := client.getToken()
	if err != nil {
		t.Fatalf("getToken: %v", err)
	}
	if tok != "static_token_abc" {
		t.Errorf("token = %q, want static_token_abc", tok)
	}
}

func TestClient_TokenFn_PreferredOverStatic(t *testing.T) {
	client := &Client{
		UserID:      "12345",
		AccessToken: "static_should_not_be_used",
		TokenFn: func(ctx context.Context) (string, error) {
			return "dynamic_token", nil
		},
		HTTPClient: &http.Client{Timeout: 5 * time.Second},
	}

	tok, err := client.getToken()
	if err != nil {
		t.Fatalf("getToken: %v", err)
	}
	if tok != "dynamic_token" {
		t.Errorf("token = %q, want dynamic_token (should prefer TokenFn)", tok)
	}
}

func TestClient_TokenFn_Error(t *testing.T) {
	client := &Client{
		UserID: "12345",
		TokenFn: func(ctx context.Context) (string, error) {
			return "", fmt.Errorf("token source unavailable")
		},
		HTTPClient: &http.Client{Timeout: 5 * time.Second},
	}

	_, err := client.getToken()
	if err == nil {
		t.Fatal("expected error from TokenFn")
	}
	if !strings.Contains(err.Error(), "token source unavailable") {
		t.Errorf("error = %v", err)
	}
}

func TestClient_RetryOnTokenExpiry(t *testing.T) {
	callCount := 0
	invalidated := false

	// Simulate: first call returns 401, after invalidation second call succeeds
	result := func(token string) ([]byte, int, error) {
		callCount++
		if token == "expired_token" {
			body, _ := json.Marshal(map[string]interface{}{
				"error": map[string]interface{}{
					"message": "token expired",
					"code":    190,
				},
			})
			return body, 400, nil
		}
		body, _ := json.Marshal(map[string]interface{}{"id": "success"})
		return body, 200, nil
	}

	tokenCallCount := 0
	client := &Client{
		UserID: "12345",
		TokenFn: func(ctx context.Context) (string, error) {
			tokenCallCount++
			if !invalidated {
				return "expired_token", nil
			}
			return "fresh_token", nil
		},
		OnTokenError: func() {
			invalidated = true
		},
		HTTPClient: &http.Client{Timeout: 5 * time.Second},
	}

	body, err := client.doWithRetry(result)
	if err != nil {
		t.Fatalf("doWithRetry: %v", err)
	}

	var parsed struct {
		ID string `json:"id"`
	}
	json.Unmarshal(body, &parsed)
	if parsed.ID != "success" {
		t.Errorf("id = %q, want success", parsed.ID)
	}
	if !invalidated {
		t.Error("OnTokenError should have been called")
	}
	if callCount != 2 {
		t.Errorf("fn called %d times, want 2 (original + retry)", callCount)
	}
}

func TestNewClientWithTokenSource(t *testing.T) {
	tokenFn := func(ctx context.Context) (string, error) {
		return "tok", nil
	}

	client, err := NewClientWithTokenSource("17841442845568912", tokenFn, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client.UserID != "17841442845568912" {
		t.Errorf("user_id = %q", client.UserID)
	}
	if client.TokenFn == nil {
		t.Error("TokenFn should be set")
	}
}

func TestNewClientWithTokenSource_InvalidUserID(t *testing.T) {
	_, err := NewClientWithTokenSource("nebtrx", func(ctx context.Context) (string, error) {
		return "tok", nil
	}, nil)
	if err == nil {
		t.Fatal("expected error for non-numeric user ID")
	}
}

func TestNewClientWithTokenSource_NilTokenFn(t *testing.T) {
	_, err := NewClientWithTokenSource("12345", nil, nil)
	if err == nil {
		t.Fatal("expected error for nil tokenFn")
	}
}

func TestIsTokenError(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   bool
	}{
		{"401", 401, `{}`, true},
		{"400 with code 190", 400, `{"error":{"code":190}}`, true},
		{"400 with other code", 400, `{"error":{"code":100}}`, false},
		{"200", 200, `{}`, false},
		{"500", 500, `{}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isTokenError(tt.status, []byte(tt.body))
			if got != tt.want {
				t.Errorf("isTokenError(%d, %q) = %v, want %v", tt.status, tt.body, got, tt.want)
			}
		})
	}
}
