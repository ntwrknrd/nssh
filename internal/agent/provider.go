//go:build linux || darwin

package agent

import (
	"context"
)

// Provider abstracts an agent runtime.
type Provider interface {
	// Close zeroizes secrets and releases any resources held by the provider.
	// Must be called when the agent shuts down.
	Close() error
}

// SessionProvider marks runtime providers that broker external credential
// provider requests.
type SessionProvider interface {
	HandleProviderRequest(ctx context.Context, req ProviderRequest) (ProviderResponse, error)
}
