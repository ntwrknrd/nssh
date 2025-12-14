//go:build !hardware

package self

import (
	"fmt"

	"github.com/ntwrknrd/nssh/internal/config"
)

// runRekeyPIV is a stub for non-hardware builds.
func runRekeyPIV(paths *config.Paths, cfg *config.Config) error {
	return fmt.Errorf("PIV hardware support not compiled; rebuild with: go build -tags hardware ./cmd/nssh")
}
