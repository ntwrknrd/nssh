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

	// TimingCatalogTotal is emitted after the host catalog is built.
	TimingCatalogTotal = "catalog_total"

	// TimingProviderStateList is emitted after provider state files are listed.
	TimingProviderStateList = "provider_state_list"

	// TimingProviderStateLoad is emitted after provider state files are loaded.
	TimingProviderStateLoad = "provider_state_load"

	// TimingCatalogLocalHosts is emitted after local inventory hosts are added to the catalog.
	TimingCatalogLocalHosts = "catalog_local_hosts"

	// TimingCatalogProviderHosts is emitted after provider state hosts are added to the catalog.
	TimingCatalogProviderHosts = "catalog_provider_hosts"

	// TimingAuthResolve is emitted after inventory auth inheritance is resolved.
	TimingAuthResolve = "auth_resolve"

	// TimingCredentialRegistry is emitted after credential provider registry construction completes.
	TimingCredentialRegistry = "credential_registry"

	// TimingCredentialLookup is emitted after credential resolution completes.
	TimingCredentialLookup = "credential_lookup"

	// TimingCredentialLookupLazy is emitted when a deferred password resolver runs.
	TimingCredentialLookupLazy = "credential_lookup_lazy"

	// TimingAskpassSetup is emitted after remote-command askpass setup completes.
	TimingAskpassSetup = "askpass_setup"

	// TimingSSHArgsBuild is emitted after OpenSSH argv construction completes.
	TimingSSHArgsBuild = "ssh_args_build"

	// TimingSSHProcessStart is emitted after the ssh subprocess starts.
	TimingSSHProcessStart = "ssh_process_start"

	// TimingSSHProcessWait is emitted after ssh output drain and process wait complete.
	TimingSSHProcessWait = "ssh_process_wait"

	// TimingSSHProcessTotal is emitted after the full ssh subprocess lifecycle completes.
	TimingSSHProcessTotal = "ssh_process_total"

	// Connector stages (emitted from connector)

	// TimingPTYStart is emitted after PTY allocation completes.
	TimingPTYStart = "pty_start"

	// TimingFirstRead is emitted when first data is read from PTY (SSH banner/prompt).
	TimingFirstRead = "first_read"

	// TimingPasswordPrompt is emitted when password prompt is detected (time since session start).
	TimingPasswordPrompt = "password_prompt"

	// TimingPasswordSent is emitted after password is injected (duration of injection).
	TimingPasswordSent = "password_sent"

	// TimingPasswordWrite is emitted for PTY password write time after lookup is complete.
	TimingPasswordWrite = "password_write"

	// TimingSessionEnd is emitted when the session ends (total session duration).
	TimingSessionEnd = "session_end"

	// TimingTotal is emitted with the total connection time (from connector.Run() start to end).
	TimingTotal = "total"
)
