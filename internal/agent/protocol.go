// Package agent provides the nssh background runtime over a Unix socket.
package agent

import "github.com/ntwrknrd/nssh/internal/credential/providerexec"

// ProtocolVersion is incremented when breaking changes are made to the protocol.
// Clients and agents must agree on version to communicate.
const ProtocolVersion = 1

// Request represents a message from client to agent.
type Request struct {
	Version  int              `json:"v"`                          // Protocol version (required)
	ID       string           `json:"id,omitempty"`               // Request ID for log correlation (optional)
	Op       string           `json:"op"`                         // Operation name
	Data     []byte           `json:"data,omitempty"`             // Operation payload
	Provider *ProviderRequest `json:"provider_request,omitempty"` // Credential provider request
}

// Response represents a message from agent to client.
type Response struct {
	ID       string            `json:"id,omitempty"`                // Echoes request ID for correlation
	OK       bool              `json:"ok"`                          // true if operation succeeded
	Data     []byte            `json:"data,omitempty"`              // Result data
	Provider *ProviderResponse `json:"provider_response,omitempty"` // Credential provider response
	Err      string            `json:"err,omitempty"`               // Error message if OK is false
}

// Operation constants for Request.Op field.
const (
	OpStatus          = "status" // Returns session status (JSON StatusInfo)
	OpLock            = "lock"   // Terminates the agent
	OpProviderRequest = "provider_request"
)

// ErrBitwardenNotAuthenticated is returned when a Bitwarden provider needs a
// BW_SESSION token before it can resolve credential refs.
const ErrBitwardenNotAuthenticated = providerexec.ErrBitwardenNotAuthenticated

// StatusInfo is returned by the status operation.
type StatusInfo struct {
	ProtocolVersion         int            `json:"protocol_version"`          // Agent protocol version
	PID                     int            `json:"pid"`                       // Agent process ID
	SocketPath              string         `json:"socket_path"`               // Unix socket path
	PeerVerification        string         `json:"peer_verification"`         // Peer verification mode
	IdleTimeout             int64          `json:"idle_timeout"`              // Configured idle timeout in seconds
	MaxLifetime             int64          `json:"max_lifetime"`              // Configured max lifetime in seconds
	UptimeSeconds           int64          `json:"uptime_seconds"`            // Agent uptime in seconds
	RemainingLife           int64          `json:"remaining_life"`            // Seconds until max lifetime expires
	RemainingIdle           int64          `json:"remaining_idle"`            // Seconds until idle timeout (approximate)
	CredentialProviders     int            `json:"credential_providers"`      // Configured credential provider count
	CredentialProviderNames []string       `json:"credential_provider_names"` // Configured credential provider names
	ProcessCount            int            `json:"process_count"`             // Detected active runtime agent process count
	DuplicateProcesses      bool           `json:"duplicate_processes"`       // True when more than one runtime appears active
	RSSBytes                uint64         `json:"rss_bytes,omitempty"`       // Resident memory when available
	HeapAllocBytes          uint64         `json:"heap_alloc_bytes"`          // Go heap allocation
	Goroutines              int            `json:"goroutines"`                // Current goroutine count
	OpenFDs                 int            `json:"open_fds"`                  // Open fd count, or -1 when unknown
	ProviderRequests        int64          `json:"provider_requests"`         // Total brokered provider requests
	ProviderFailures        int64          `json:"provider_failures"`         // Total brokered provider failures
	Access                  []AccessStatus `json:"access,omitempty"`          // Agent-managed retained access state
}

// ProviderRequest describes a provider-scoped operation brokered by the agent.
type ProviderRequest = providerexec.ProviderRequest

// ProviderResponse returns a request-scoped credential record.
type ProviderResponse = providerexec.ProviderResponse
