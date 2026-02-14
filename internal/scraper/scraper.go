// Package scraper extracts past Instagram captions from the user's account
// via the Instagram Graph API. It produces a style corpus JSON file used by
// the enrichment stage for style-matched caption generation.
package scraper

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

const graphAPIBase = "https://graph.instagram.com"

// Config holds scraper configuration.
type Config struct {
	AccessToken string
	UserID      string // if empty, resolved from /me
	Count       int    // target number of posts to scrape
	OutputPath  string
}

// Corpus matches the style corpus schema from the enricher package.
type Corpus struct {
	Handle       string          `json:"handle"`
	UserID       string          `json:"user_id"`
	ScrapedAt    string          `json:"scraped_at"`
	PostCount    int             `json:"post_count"`
	Captions     []CaptionEntry  `json:"captions"`
	StyleSummary *StyleSummary   `json:"style_summary,omitempty"`
}

// CaptionEntry represents a single scraped caption.
type CaptionEntry struct {
	Text      string   `json:"text"`
	PostDate  string   `json:"post_date,omitempty"`
	Hashtags  []string `json:"hashtags,omitempty"`
	MediaType string   `json:"media_type,omitempty"`
	Permalink string   `json:"permalink,omitempty"`
}

// StyleSummary provides aggregate statistics about the user's captions.
type StyleSummary struct {
	AvgCaptionLength int            `json:"avg_caption_length"`
	AvgHashtagCount  float64        `json:"avg_hashtag_count"`
	CommonHashtags   []string       `json:"common_hashtags"`
}

var hashtagRe = regexp.MustCompile(`#\w+`)

// Scrape fetches the user's media from the Instagram Graph API and builds
// a style corpus.
func Scrape(cfg Config) (*Corpus, error) {
	if cfg.AccessToken == "" {
		return nil, fmt.Errorf("INSTAGRAM_ACCESS_TOKEN is required")
	}
	if cfg.Count <= 0 {
		cfg.Count = 200
	}

	// Resolve user ID from /me if not provided
	userID := cfg.UserID
	handle := ""
	if userID == "" {
		me, err := fetchMe(cfg.AccessToken)
		if err != nil {
			return nil, fmt.Errorf("resolve user: %w", err)
		}
		userID = me.ID
		handle = me.Username
	}

	// Paginate through media
	var captions []CaptionEntry
	nextURL := fmt.Sprintf("%s/%s/media?fields=id,caption,timestamp,media_type,permalink&limit=50&access_token=%s",
		graphAPIBase, userID, cfg.AccessToken)

	for len(captions) < cfg.Count && nextURL != "" {
		page, next, err := fetchMediaPage(nextURL)
		if err != nil {
			if len(captions) > 0 {
				fmt.Fprintf(os.Stderr, "WARN: partial scrape (%d/%d posts): %v\n", len(captions), cfg.Count, err)
				break
			}
			return nil, fmt.Errorf("fetch media: %w", err)
		}

		for _, item := range page {
			if item.Caption == "" {
				continue // skip posts without captions
			}
			hashtags := hashtagRe.FindAllString(item.Caption, -1)
			captions = append(captions, CaptionEntry{
				Text:      item.Caption,
				PostDate:  item.Timestamp,
				Hashtags:  hashtags,
				MediaType: item.MediaType,
				Permalink: item.Permalink,
			})
			if len(captions) >= cfg.Count {
				break
			}
		}

		nextURL = next

		// Rate limit courtesy: pause between pages
		time.Sleep(500 * time.Millisecond)
	}

	corpus := &Corpus{
		Handle:    handle,
		UserID:    userID,
		ScrapedAt: time.Now().UTC().Format(time.RFC3339),
		PostCount: len(captions),
		Captions:  captions,
	}

	corpus.StyleSummary = computeSummary(captions)

	return corpus, nil
}

// Write serializes the corpus to a JSON file.
func (c *Corpus) Write(path string) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal corpus: %w", err)
	}
	b = append(b, '\n')
	return os.WriteFile(path, b, 0o644)
}

// --- Instagram Graph API helpers ---

type meResponse struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

func fetchMe(accessToken string) (*meResponse, error) {
	u := fmt.Sprintf("%s/me?fields=id,username&access_token=%s", graphAPIBase, url.QueryEscape(accessToken))
	resp, err := http.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GET /me returned %d: %s", resp.StatusCode, string(body))
	}

	var me meResponse
	if err := json.NewDecoder(resp.Body).Decode(&me); err != nil {
		return nil, fmt.Errorf("decode /me response: %w", err)
	}
	return &me, nil
}

type mediaItem struct {
	ID        string `json:"id"`
	Caption   string `json:"caption"`
	Timestamp string `json:"timestamp"`
	MediaType string `json:"media_type"`
	Permalink string `json:"permalink"`
}

type mediaResponse struct {
	Data   []mediaItem `json:"data"`
	Paging *struct {
		Next string `json:"next"`
	} `json:"paging"`
}

func fetchMediaPage(pageURL string) ([]mediaItem, string, error) {
	resp, err := http.Get(pageURL)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 429 {
		return nil, "", fmt.Errorf("rate limited (429) — wait and retry")
	}
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, "", fmt.Errorf("GET media returned %d: %s", resp.StatusCode, string(body))
	}

	var mr mediaResponse
	if err := json.NewDecoder(resp.Body).Decode(&mr); err != nil {
		return nil, "", fmt.Errorf("decode media response: %w", err)
	}

	next := ""
	if mr.Paging != nil {
		next = mr.Paging.Next
	}
	return mr.Data, next, nil
}

func computeSummary(captions []CaptionEntry) *StyleSummary {
	if len(captions) == 0 {
		return nil
	}

	totalLen := 0
	totalHashtags := 0
	hashtagCounts := map[string]int{}

	for _, c := range captions {
		totalLen += len(c.Text)
		totalHashtags += len(c.Hashtags)
		for _, h := range c.Hashtags {
			hashtagCounts[strings.ToLower(h)]++
		}
	}

	// Top hashtags by frequency
	type hc struct {
		tag   string
		count int
	}
	var sorted []hc
	for t, c := range hashtagCounts {
		sorted = append(sorted, hc{t, c})
	}
	// Simple sort by count descending
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j].count > sorted[i].count {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}
	var topTags []string
	for i, h := range sorted {
		if i >= 20 {
			break
		}
		topTags = append(topTags, h.tag)
	}

	return &StyleSummary{
		AvgCaptionLength: totalLen / len(captions),
		AvgHashtagCount:  float64(totalHashtags) / float64(len(captions)),
		CommonHashtags:   topTags,
	}
}
