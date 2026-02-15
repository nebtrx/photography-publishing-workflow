package publisher

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	facebookGraphBase = "https://graph.facebook.com/v21.0"
	threadsGraphBase  = "https://graph.threads.net/v1.0"
)

// AccessTokenSource returns an access token on demand.
type AccessTokenSource func(ctx context.Context) (string, error)

// FacebookSyndicator posts the Instagram permalink to a Facebook Page feed.
type FacebookSyndicator interface {
	PublishLink(ctx context.Context, message, link string) (postID string, err error)
}

// ThreadsSyndicator publishes a text post to Threads.
type ThreadsSyndicator interface {
	PublishText(ctx context.Context, text string) (postID, permalink string, err error)
}

// FacebookLinkClient posts to /{page-id}/feed with message + link.
type FacebookLinkClient struct {
	PageID     string
	TokenFn    AccessTokenSource
	HTTPClient *http.Client
	APIBase    string
}

// NewFacebookLinkClient builds a Facebook feed syndicator.
func NewFacebookLinkClient(pageID string, tokenFn AccessTokenSource) (*FacebookLinkClient, error) {
	if strings.TrimSpace(pageID) == "" {
		return nil, fmt.Errorf("facebook page ID is required")
	}
	if tokenFn == nil {
		return nil, fmt.Errorf("facebook token source is required")
	}
	return &FacebookLinkClient{
		PageID:     strings.TrimSpace(pageID),
		TokenFn:    tokenFn,
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
		APIBase:    facebookGraphBase,
	}, nil
}

// PublishLink creates a Page feed post linking to the Instagram permalink.
func (c *FacebookLinkClient) PublishLink(ctx context.Context, message, link string) (string, error) {
	token, err := c.TokenFn(ctx)
	if err != nil {
		return "", fmt.Errorf("get facebook token: %w", err)
	}
	if strings.TrimSpace(token) == "" {
		return "", fmt.Errorf("empty facebook token")
	}
	if strings.TrimSpace(link) == "" {
		return "", fmt.Errorf("link is required")
	}

	params := url.Values{
		"access_token": {token},
		"link":         {link},
	}
	if strings.TrimSpace(message) != "" {
		params.Set("message", message)
	}

	u := strings.TrimRight(c.APIBase, "/") + "/" + url.PathEscape(c.PageID) + "/feed"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(params.Encode()))
	if err != nil {
		return "", fmt.Errorf("build facebook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("facebook feed publish request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("facebook feed publish failed (%d): %s", resp.StatusCode, string(body))
	}

	var out struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("parse facebook feed publish response: %w", err)
	}
	if out.ID == "" {
		return "", fmt.Errorf("facebook feed publish returned empty id")
	}
	return out.ID, nil
}

// ThreadsClient publishes text to Threads via container + publish flow.
type ThreadsClient struct {
	UserID     string
	TokenFn    AccessTokenSource
	HTTPClient *http.Client
	APIBase    string
}

// NewThreadsClient creates a Threads API client.
func NewThreadsClient(userID string, tokenFn AccessTokenSource) (*ThreadsClient, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, fmt.Errorf("threads user ID is required")
	}
	if tokenFn == nil {
		return nil, fmt.Errorf("threads token source is required")
	}
	return &ThreadsClient{
		UserID:     userID,
		TokenFn:    tokenFn,
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
		APIBase:    threadsGraphBase,
	}, nil
}

// PublishText publishes a text post and returns post ID + permalink (if available).
func (c *ThreadsClient) PublishText(ctx context.Context, text string) (string, string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", "", fmt.Errorf("threads text is required")
	}

	token, err := c.TokenFn(ctx)
	if err != nil {
		return "", "", fmt.Errorf("get threads token: %w", err)
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return "", "", fmt.Errorf("empty threads token")
	}

	containerID, err := c.createTextContainer(ctx, text, token)
	if err != nil {
		return "", "", err
	}

	postID, err := c.publishContainer(ctx, containerID, token)
	if err != nil {
		return "", "", err
	}

	permalink, _ := c.getPermalink(ctx, postID, token) // best-effort
	return postID, permalink, nil
}

func (c *ThreadsClient) createTextContainer(ctx context.Context, text, token string) (string, error) {
	params := url.Values{
		"media_type":   {"TEXT"},
		"text":         {text},
		"access_token": {token},
	}
	u := strings.TrimRight(c.APIBase, "/") + "/" + url.PathEscape(c.UserID) + "/threads"
	body, status, err := c.postForm(ctx, u, params)
	if err != nil {
		return "", fmt.Errorf("threads create container request: %w", err)
	}
	if status != 200 {
		return "", fmt.Errorf("threads create container failed (%d): %s", status, string(body))
	}

	var out struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("parse threads container response: %w", err)
	}
	if out.ID == "" {
		return "", fmt.Errorf("threads container response missing id")
	}
	return out.ID, nil
}

func (c *ThreadsClient) publishContainer(ctx context.Context, containerID, token string) (string, error) {
	params := url.Values{
		"creation_id":  {containerID},
		"access_token": {token},
	}
	u := strings.TrimRight(c.APIBase, "/") + "/" + url.PathEscape(c.UserID) + "/threads_publish"
	body, status, err := c.postForm(ctx, u, params)
	if err != nil {
		return "", fmt.Errorf("threads publish request: %w", err)
	}
	if status != 200 {
		return "", fmt.Errorf("threads publish failed (%d): %s", status, string(body))
	}

	var out struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("parse threads publish response: %w", err)
	}
	if out.ID == "" {
		return "", fmt.Errorf("threads publish response missing id")
	}
	return out.ID, nil
}

func (c *ThreadsClient) getPermalink(ctx context.Context, postID, token string) (string, error) {
	u := fmt.Sprintf("%s/%s?fields=permalink&access_token=%s",
		strings.TrimRight(c.APIBase, "/"),
		url.PathEscape(postID),
		url.QueryEscape(token),
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("threads get permalink failed (%d): %s", resp.StatusCode, string(body))
	}
	var out struct {
		Permalink string `json:"permalink"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", err
	}
	return out.Permalink, nil
}

func (c *ThreadsClient) postForm(ctx context.Context, u string, values url.Values) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(values.Encode()))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, err
	}
	return body, resp.StatusCode, nil
}
