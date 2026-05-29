// Package providers adapts concrete inventory provider implementations.
package providers

import (
	"context"
	"fmt"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/inventory"
	intsync "github.com/ntwrknrd/nssh/internal/sync"
	syncproviders "github.com/ntwrknrd/nssh/internal/sync/providers"
)

// New returns an inventory provider implementation for providerType.
func New(providerType string) (inventory.InventoryProvider, error) {
	switch providerType {
	case config.ProviderContainerlab:
		return adapter{inner: syncproviders.NewContainerlabProvider()}, nil
	case config.ProviderNetBox:
		return adapter{inner: syncproviders.NewNetBoxProvider()}, nil
	default:
		return nil, fmt.Errorf("unsupported provider %q", providerType)
	}
}

type syncProvider interface {
	Discover(context.Context, config.SyncSourceConfig, intsync.RemoteRunner) ([]intsync.InventoryObject, error)
}

type adapter struct {
	inner syncProvider
}

func (a adapter) Discover(ctx context.Context, providerName string, cfg config.InventoryProviderConfig, runner inventory.RemoteRunner) ([]inventory.Object, error) {
	objects, err := a.inner.Discover(ctx, toSyncSourceConfig(providerName, cfg), runner)
	if err != nil {
		return nil, err
	}
	out := make([]inventory.Object, 0, len(objects))
	for _, obj := range objects {
		out = append(out, inventory.Object{
			Provider:   providerName,
			ObjectID:   obj.ObjectID,
			ObjectType: obj.ObjectType,
			Name:       obj.Name,
			FQDN:       obj.FQDN,
			HostName:   obj.HostName,
			Port:       obj.Port,
			ProxyJump:  obj.ProxyJump,
			Attributes: obj.Attributes,
		})
	}
	return out, nil
}

func toSyncSourceConfig(name string, cfg config.InventoryProviderConfig) config.SyncSourceConfig {
	src := config.SyncSourceConfig{
		Name:     name,
		Provider: cfg.Type,
		Routes:   make([]config.SyncRouteConfig, 0, len(cfg.Route)),
	}
	for _, route := range cfg.Route {
		src.Routes = append(src.Routes, config.SyncRouteConfig{
			Name:    route.Name,
			Context: route.Group,
			Match:   config.SyncRouteMatch(route.Match),
		})
	}
	switch cfg.Type {
	case config.ProviderContainerlab:
		src.Containerlab = &config.ContainerlabConfig{
			JumpHost:              cfg.Config.JumpHost,
			Sudo:                  cfg.Config.Sudo,
			StrictHostKeyChecking: cfg.Config.StrictHostKeyChecking,
		}
	case config.ProviderNetBox:
		src.NetBox = &config.NetBoxConfig{
			BaseURL:  cfg.Config.BaseURL,
			URLEnv:   cfg.Config.URLEnv,
			TokenEnv: cfg.Config.TokenEnv,
			EnvFile:  cfg.Config.EnvFile,
		}
	}
	return src
}
