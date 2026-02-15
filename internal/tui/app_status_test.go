package tui

import (
	"strings"
	"testing"
)

func TestSingleLine(t *testing.T) {
	in := "line1\nline2\r\nline3\tvalue"
	out := singleLine(in)
	if strings.Contains(out, "\n") || strings.Contains(out, "\r") || strings.Contains(out, "\t") {
		t.Fatalf("singleLine output contains control whitespace: %q", out)
	}
	if out != "line1 line2 line3 value" {
		t.Fatalf("singleLine output = %q", out)
	}
}

func TestRenderStatusBar_UsesSingleLineStatus(t *testing.T) {
	m := AppModel{statusMsg: "Pipeline error:\nline two\nline three"}
	bar := m.renderStatusBar(100)
	if strings.Contains(bar, "\n") {
		t.Fatalf("status bar contains newline: %q", bar)
	}
}
