//go:build unix

// Package recording provides session recording configuration, planning, and
// metadata utilities.
//
// This package handles the configuration and orchestration of SSH session
// recording using asciinema. It does not perform recording directly; instead,
// it determines whether recording should occur and provides metadata utilities.
//
// # Recording Configuration
//
// Recording behavior is controlled by:
//   - Config file: [logging.session] section
//   - Environment: NSSH_RECORDING_ENABLED, NSSH_RECORDING_DIR
//   - Host patterns: include/exclude lists for selective recording
//
// Use [LoadRecordingSettings] to get the resolved configuration.
//
// # Recording Flow
//
// When recording is enabled for a host:
//  1. The connector's [MaybeWrapWithRecording] checks settings
//  2. If recording applies, nssh re-executes itself under asciinema
//  3. The inner nssh connects normally while asciinema captures output
//  4. Recordings are saved as .cast files (asciinema v2 format)
//
// # Host Patterns
//
// Include and exclude patterns support:
//   - Exact matches: "myserver"
//   - Glob patterns: "*.example.com"
//   - All hosts: "*"
//
// Exclude patterns take precedence over include patterns.
//
// # Recording Index
//
// The package provides utilities for parsing the recording index file,
// which tracks metadata (hostname, timestamp, duration) for all recordings.
// It also owns recording archive bundle policy used by the explicit
// `nssh log archive` command.
package recording
