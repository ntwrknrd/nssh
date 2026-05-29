// Package providers contains concrete inventory provider implementations.
package providers

import (
	"fmt"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/inventory"
)

// New returns an inventory provider implementation for providerType.
func New(providerType string) (inventory.InventoryProvider, error) {
	switch providerType {
	case config.ProviderContainerlab:
		return NewContainerlabProvider(), nil
	case config.ProviderNetBox:
		return NewNetBoxProvider(), nil
	default:
		return nil, fmt.Errorf("unsupported provider %q", providerType)
	}
}
