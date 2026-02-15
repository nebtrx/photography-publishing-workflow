package joblog

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultLogDir        = "~/.ppw/logs"
	defaultSuccessTTL    = 24 * time.Hour
	defaultFailedTTL     = 30 * 24 * time.Hour
	defaultSweepInterval = 60 * time.Minute
)

// Config controls job log pathing and retention policy.
type Config struct {
	LogDir        string
	SuccessTTL    time.Duration
	FailedTTL     time.Duration
	SweepInterval time.Duration
}

// Session writes one job log file and finalizes it with success/failed status.
type Session struct {
	cfg        Config
	module     string
	jobID      string
	activePath string
	finalPath  string
	file       *os.File
	writer     io.Writer
	mu         sync.Mutex
	closed     bool
}

// DefaultConfig returns default retention/log settings.
func DefaultConfig() Config {
	return Config{
		LogDir:        defaultLogDir,
		SuccessTTL:    defaultSuccessTTL,
		FailedTTL:     defaultFailedTTL,
		SweepInterval: defaultSweepInterval,
	}
}

// NewSession creates a new per-job log file session.
func NewSession(cfg Config, module, jobID string, base io.Writer) (*Session, error) {
	logDir, err := expandHome(cfg.LogDir)
	if err != nil {
		return nil, err
	}
	jobsDir := filepath.Join(logDir, "jobs")
	if err := os.MkdirAll(jobsDir, 0o700); err != nil {
		return nil, fmt.Errorf("create jobs log directory: %w", err)
	}

	if strings.TrimSpace(jobID) == "" {
		jobID = GenerateJobID(module)
	}
	activePath := filepath.Join(jobsDir, jobID+".active.jsonl")
	f, err := os.OpenFile(activePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open job log file: %w", err)
	}

	if base == nil {
		base = io.Discard
	}

	return &Session{
		cfg:        cfg,
		module:     module,
		jobID:      jobID,
		activePath: activePath,
		file:       f,
		writer:     io.MultiWriter(base, f),
	}, nil
}

func (s *Session) JobID() string { return s.jobID }

func (s *Session) ActivePath() string { return s.activePath }

func (s *Session) FinalPath() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.finalPath
}

// Writer returns an io.Writer that mirrors into this job file.
func (s *Session) Writer() io.Writer { return s.writer }

// Close finalizes the job log by renaming active file to success/failed path.
func (s *Session) Close(success bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true

	status := "failed"
	if success {
		status = "success"
	}
	completedUnix := time.Now().UTC().Unix()
	finalName := fmt.Sprintf("%s.%s.%d.jsonl", s.jobID, status, completedUnix)
	finalPath := filepath.Join(filepath.Dir(s.activePath), finalName)

	if err := s.file.Close(); err != nil {
		return err
	}
	if err := os.Rename(s.activePath, finalPath); err != nil {
		return fmt.Errorf("finalize job log file: %w", err)
	}
	s.finalPath = finalPath
	return nil
}

// Sweep removes completed job logs exceeding their retention TTL.
func Sweep(cfg Config, now time.Time) (int, error) {
	logDir, err := expandHome(cfg.LogDir)
	if err != nil {
		return 0, err
	}
	jobsDir := filepath.Join(logDir, "jobs")
	entries, err := os.ReadDir(jobsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("read jobs log directory: %w", err)
	}

	removed := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		status, completedAt, ok := parseCompletedName(name)
		if !ok {
			continue
		}
		var ttl time.Duration
		switch status {
		case "success":
			ttl = cfg.SuccessTTL
		case "failed":
			ttl = cfg.FailedTTL
		default:
			continue
		}
		if ttl < 0 {
			continue // disabled retention for this status
		}
		if now.Sub(completedAt) < ttl {
			continue
		}
		if err := os.Remove(filepath.Join(jobsDir, name)); err == nil {
			removed++
		}
	}

	return removed, nil
}

// StartPeriodicSweep runs retention cleanup on an interval until ctx cancellation.
func StartPeriodicSweep(ctx context.Context, cfg Config, logOutput io.Writer) {
	if cfg.SweepInterval <= 0 {
		cfg.SweepInterval = defaultSweepInterval
	}
	if logOutput == nil {
		logOutput = io.Discard
	}

	ticker := time.NewTicker(cfg.SweepInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			removed, err := Sweep(cfg, now)
			if err != nil {
				fmt.Fprintf(logOutput, "[log-retention] sweep error: %v\n", err)
				continue
			}
			if removed > 0 {
				fmt.Fprintf(logOutput, "[log-retention] sweep removed %d expired job logs\n", removed)
			}
		}
	}
}

// SweepNow executes one cleanup pass immediately.
func SweepNow(cfg Config, logOutput io.Writer) {
	if logOutput == nil {
		logOutput = io.Discard
	}
	removed, err := Sweep(cfg, time.Now().UTC())
	if err != nil {
		fmt.Fprintf(logOutput, "[log-retention] sweep error: %v\n", err)
		return
	}
	if removed > 0 {
		fmt.Fprintf(logOutput, "[log-retention] sweep removed %d expired job logs\n", removed)
	}
}

func GenerateJobID(module string) string {
	module = sanitize(module)
	ts := time.Now().UTC().Format("20060102T150405Z")
	randSuffix := "0000"
	b := make([]byte, 2)
	if _, err := rand.Read(b); err == nil {
		randSuffix = fmt.Sprintf("%02x%02x", b[0], b[1])
	}
	if module == "" {
		module = "job"
	}
	return fmt.Sprintf("%s-%s-%s", module, ts, randSuffix)
}

func sanitize(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "/", "-")
	s = strings.ReplaceAll(s, "\\", "-")
	if s == "" {
		return "job"
	}
	return s
}

func parseCompletedName(name string) (status string, completedAt time.Time, ok bool) {
	if strings.HasSuffix(name, ".active.jsonl") {
		return "", time.Time{}, false
	}
	parts := strings.Split(name, ".")
	if len(parts) < 4 {
		return "", time.Time{}, false
	}
	if parts[len(parts)-1] != "jsonl" {
		return "", time.Time{}, false
	}
	status = parts[len(parts)-3]
	tsStr := parts[len(parts)-2]
	unixSec, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		return "", time.Time{}, false
	}
	return status, time.Unix(unixSec, 0).UTC(), true
}

func expandHome(path string) (string, error) {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		if path == "~" {
			return home, nil
		}
		return filepath.Join(home, strings.TrimPrefix(path, "~/")), nil
	}
	return path, nil
}
