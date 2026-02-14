// Package instagram provides a client for the Instagram Graph API,
// handling container creation, polling, publishing, and location search.
//
// Supports two token modes:
//   - Static: AccessToken field set directly (legacy, from env vars)
//   - Dynamic: TokenFn returns a Page token on demand (from meta.TokenManager)
//
// When TokenFn is set, all API calls use it and auto-retry once on 401/expired token.
package instagram

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const graphAPIBase = "https://graph.facebook.com/v21.0"

// TokenSource returns an access token. Called before each API request.
type TokenSource func(ctx context.Context) (string, error)

// Client is the Instagram Graph API client.
type Client struct {
	UserID       string
	AccessToken  string      // static token (legacy mode)
	TokenFn      TokenSource // dynamic token source (auth-managed mode)
	OnTokenError func()      // called on 401 to invalidate cached tokens
	HTTPClient   *http.Client
}

// NewClient creates an Instagram API client from environment variables.
// Uses static token mode (no auto-refresh).
func NewClient() (*Client, error) {
	userID := normalizeUserID(os.Getenv("INSTAGRAM_USER_ID"))
	token := normalizeAccessToken(os.Getenv("INSTAGRAM_ACCESS_TOKEN"))

	if userID == "" {
		return nil, fmt.Errorf("INSTAGRAM_USER_ID is required")
	}
	if !isNumericID(userID) {
		return nil, fmt.Errorf("INSTAGRAM_USER_ID must be a numeric Instagram user ID (e.g. 1784...), got %q (username/handle is not valid here)", userID)
	}
	if token == "" {
		return nil, fmt.Errorf("INSTAGRAM_ACCESS_TOKEN is required")
	}

	return &Client{
		UserID:      userID,
		AccessToken: token,
		HTTPClient:  &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// NewClientWithTokenSource creates a Client that obtains tokens dynamically.
// tokenFn is called before each API request to get the current access token.
// onTokenError is called on 401 to allow cache invalidation before retry.
func NewClientWithTokenSource(userID string, tokenFn TokenSource, onTokenError func()) (*Client, error) {
	userID = normalizeUserID(userID)
	if userID == "" {
		return nil, fmt.Errorf("INSTAGRAM_USER_ID is required")
	}
	if !isNumericID(userID) {
		return nil, fmt.Errorf("INSTAGRAM_USER_ID must be numeric, got %q", userID)
	}
	if tokenFn == nil {
		return nil, fmt.Errorf("tokenFn is required")
	}

	return &Client{
		UserID:       userID,
		TokenFn:      tokenFn,
		OnTokenError: onTokenError,
		HTTPClient:   &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// normalizeUserID trims common copy/paste artifacts from user IDs.
func normalizeUserID(raw string) string {
	id := strings.TrimSpace(raw)
	id = strings.Trim(id, `"'`)
	id = strings.TrimPrefix(id, "@")
	return id
}

func isNumericID(v string) bool {
	if v == "" {
		return false
	}
	for _, r := range v {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// normalizeAccessToken trims common copy/paste artifacts from access tokens.
func normalizeAccessToken(raw string) string {
	t := strings.TrimSpace(raw)
	t = strings.Trim(t, `"'`)
	if strings.HasPrefix(strings.ToLower(t), "bearer ") {
		t = strings.TrimSpace(t[7:])
	}
	// Tokens should not contain whitespace; collapse it defensively.
	t = strings.Join(strings.Fields(t), "")
	return t
}

// getToken returns the current access token, preferring TokenFn over static.
func (c *Client) getToken() (string, error) {
	if c.TokenFn != nil {
		return c.TokenFn(context.Background())
	}
	if c.AccessToken == "" {
		return "", fmt.Errorf("no access token configured")
	}
	return c.AccessToken, nil
}

// tokenSuffix returns the last 4 chars of the token for safe logging.
func (c *Client) tokenSuffix() string {
	tok, err := c.getToken()
	if err != nil || len(tok) < 4 {
		return "****"
	}
	return tok[len(tok)-4:]
}

// --- HTTP helpers with token-retry ---

// doGet performs a GET request with token auth. On 401/OAuthException code 190,
// invalidates token cache and retries once.
func (c *Client) doGet(path string, extraParams url.Values) ([]byte, error) {
	return c.doWithRetry(func(token string) ([]byte, int, error) {
		params := url.Values{"access_token": {token}}
		for k, v := range extraParams {
			params[k] = v
		}
		u := fmt.Sprintf("%s/%s?%s", graphAPIBase, path, params.Encode())

		resp, err := c.HTTPClient.Get(u)
		if err != nil {
			return nil, 0, err
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return body, resp.StatusCode, nil
	})
}

// doPost performs a POST (form-encoded) with token auth. On 401/OAuthException code 190,
// invalidates token cache and retries once.
func (c *Client) doPost(path string, params url.Values) ([]byte, error) {
	return c.doWithRetry(func(token string) ([]byte, int, error) {
		params.Set("access_token", token)
		u := fmt.Sprintf("%s/%s", graphAPIBase, path)

		resp, err := c.HTTPClient.PostForm(u, params)
		if err != nil {
			return nil, 0, err
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return body, resp.StatusCode, nil
	})
}

// doWithRetry gets a token, executes fn, and retries once on token expiry.
func (c *Client) doWithRetry(fn func(token string) (body []byte, status int, err error)) ([]byte, error) {
	tok, err := c.getToken()
	if err != nil {
		return nil, fmt.Errorf("get token: %w", err)
	}

	body, status, err := fn(tok)
	if err != nil {
		return nil, err
	}

	if isTokenError(status, body) && c.TokenFn != nil && c.OnTokenError != nil {
		// Invalidate cache and retry once
		c.OnTokenError()
		tok, err = c.getToken()
		if err != nil {
			return nil, fmt.Errorf("token refresh failed: %w", err)
		}
		body, status, err = fn(tok)
		if err != nil {
			return nil, err
		}
	}

	if status == 429 {
		return nil, fmt.Errorf("rate limited (429)")
	}
	if status != 200 {
		return nil, parseAPIError(status, body)
	}

	return body, nil
}

// isTokenError returns true if the response indicates an expired/invalid token.
func isTokenError(status int, body []byte) bool {
	if status == 401 {
		return true
	}
	// Meta returns 400 with OAuthException code 190 for expired tokens
	if status == 400 {
		var errResp struct {
			Error struct {
				Code int `json:"code"`
			} `json:"error"`
		}
		if json.Unmarshal(body, &errResp) == nil && errResp.Error.Code == 190 {
			return true
		}
	}
	return false
}

// parseAPIError extracts error details from a Meta API error response.
func parseAPIError(statusCode int, body []byte) error {
	var errResp struct {
		Error struct {
			Message  string `json:"message"`
			Type     string `json:"type"`
			Code     int    `json:"code"`
			FbTrace  string `json:"fbtrace_id"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &errResp) == nil && errResp.Error.Message != "" {
		msg := fmt.Sprintf("Meta API %d: %s (type=%s, code=%d",
			statusCode, errResp.Error.Message, errResp.Error.Type, errResp.Error.Code)
		if errResp.Error.FbTrace != "" {
			msg += ", fbtrace_id=" + errResp.Error.FbTrace
		}
		msg += ")"
		return fmt.Errorf("%s", msg)
	}
	return fmt.Errorf("API returned %d: %s", statusCode, string(body))
}

// --- Public API methods (InstagramAPI interface) ---

// VerifyToken checks that the access token is valid by calling GET /{userID}.
func (c *Client) VerifyToken() (string, error) {
	body, err := c.doGet(url.PathEscape(c.UserID), url.Values{"fields": {"id"}})
	if err != nil {
		return "", fmt.Errorf("pre-flight token check: %w", err)
	}

	var result struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("decode /%s: %w", c.UserID, err)
	}
	if result.ID == "" {
		return "", fmt.Errorf("verify token: empty id returned for user %s", c.UserID)
	}
	return result.ID, nil
}

// CreateChildContainer creates a carousel child container for a single image.
func (c *Client) CreateChildContainer(imageURL string) (string, error) {
	params := url.Values{
		"image_url":        {imageURL},
		"is_carousel_item": {"true"},
	}
	return c.createContainer(params)
}

// CreateSingleContainer creates a single-image post container.
func (c *Client) CreateSingleContainer(imageURL, caption, locationID string) (string, error) {
	params := url.Values{
		"image_url": {imageURL},
		"caption":   {caption},
	}
	if locationID != "" {
		params.Set("location_id", locationID)
	}
	return c.createContainer(params)
}

// CreateCarouselContainer creates a carousel container from child container IDs.
func (c *Client) CreateCarouselContainer(childIDs []string, caption, locationID string) (string, error) {
	params := url.Values{
		"media_type": {"CAROUSEL"},
		"children":   {strings.Join(childIDs, ",")},
		"caption":    {caption},
	}
	if locationID != "" {
		params.Set("location_id", locationID)
	}
	return c.createContainer(params)
}

// CreateStoryContainer creates a story container for the hero image.
func (c *Client) CreateStoryContainer(imageURL string) (string, error) {
	params := url.Values{
		"media_type": {"STORIES"},
		"image_url":  {imageURL},
	}
	return c.createContainer(params)
}

func (c *Client) createContainer(params url.Values) (string, error) {
	body, err := c.doPost(c.UserID+"/media", params)
	if err != nil {
		return "", fmt.Errorf("create container: %w", err)
	}

	var result struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("decode container response: %w", err)
	}
	if result.ID == "" {
		return "", fmt.Errorf("empty container ID in response: %s", string(body))
	}
	return result.ID, nil
}

// ContainerStatus represents the status of a media container.
type ContainerStatus struct {
	ID        string `json:"id"`
	Status    string `json:"status_code"`
	StatusMsg string `json:"status,omitempty"`
}

// PollContainer polls a container until it reaches FINISHED status.
// Polls every pollInterval, up to maxPolls times.
func (c *Client) PollContainer(containerID string, pollInterval time.Duration, maxPolls int) error {
	for i := 0; i < maxPolls; i++ {
		status, err := c.getContainerStatus(containerID)
		if err != nil {
			return fmt.Errorf("poll container %s: %w", containerID, err)
		}

		switch status.Status {
		case "FINISHED":
			return nil
		case "ERROR":
			return fmt.Errorf("container %s failed: %s", containerID, status.StatusMsg)
		case "IN_PROGRESS", "":
			time.Sleep(pollInterval)
		default:
			time.Sleep(pollInterval)
		}
	}
	return fmt.Errorf("container %s timed out after %d polls", containerID, maxPolls)
}

func (c *Client) getContainerStatus(containerID string) (*ContainerStatus, error) {
	body, err := c.doGet(containerID, url.Values{"fields": {"status_code,status"}})
	if err != nil {
		return nil, fmt.Errorf("get container status: %w", err)
	}

	var status ContainerStatus
	if err := json.Unmarshal(body, &status); err != nil {
		return nil, fmt.Errorf("decode status: %w", err)
	}
	return &status, nil
}

// Publish publishes a media container and returns the media ID.
func (c *Client) Publish(containerID string) (string, error) {
	params := url.Values{
		"creation_id": {containerID},
	}
	body, err := c.doPost(c.UserID+"/media_publish", params)
	if err != nil {
		return "", fmt.Errorf("publish: %w", err)
	}

	var result struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("decode publish response: %w", err)
	}
	return result.ID, nil
}

// GetPermalink fetches the permalink for a published post.
func (c *Client) GetPermalink(mediaID string) (string, error) {
	body, err := c.doGet(mediaID, url.Values{"fields": {"permalink"}})
	if err != nil {
		return "", fmt.Errorf("get permalink: %w", err)
	}

	var result struct {
		Permalink string `json:"permalink"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("decode permalink: %w", err)
	}
	return result.Permalink, nil
}

// SearchLocation searches for an Instagram location by name.
// Uses the Facebook Pages Search API.
func (c *Client) SearchLocation(query string) (string, string, error) {
	body, err := c.doGet("search", url.Values{
		"type":   {"place"},
		"q":      {query},
		"fields": {"name,location"},
	})
	if err != nil {
		return "", "", fmt.Errorf("search location: %w", err)
	}

	var result struct {
		Data []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", "", fmt.Errorf("decode location search: %w", err)
	}

	if len(result.Data) == 0 {
		return "", "", nil
	}

	return result.Data[0].ID, result.Data[0].Name, nil
}
