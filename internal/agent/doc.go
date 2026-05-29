// Package agent implements the nssh background runtime daemon.
//
// The agent communicates with nssh clients over a Unix domain socket using a
// JSON protocol. It brokers provider-session requests, stores non-secret
// metadata cache entries, owns socket lifecycle checks, and runs recording
// archival tasks.
//
// # Lifecycle
//
// The agent terminates automatically based on configurable timeouts:
//
//   - Idle timeout: terminates after period of inactivity (default 1h)
//   - Max lifetime: hard cap regardless of activity (default 24h)
//   - Stop request: terminated by the internal lock protocol operation used by
//     `nssh agent stop`
//
// # Protocol
//
// Clients communicate via JSON messages over the Unix socket. Operations include:
//
//   - OpStatus: query runtime status and remaining lifetime
//   - OpLock: terminate the agent session
//   - OpProviderRequest: broker a provider-scoped request
//   - Metadata cache operations for non-secret state
//
// # Background Tasks
//
// The agent runs background goroutines for:
//
//   - Recording archival: monitors live session recordings and archives them
//   - Connection handling: serves client requests with concurrency limits
package agent
