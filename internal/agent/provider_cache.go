//go:build linux || darwin

package agent

import "errors"

// CacheOnlyProvider supports agent cache operations without vault decryption.
type CacheOnlyProvider struct{}

// NewCacheOnlyProvider creates an agent provider for external credential caches.
func NewCacheOnlyProvider() *CacheOnlyProvider {
	return &CacheOnlyProvider{}
}

func (p *CacheOnlyProvider) Mode() string {
	return ModeCache
}

func (p *CacheOnlyProvider) Decrypt([]byte) ([]byte, error) {
	return nil, errors.New("cache-only agent cannot decrypt vault credentials")
}

func (p *CacheOnlyProvider) Recipient() string {
	return ""
}

func (p *CacheOnlyProvider) Close() error {
	return nil
}

var _ Provider = (*CacheOnlyProvider)(nil)
