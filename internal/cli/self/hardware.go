package self

import (
	"fmt"

	"github.com/ntwrknrd/nssh/internal/agent"
	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/ui"
)

// HardwareType represents a specific hardware security implementation.
type HardwareType string

// Hardware security implementation types.
const (
	HardwareTypePIV HardwareType = "piv" // YubiKey PIV applet
	// Future: HardwareTypeFIDO2 HardwareType = "fido2"
	// Future: HardwareTypeSecureEnclave HardwareType = "secureenclave"
)

// AvailableHardwareTypes returns the list of available hardware types.
// This will expand as more implementations are added.
func AvailableHardwareTypes() []HardwareType {
	return []HardwareType{
		HardwareTypePIV,
		// Future: HardwareTypeFIDO2 (requires CGO)
	}
}

// initHardwareMode handles hardware security initialization.
// Currently only PIV is available, so no sub-selection is shown.
// When additional hardware types are added, this will present a choice.
func initHardwareMode(paths *config.Paths, cfg *config.Config) error {
	// Runtime check - works in both pure-Go and hardware builds
	if !agent.PIVAvailable() {
		ui.Error("Hardware support not compiled into this binary")
		ui.Info("Rebuild with: go build -tags hardware ./cmd/nssh")
		ui.Info("Or use: make build-hardware")
		return fmt.Errorf("hardware support not available")
	}

	available := AvailableHardwareTypes()

	// For now, only PIV is available - go directly to it
	if len(available) == 1 {
		return initPIV(paths, cfg)
	}

	// Future: when multiple hardware types exist, show selection
	// hwType, err := selectHardwareType(available)
	// switch hwType {
	// case HardwareTypePIV:
	//     return initPIV(paths, cfg)
	// case HardwareTypeFIDO2:
	//     return initFIDO2(paths, cfg)
	// }

	return initPIV(paths, cfg)
}
