package enricher

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
)

// StyleCorpus holds scraped past captions for style matching.
type StyleCorpus struct {
	Handle    string          `json:"handle"`
	UserID    string          `json:"user_id,omitempty"`
	ScrapedAt string          `json:"scraped_at"`
	PostCount int             `json:"post_count"`
	Captions  []CorpusCaption `json:"captions"`
}

// CorpusCaption represents a single scraped caption.
type CorpusCaption struct {
	Text      string   `json:"text"`
	PostDate  string   `json:"post_date,omitempty"`
	Hashtags  []string `json:"hashtags,omitempty"`
	MediaType string   `json:"media_type,omitempty"`
	Permalink string   `json:"permalink,omitempty"`
}

// LoadCorpus reads the style corpus from a JSON file.
// Returns nil (not an error) if the file doesn't exist.
func LoadCorpus(path string) (*StyleCorpus, error) {
	if path == "" {
		return nil, nil
	}

	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read corpus %s: %w", path, err)
	}

	var corpus StyleCorpus
	if err := json.Unmarshal(b, &corpus); err != nil {
		return nil, fmt.Errorf("parse corpus %s: %w", path, err)
	}
	return &corpus, nil
}

// SampleCaptions returns up to n random captions from the corpus.
// Prefers recent captions but includes some older ones for variety.
func SampleCaptions(corpus *StyleCorpus, n int) []string {
	if corpus == nil || len(corpus.Captions) == 0 {
		return nil
	}

	captions := make([]CorpusCaption, len(corpus.Captions))
	copy(captions, corpus.Captions)

	// Filter out empty captions
	var filtered []string
	for _, c := range captions {
		if c.Text != "" {
			filtered = append(filtered, c.Text)
		}
	}

	if len(filtered) == 0 {
		return nil
	}

	if len(filtered) <= n {
		return filtered
	}

	// Shuffle and take n
	rand.Shuffle(len(filtered), func(i, j int) {
		filtered[i], filtered[j] = filtered[j], filtered[i]
	})
	return filtered[:n]
}
