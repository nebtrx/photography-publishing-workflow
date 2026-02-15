package main

import (
	"bytes"
	"strings"
	"testing"

	"photography-publishing-workflow/internal/manifest"
)

func TestPrintSyndicationSummary(t *testing.T) {
	m := &manifest.Manifest{
		Publishing: &manifest.Publishing{
			Syndication: &manifest.Syndication{
				Facebook: &manifest.SyndicationTarget{
					Enabled: true,
					Status:  "failed",
					Error:   "missing page token",
				},
				Threads: &manifest.SyndicationTarget{
					Enabled:   true,
					Status:    "published",
					Permalink: "https://threads.net/t/123",
				},
			},
		},
	}

	var out bytes.Buffer
	printSyndicationSummary(&out, m)
	text := out.String()

	if !strings.Contains(text, "Syndication facebook: failed (missing page token)") {
		t.Fatalf("missing facebook summary: %q", text)
	}
	if !strings.Contains(text, "Syndication threads: published (https://threads.net/t/123)") {
		t.Fatalf("missing threads summary: %q", text)
	}
}

func TestPrintSyndicationSummary_NoData(t *testing.T) {
	var out bytes.Buffer
	printSyndicationSummary(&out, &manifest.Manifest{})
	if out.Len() != 0 {
		t.Fatalf("expected no output, got %q", out.String())
	}
}
