package tui

import (
	"bytes"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
)

// EventLogWriter converts line-based writes into AppLogMsg messages.
type EventLogWriter struct {
	ch  chan<- tea.Msg
	mu  sync.Mutex
	buf bytes.Buffer
}

// NewEventLogWriter creates a writer that sends log lines into the TUI event channel.
func NewEventLogWriter(ch chan<- tea.Msg) *EventLogWriter {
	return &EventLogWriter{ch: ch}
}

func (w *EventLogWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	n, err := w.buf.Write(p)
	if err != nil {
		return n, err
	}

	for {
		data := w.buf.Bytes()
		idx := bytes.IndexByte(data, '\n')
		if idx < 0 {
			break
		}
		line := strings.TrimRight(string(data[:idx]), "\r")
		w.buf.Next(idx + 1)
		if line != "" {
			w.ch <- AppLogMsg{Line: line}
		}
	}

	return n, nil
}
