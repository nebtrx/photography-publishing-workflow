package publisher

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestNewThreadsClientRequiresTokenSource(t *testing.T) {
	_, err := NewThreadsClient("123", nil)
	if err == nil {
		t.Fatal("expected error when token source is nil")
	}
}

func TestThreadsClientPublishTextUsesTokenSource(t *testing.T) {
	var tokenCalls int
	var createCalls int
	var publishCalls int

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/th_user/threads":
			createCalls++
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse form: %v", err)
			}
			if r.Form.Get("access_token") != "th_token" {
				http.Error(w, "bad token", http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"id":"container_1"}`))
		case "/th_user/threads_publish":
			publishCalls++
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse form: %v", err)
			}
			if r.Form.Get("access_token") != "th_token" {
				http.Error(w, "bad token", http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"id":"post_1"}`))
		case "/post_1":
			if r.URL.Query().Get("access_token") != "th_token" {
				http.Error(w, "bad token", http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"permalink":"https://www.threads.net/@x/post/1"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	tokenFn := func(context.Context) (string, error) {
		tokenCalls++
		return "th_token", nil
	}

	client, err := NewThreadsClient("th_user", tokenFn)
	if err != nil {
		t.Fatalf("NewThreadsClient: %v", err)
	}
	client.APIBase = strings.TrimRight(srv.URL, "/")
	client.HTTPClient = srv.Client()

	postID, permalink, err := client.PublishText(context.Background(), "hello threads")
	if err != nil {
		t.Fatalf("PublishText: %v", err)
	}
	if postID != "post_1" {
		t.Fatalf("postID = %q, want post_1", postID)
	}
	if permalink != "https://www.threads.net/@x/post/1" {
		t.Fatalf("permalink = %q", permalink)
	}
	if tokenCalls != 1 {
		t.Fatalf("token source calls = %d, want 1", tokenCalls)
	}
	if createCalls != 1 || publishCalls != 1 {
		t.Fatalf("container/publish calls = %d/%d, want 1/1", createCalls, publishCalls)
	}
}

func TestThreadsClientPublishTextTokenError(t *testing.T) {
	client, err := NewThreadsClient("th_user", func(context.Context) (string, error) {
		return "", context.DeadlineExceeded
	})
	if err != nil {
		t.Fatalf("NewThreadsClient: %v", err)
	}
	client.APIBase = (&url.URL{Scheme: "http", Host: "example.invalid"}).String()
	client.HTTPClient = &http.Client{}

	_, _, err = client.PublishText(context.Background(), "x")
	if err == nil {
		t.Fatal("expected token error")
	}
}
