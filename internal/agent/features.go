//go:build linux || darwin

package agent

// CompiledFeatures lists optional security features compiled into this binary.
// nssh currently ships as a pure-Go software build, so there are no optional
// compiled-in credential features.
var CompiledFeatures []string
