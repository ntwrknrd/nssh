//go:build hardware

package agent

// CompiledFeatures lists optional security features compiled into this binary.
// Hardware build includes PIV (YubiKey) support.
var CompiledFeatures = []string{"piv"}

// PIVAvailable reports whether PIV support is compiled in.
// Returns true in hardware builds.
func PIVAvailable() bool { return true }
