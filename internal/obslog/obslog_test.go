package obslog

import (
	"bytes"
	"errors"
	"log"
	"strings"
	"testing"
	"time"
)

func TestIntentAndResult(t *testing.T) {
	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)

	start := Intent(logger, "pipeline", "job-1", "post-1", "scan", "scan post directory", map[string]any{"dir": "/tmp/post"})
	Result(logger, "pipeline", "job-1", "post-1", "scan", start, nil, map[string]any{"image_count": 3})

	out := buf.String()
	if !strings.Contains(out, `"type":"intent"`) {
		t.Fatalf("missing intent event: %q", out)
	}
	if !strings.Contains(out, `"type":"result"`) {
		t.Fatalf("missing result event: %q", out)
	}
	if !strings.Contains(out, `"outcome":"success"`) {
		t.Fatalf("missing success outcome: %q", out)
	}
}

func TestResultFailure(t *testing.T) {
	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)

	Result(logger, "publisher", "job-2", "post-2", "publish_threads", time.Now().Add(-250*time.Millisecond), errors.New("boom"), nil)
	out := buf.String()
	if !strings.Contains(out, `"outcome":"failure"`) {
		t.Fatalf("missing failure outcome: %q", out)
	}
	if !strings.Contains(out, `"error":"boom"`) {
		t.Fatalf("missing error message: %q", out)
	}
}
