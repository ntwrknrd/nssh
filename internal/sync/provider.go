package sync

import (
	"context"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/ssh/remoteexec"
)

// InventoryObject is a normalized representation of a discovered object from
// an external source. All providers convert their raw payloads into this shape
// so the sync engine can process them uniformly.
type InventoryObject struct {
	Provider        string
	Source          string
	ObjectID        string
	ObjectType      string
	Name            string
	FQDN            string
	HostName        string
	Port            int
	ProxyJump       string
	UsesPassword    bool
	CredentialClass string
	Attributes      map[string][]string
}

// Provider discovers inventory objects from an external source of truth.
type Provider interface {
	Discover(ctx context.Context, source config.SyncSourceConfig, runner RemoteRunner) ([]InventoryObject, error)
}

// RemoteCommand is an alias for remoteexec.RemoteCommand.
type RemoteCommand = remoteexec.RemoteCommand

// RemoteResult is an alias for remoteexec.RemoteResult.
type RemoteResult = remoteexec.RemoteResult

// RemoteRunner executes commands on remote hosts via SSH.
type RemoteRunner interface {
	Run(ctx context.Context, host string, cmd RemoteCommand) (*RemoteResult, error)
}
