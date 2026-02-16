// Package ai defines the AI provider interface and implementations for
// generating captions and identifying locations.
package ai

import "context"

// Request represents an AI prompt with optional image attachments.
type Request struct {
	SystemPrompt string   // system-level instructions
	UserPrompt   string   // user-level prompt text
	ImagePaths   []string // local file paths to images to attach
}

// Response represents the AI provider's text response.
type Response struct {
	Text  string // raw response text
	Model string // model identifier that produced the response
}

// Provider is the interface for AI backends. Implementations must be
// replaceable (Claude CLI, OpenRouter, etc.).
type Provider interface {
	// Generate sends a prompt (with optional images) and returns the response text.
	Generate(ctx context.Context, req Request) (*Response, error)

	// Name returns a human-readable identifier for this provider.
	Name() string
}
