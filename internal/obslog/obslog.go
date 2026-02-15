package obslog

import (
	"encoding/json"
	"log"
	"time"
)

// Event is the structured runtime logging envelope.
type Event struct {
	TS         time.Time      `json:"ts"`
	Type       string         `json:"type"` // intent | result
	Module     string         `json:"module"`
	JobID      string         `json:"job_id,omitempty"`
	PostID     string         `json:"post_id,omitempty"`
	Action     string         `json:"action"`
	Intent     string         `json:"intent,omitempty"`
	Outcome    string         `json:"outcome,omitempty"` // success | failure
	Success    *bool          `json:"success,omitempty"`
	DurationMS int64          `json:"duration_ms,omitempty"`
	Error      string         `json:"error,omitempty"`
	Details    map[string]any `json:"details,omitempty"`
}

// Intent logs a structured action-intent event and returns the start time.
func Intent(logger *log.Logger, module, jobID, postID, action, intent string, details map[string]any) time.Time {
	startedAt := time.Now().UTC()
	emit(logger, Event{
		TS:      startedAt,
		Type:    "intent",
		Module:  module,
		JobID:   jobID,
		PostID:  postID,
		Action:  action,
		Intent:  intent,
		Details: details,
	})
	return startedAt
}

// Result logs a structured result event for an action started at `startedAt`.
func Result(logger *log.Logger, module, jobID, postID, action string, startedAt time.Time, err error, details map[string]any) {
	success := err == nil
	outcome := "success"
	errText := ""
	if !success {
		outcome = "failure"
		errText = err.Error()
	}
	end := time.Now().UTC()
	duration := int64(0)
	if !startedAt.IsZero() {
		duration = end.Sub(startedAt).Milliseconds()
	}
	emit(logger, Event{
		TS:         end,
		Type:       "result",
		Module:     module,
		JobID:      jobID,
		PostID:     postID,
		Action:     action,
		Outcome:    outcome,
		Success:    &success,
		DurationMS: duration,
		Error:      errText,
		Details:    details,
	})
}

func emit(logger *log.Logger, event Event) {
	if logger == nil {
		return
	}
	if event.TS.IsZero() {
		event.TS = time.Now().UTC()
	}
	b, err := json.Marshal(event)
	if err != nil {
		logger.Printf(`{"type":"log_marshal_error","error":%q}`, err.Error())
		return
	}
	logger.Printf("%s", string(b))
}
