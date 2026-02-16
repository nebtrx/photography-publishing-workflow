package ai

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCodexCLI_Name(t *testing.T) {
	c := NewCodexCLI("", "")
	if c.Name() != "codex-cli" {
		t.Fatalf("Name() = %q, want %q", c.Name(), "codex-cli")
	}
}

func TestCodexCLI_DefaultBinaryPath(t *testing.T) {
	c := NewCodexCLI("", "")
	if c.BinaryPath != "codex" {
		t.Fatalf("BinaryPath = %q, want %q", c.BinaryPath, "codex")
	}
}

func TestCodexCLI_CustomBinaryPath(t *testing.T) {
	c := NewCodexCLI("/usr/local/bin/codex", "gpt-4o")
	if c.BinaryPath != "/usr/local/bin/codex" {
		t.Fatalf("BinaryPath = %q, want %q", c.BinaryPath, "/usr/local/bin/codex")
	}
	if c.Model != "gpt-4o" {
		t.Fatalf("Model = %q, want %q", c.Model, "gpt-4o")
	}
}

func TestBuildCodexPrompt_SystemAndUser(t *testing.T) {
	got := buildCodexPrompt("You are a helpful assistant.", "Describe this image.")
	if !strings.Contains(got, "SYSTEM INSTRUCTIONS:") {
		t.Fatal("expected SYSTEM INSTRUCTIONS header")
	}
	if !strings.Contains(got, "You are a helpful assistant.") {
		t.Fatal("expected system prompt in output")
	}
	if !strings.Contains(got, "Describe this image.") {
		t.Fatal("expected user prompt in output")
	}
	if !strings.Contains(got, "Do not create files") {
		t.Fatal("expected safety instruction")
	}
}

func TestBuildCodexPrompt_UserOnly(t *testing.T) {
	got := buildCodexPrompt("", "Just a question.")
	if strings.Contains(got, "SYSTEM INSTRUCTIONS:") {
		t.Fatal("should not have system header when system prompt is empty")
	}
	if !strings.Contains(got, "Just a question.") {
		t.Fatal("expected user prompt")
	}
}

func TestReadOutputFile_ValidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")
	if err := os.WriteFile(path, []byte("  hello world  \n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	got := readOutputFile(path)
	if got != "hello world" {
		t.Fatalf("readOutputFile = %q, want %q", got, "hello world")
	}
}

func TestReadOutputFile_MissingFile(t *testing.T) {
	got := readOutputFile("/nonexistent/path/file.txt")
	if got != "" {
		t.Fatalf("readOutputFile = %q, want empty", got)
	}
}

func TestReadOutputFile_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.txt")
	if err := os.WriteFile(path, []byte("   \n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	got := readOutputFile(path)
	if got != "" {
		t.Fatalf("readOutputFile = %q, want empty", got)
	}
}

func TestBuildCodexArgs_NoImages(t *testing.T) {
	args := buildCodexArgs("gpt-5.2", nil, "/tmp/out.txt")
	got := strings.Join(args, " ")
	if !strings.Contains(got, "--model gpt-5.2") {
		t.Fatalf("args missing model: %q", got)
	}
	if !strings.Contains(got, "--output-last-message /tmp/out.txt") {
		t.Fatalf("args missing output path: %q", got)
	}
	if !strings.HasSuffix(got, "-- -") {
		t.Fatalf("args should end with '-- -', got: %q", got)
	}
}

func TestBuildCodexArgs_WithImages(t *testing.T) {
	args := buildCodexArgs("", []string{"/tmp/a.jpg", "/tmp/b.jpg"}, "/tmp/out.txt")
	got := strings.Join(args, " ")
	if strings.Contains(got, "--image /tmp/a.jpg,/tmp/b.jpg") {
		t.Fatalf("args should not pass comma-joined images: %q", got)
	}
	if !strings.Contains(got, "--image /tmp/a.jpg") || !strings.Contains(got, "--image /tmp/b.jpg") {
		t.Fatalf("args should include each image separately: %q", got)
	}
	if !strings.HasSuffix(got, "-- -") {
		t.Fatalf("args should end with '-- -', got: %q", got)
	}
}
