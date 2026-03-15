package sync

import (
	"context"
	"time"

	"github.com/ntwrknrd/nssh/internal/config"
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

// RemoteCommand describes a command to execute on a remote host.
type RemoteCommand struct {
	Argv    []string
	Sudo    bool
	Timeout time.Duration
}

// RemoteResult holds the output of a remote command execution.
type RemoteResult struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

// RemoteRunner executes commands on remote hosts via SSH.
type RemoteRunner interface {
	Run(ctx context.Context, host string, cmd RemoteCommand) (*RemoteResult, error)
}
