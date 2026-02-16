package tui

import "testing"

func TestNormalizeCaptionText_StripsTrailingNewlinesOnly(t *testing.T) {
	got := normalizeCaptionText("hello\nworld\n\n")
	if got != "hello\nworld" {
		t.Fatalf("got %q", got)
	}
}

func TestNormalizeCaptionText_NormalizesCRLF(t *testing.T) {
	got := normalizeCaptionText("hello\r\nworld\r\n")
	if got != "hello\nworld" {
		t.Fatalf("got %q", got)
	}
}
