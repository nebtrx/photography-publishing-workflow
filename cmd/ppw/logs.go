package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"photography-publishing-workflow/internal/joblog"
	"photography-publishing-workflow/internal/obslog"
)

type logQuery struct {
	JobID   string
	PostID  string
	Module  string
	Action  string
	Outcome string
	Since   time.Time
	Limit   int
}

type logEntry struct {
	Event obslog.Event
	File  string
	Line  int
}

func logsCmd() *cobra.Command {
	var (
		jobID      string
		postID     string
		module     string
		action     string
		outcome    string
		since      time.Duration
		limit      int
		jsonOutput bool
	)

	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Inspect structured runtime/job logs",
		Long: `Reads structured events from per-job logs and prints the most recent entries.

Examples:
  ppw logs --limit 100
  ppw logs --post-id rotterdam-snow-sun --module publisher
  ppw logs --outcome failure --since 24h
  ppw logs --job-id publish-20260215T200001Z-ab12 --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			q := logQuery{
				JobID:   strings.TrimSpace(jobID),
				PostID:  strings.TrimSpace(postID),
				Module:  strings.TrimSpace(module),
				Action:  strings.TrimSpace(action),
				Outcome: strings.TrimSpace(strings.ToLower(outcome)),
				Limit:   limit,
			}
			if since > 0 {
				q.Since = time.Now().UTC().Add(-since)
			}

			entries, err := collectLogEntries(jobLogConfig(cfg), q)
			if err != nil {
				return err
			}
			if len(entries) == 0 {
				fmt.Println("No log events matched filters.")
				return nil
			}

			for _, entry := range entries {
				if jsonOutput {
					line, err := json.Marshal(entry.Event)
					if err != nil {
						return err
					}
					fmt.Println(string(line))
					continue
				}
				fmt.Println(formatLogEntry(entry))
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&jobID, "job-id", "", "Filter by job ID")
	cmd.Flags().StringVar(&postID, "post-id", "", "Filter by post ID")
	cmd.Flags().StringVar(&module, "module", "", "Filter by module (pipeline/enricher/publisher/watcher)")
	cmd.Flags().StringVar(&action, "action", "", "Filter by action")
	cmd.Flags().StringVar(&outcome, "outcome", "", "Filter by outcome (success/failure)")
	cmd.Flags().DurationVar(&since, "since", 0, "Only include events within this duration (e.g. 24h, 30m)")
	cmd.Flags().IntVar(&limit, "limit", 200, "Maximum number of events to print (most recent)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Print raw JSON events")

	return cmd
}

func collectLogEntries(cfg joblog.Config, q logQuery) ([]logEntry, error) {
	logDir, err := expandHome(cfg.LogDir)
	if err != nil {
		return nil, err
	}
	jobsDir := filepath.Join(logDir, "jobs")

	entries, err := os.ReadDir(jobsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read jobs log directory: %w", err)
	}

	files := make([]string, 0, len(entries))
	for _, de := range entries {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".jsonl") {
			continue
		}
		files = append(files, filepath.Join(jobsDir, de.Name()))
	}
	sort.Strings(files)

	results := make([]logEntry, 0, 256)
	for _, path := range files {
		batch, err := collectFromFile(path, q)
		if err != nil {
			return nil, err
		}
		results = append(results, batch...)
	}

	sort.SliceStable(results, func(i, j int) bool {
		ti := results[i].Event.TS
		tj := results[j].Event.TS
		if ti.Equal(tj) {
			if results[i].File == results[j].File {
				return results[i].Line < results[j].Line
			}
			return results[i].File < results[j].File
		}
		return ti.Before(tj)
	})

	if q.Limit > 0 && len(results) > q.Limit {
		results = results[len(results)-q.Limit:]
	}

	return results, nil
}

func collectFromFile(path string, q logQuery) ([]logEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open log file %s: %w", path, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	filename := filepath.Base(path)
	derivedJobID := deriveJobIDFromFilename(filename)

	out := make([]logEntry, 0, 64)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		raw := strings.TrimSpace(scanner.Text())
		if raw == "" {
			continue
		}
		event, ok := parseEventFromLine(raw)
		if !ok {
			continue
		}
		if event.JobID == "" {
			event.JobID = derivedJobID
		}

		if !matchLogQuery(event, q) {
			continue
		}
		out = append(out, logEntry{Event: event, File: filename, Line: lineNo})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan log file %s: %w", path, err)
	}
	return out, nil
}

func parseEventFromLine(raw string) (obslog.Event, bool) {
	var event obslog.Event

	// Fast path: full line is JSON.
	if json.Unmarshal([]byte(raw), &event) == nil {
		return event, true
	}

	// Runtime/job log lines are often prefixed, e.g.
	// "[pipeline] 2026/... { ...json... }"
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return obslog.Event{}, false
	}

	candidate := strings.TrimSpace(raw[start : end+1])
	if candidate == "" {
		return obslog.Event{}, false
	}
	if json.Unmarshal([]byte(candidate), &event) != nil {
		return obslog.Event{}, false
	}

	return event, true
}

func matchLogQuery(event obslog.Event, q logQuery) bool {
	if q.JobID != "" && event.JobID != q.JobID {
		return false
	}
	if q.PostID != "" && event.PostID != q.PostID {
		return false
	}
	if q.Module != "" && !strings.EqualFold(event.Module, q.Module) {
		return false
	}
	if q.Action != "" && event.Action != q.Action {
		return false
	}
	if q.Outcome != "" && !strings.EqualFold(event.Outcome, q.Outcome) {
		return false
	}
	if !q.Since.IsZero() && !event.TS.IsZero() && event.TS.Before(q.Since) {
		return false
	}
	return true
}

func deriveJobIDFromFilename(name string) string {
	if strings.HasSuffix(name, ".active.jsonl") {
		return strings.TrimSuffix(name, ".active.jsonl")
	}
	if !strings.HasSuffix(name, ".jsonl") {
		return ""
	}
	parts := strings.Split(name, ".")
	if len(parts) >= 4 {
		return strings.Join(parts[:len(parts)-3], ".")
	}
	return strings.TrimSuffix(name, ".jsonl")
}

func formatLogEntry(entry logEntry) string {
	e := entry.Event
	ts := "0001-01-01T00:00:00Z"
	if !e.TS.IsZero() {
		ts = e.TS.UTC().Format(time.RFC3339)
	}

	segments := []string{
		ts,
		fmt.Sprintf("module=%s", valueOrDash(e.Module)),
		fmt.Sprintf("job=%s", valueOrDash(e.JobID)),
		fmt.Sprintf("post=%s", valueOrDash(e.PostID)),
		fmt.Sprintf("action=%s", valueOrDash(e.Action)),
		fmt.Sprintf("type=%s", valueOrDash(e.Type)),
	}
	if e.Intent != "" {
		segments = append(segments, fmt.Sprintf("intent=%q", e.Intent))
	}
	if e.Outcome != "" {
		segments = append(segments, fmt.Sprintf("outcome=%s", e.Outcome))
	}
	if e.Success != nil {
		segments = append(segments, fmt.Sprintf("success=%t", *e.Success))
	}
	if e.DurationMS > 0 {
		segments = append(segments, fmt.Sprintf("duration_ms=%d", e.DurationMS))
	}
	if e.Error != "" {
		segments = append(segments, fmt.Sprintf("error=%q", e.Error))
	}
	if len(e.Details) > 0 {
		b, _ := json.Marshal(e.Details)
		segments = append(segments, fmt.Sprintf("details=%s", string(b)))
	}
	segments = append(segments, fmt.Sprintf("src=%s:%d", entry.File, entry.Line))

	return strings.Join(segments, " ")
}

func valueOrDash(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "-"
	}
	return s
}
