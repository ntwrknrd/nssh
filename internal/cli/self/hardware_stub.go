//go:build !hardware

package self

import (
	"fmt"

	"github.com/ntwrknrd/nssh/internal/config"
)

// initPIV returns an error indicating PIV support is not compiled in.
func initPIV(paths *config.Paths, cfg *config.Config) error {
	return fmt.Errorf("PIV support not compiled; rebuild with -tags hardware")
}
