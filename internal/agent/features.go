//go:build (linux || darwin) && !hardware

package agent

// CompiledFeatures lists optional security features compiled into this binary.
// The default build has no optional features (software mode is always available).
var CompiledFeatures []string

// PIVAvailable reports whether PIV support is compiled in.
// Returns false in non-hardware builds.
func PIVAvailable() bool { return false }
