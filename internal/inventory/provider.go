// Package inventory implements SSH-config-backed inventory providers.
package inventory

import (
	"context"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/ssh/remoteexec"
)

// Object is a normalized object discovered by an inventory provider.
type Object struct {
	Provider   string
	ObjectID   string
	ObjectType string
	Name       string
	FQDN       string
	HostName   string
	Port       int
	ProxyJump  string
	Attributes map[string][]string
}

// InventoryProvider discovers inventory objects from an external source.
type InventoryProvider interface {
	Discover(ctx context.Context, providerName string, cfg config.InventoryProviderConfig, runner RemoteRunner) ([]Object, error)
}

// RemoteCommand is an alias for remoteexec.RemoteCommand.
type RemoteCommand = remoteexec.RemoteCommand

// RemoteResult is an alias for remoteexec.RemoteResult.
type RemoteResult = remoteexec.RemoteResult

// RemoteRunner executes commands on remote hosts via SSH.
type RemoteRunner interface {
	Run(ctx context.Context, host string, cmd RemoteCommand) (*RemoteResult, error)
}
