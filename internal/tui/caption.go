package tui

import "strings"

// normalizeCaptionText preserves intentional multiline captions but removes
// accidental trailing newlines often introduced by terminal editors.
func normalizeCaptionText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.TrimRight(s, "\n\r")
}
