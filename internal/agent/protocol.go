// Package agent provides a transparent in-memory credential agent for nssh.
//
// The agent holds decrypted age credentials in memory and communicates with
// nssh processes via a Unix domain socket. This replaces platform-specific
// keychain/keyring session caching with a simpler, cross-platform approach.
package agent

// ProtocolVersion is incremented when breaking changes are made to the protocol.
// Clients and agents must agree on version to communicate.
const ProtocolVersion = 1

// Request represents a message from client to agent.
type Request struct {
	Version int    `json:"v"`              // Protocol version (required)
	ID      string `json:"id,omitempty"`   // Request ID for log correlation (optional)
	Op      string `json:"op"`             // Operation: "hello", "decrypt", "recipient", "lock"
	Data    []byte `json:"data,omitempty"` // Ciphertext for decrypt operation
	Key     string `json:"key,omitempty"`  // Cache key for cache operations
}

// Response represents a message from agent to client.
type Response struct {
	ID    string `json:"id,omitempty"`   // Echoes request ID for correlation
	OK    bool   `json:"ok"`             // true if operation succeeded
	Data  []byte `json:"data,omitempty"` // Result data (mode string, plaintext, or recipient)
	Found bool   `json:"found,omitempty"`
	Err   string `json:"err,omitempty"` // Error message if OK is false
}

// Operation constants for Request.Op field.
const (
	OpHello     = "hello"     // Returns agent mode (e.g., "software")
	OpStatus    = "status"    // Returns session status (JSON StatusInfo)
	OpDecrypt   = "decrypt"   // Decrypts ciphertext, returns plaintext
	OpRecipient = "recipient" // Returns age public key for encryption
	OpLock      = "lock"      // Terminates the agent
	OpCacheGet  = "cache_get" // Returns cached data for key, if present
	OpCachePut  = "cache_put" // Stores cached data for key
)

// StatusInfo is returned by the status operation.
type StatusInfo struct {
	Mode          string `json:"mode"`           // Security mode (e.g., "software")
	IdleTimeout   int64  `json:"idle_timeout"`   // Configured idle timeout in seconds
	MaxLifetime   int64  `json:"max_lifetime"`   // Configured max lifetime in seconds
	RemainingLife int64  `json:"remaining_life"` // Seconds until max lifetime expires
	RemainingIdle int64  `json:"remaining_idle"` // Seconds until idle timeout (approximate)
}
