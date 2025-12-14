package connector

import (
	"fmt"
	"os"
	"time"
)

// TimingEnabled returns true if detailed timing instrumentation is enabled.
// Timing is enabled when NSSH_DEBUG=1 environment variable is set.
func TimingEnabled() bool {
	return os.Getenv("NSSH_DEBUG") == "1"
}

// TimingEvent represents a named timing measurement point.
// Use StartTiming() to create a new event, then call Emit() when the
// operation completes. The duration and event name will be written to
// stderr in a parseable format.
type TimingEvent struct {
	Name    string
	Started time.Time
}

// StartTiming creates a new timing event with the given name.
// The event's start time is recorded immediately.
// Call Emit() when the operation completes to log the duration.
//
// Example:
//
//	t := StartTiming("pty_start")
//	defer t.Emit()
//	// ... operation ...
func StartTiming(name string) *TimingEvent {
	return &TimingEvent{
		Name:    name,
		Started: time.Now(),
	}
}

// Emit logs the timing event to stderr if timing is enabled.
// The output format is: NSSH_TIMING:<event_name>:<duration_ms>
// This format is designed to be easily parsed by benchmark tools.
//
// If timing is not enabled (NSSH_DEBUG != "1"), this is a no-op.
func (t *TimingEvent) Emit() {
	if !TimingEnabled() {
		return
	}
	elapsed := time.Since(t.Started)
	// Format: NSSH_TIMING:<event>:<duration_ms>
	// Use 3 decimal places for sub-millisecond precision
	fmt.Fprintf(os.Stderr, "NSSH_TIMING:%s:%.3f\n", t.Name,
		float64(elapsed.Nanoseconds())/1e6)
}

// EmitWithValue logs a timing event with a custom duration value.
// Useful when timing was measured elsewhere.
func EmitWithValue(name string, duration time.Duration) {
	if !TimingEnabled() {
		return
	}
	fmt.Fprintf(os.Stderr, "NSSH_TIMING:%s:%.3f\n", name,
		float64(duration.Nanoseconds())/1e6)
}

// Timing event names used throughout the connector and CLI.
const (
	// CLI startup stages (emitted from main.go)

	// TimingConfigLoad is emitted after config file is loaded.
	TimingConfigLoad = "config_load"

	// TimingCredentialLookup is emitted after credential resolution completes.
	TimingCredentialLookup = "credential_lookup"

	// Connector stages (emitted from connector)

	// TimingPTYStart is emitted after PTY allocation completes.
	TimingPTYStart = "pty_start"

	// TimingFirstRead is emitted when first data is read from PTY (SSH banner/prompt).
	TimingFirstRead = "first_read"

	// TimingPasswordPrompt is emitted when password prompt is detected (time since session start).
	TimingPasswordPrompt = "password_prompt"

	// TimingPasswordSent is emitted after password is injected (duration of injection).
	TimingPasswordSent = "password_sent"

	// TimingSessionEnd is emitted when the session ends (total session duration).
	TimingSessionEnd = "session_end"

	// TimingTotal is emitted with the total connection time (from connector.Run() start to end).
	TimingTotal = "total"
)
