// Package agent provides the nssh background runtime over a Unix socket.
package agent

// ProtocolVersion is incremented when breaking changes are made to the protocol.
// Clients and agents must agree on version to communicate.
const ProtocolVersion = 1

// Request represents a message from client to agent.
type Request struct {
	Version  int              `json:"v"`                          // Protocol version (required)
	ID       string           `json:"id,omitempty"`               // Request ID for log correlation (optional)
	Op       string           `json:"op"`                         // Operation name
	Data     []byte           `json:"data,omitempty"`             // Operation payload
	Key      string           `json:"key,omitempty"`              // Metadata cache key
	Provider *ProviderRequest `json:"provider_request,omitempty"` // Provider-session request
}

// Response represents a message from agent to client.
type Response struct {
	ID       string            `json:"id,omitempty"`                // Echoes request ID for correlation
	OK       bool              `json:"ok"`                          // true if operation succeeded
	Data     []byte            `json:"data,omitempty"`              // Result data
	Found    bool              `json:"found,omitempty"`             // Result found flag
	Provider *ProviderResponse `json:"provider_response,omitempty"` // Provider-session response
	Err      string            `json:"err,omitempty"`               // Error message if OK is false
}

// Operation constants for Request.Op field.
const (
	OpHello                = "hello"  // Returns agent mode (e.g., "runtime")
	OpStatus               = "status" // Returns session status (JSON StatusInfo)
	OpLock                 = "lock"   // Terminates the agent
	OpMetadataGet          = "metadata_get"
	OpMetadataPut          = "metadata_put"
	OpMetadataDelete       = "metadata_delete"
	OpMetadataDeletePrefix = "metadata_delete_prefix"
	OpMetadataClear        = "metadata_clear"
	OpProviderRequest      = "provider_request"
)

// StatusInfo is returned by the status operation.
type StatusInfo struct {
	Mode                 string `json:"mode"`                   // Security mode (e.g., "software")
	IdleTimeout          int64  `json:"idle_timeout"`           // Configured idle timeout in seconds
	MaxLifetime          int64  `json:"max_lifetime"`           // Configured max lifetime in seconds
	RemainingLife        int64  `json:"remaining_life"`         // Seconds until max lifetime expires
	RemainingIdle        int64  `json:"remaining_idle"`         // Seconds until idle timeout (approximate)
	MetadataCacheEntries int    `json:"metadata_cache_entries"` // Non-secret metadata cache count
	ProviderSessions     int    `json:"provider_sessions"`      // Active provider-session count
}

// ProviderRequest describes a provider-scoped operation brokered by the agent.
type ProviderRequest struct {
	Provider    string `json:"provider"`
	Action      string `json:"action"`
	Scope       string `json:"scope,omitempty"`
	Name        string `json:"name,omitempty"`
	Ref         string `json:"ref,omitempty"`
	Username    string `json:"username,omitempty"`
	UsernameRef string `json:"username_ref,omitempty"`
}

// ProviderResponse returns a request-scoped credential record.
type ProviderResponse struct {
	Found    bool   `json:"found"`
	Username string `json:"username,omitempty"`
	Secret   []byte `json:"secret,omitempty"`
	Ref      string `json:"ref,omitempty"`
}
