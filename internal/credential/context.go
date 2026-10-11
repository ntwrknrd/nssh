package credential

import (
	"context"

	"github.com/ntwrknrd/nssh/internal/config"
)

// GetRefContext binds one automatic lookup to cancellation without mutating the
// shared provider or allowing a provider login to take over terminal input.
func GetRefContext(ctx context.Context, provider Provider, ref config.CredentialRefConfig) (*Record, error) {
	switch p := provider.(type) {
	case *sopsAgeProvider:
		copy := *p
		copy.transport = commandTransport(ctx, copy.transport)
		return copy.GetRef(ref)
	case *onePasswordProvider:
		copy := *p
		copy.transport = commandTransport(ctx, copy.transport)
		return copy.GetRef(ref)
	case *bitwardenProvider:
		copy := *p
		copy.noUnlock = true
		copy.transport = commandTransport(ctx, copy.transport)
		return copy.GetRef(ref)
	default:
		return provider.GetRef(ref)
	}
}
