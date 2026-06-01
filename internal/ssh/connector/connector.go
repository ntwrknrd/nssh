//go:build unix

package connector

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/secret"
	"golang.org/x/term"
)

// Default configuration values.
const (
	DefaultRingBufferSize       = 2048
	DefaultPasswordFilterWindow = 100 * time.Millisecond
)

// Connector manages a PTY-based SSH connection with credential injection.
type Connector struct {
	hostname         string
	username         string
	password         *secret.Secret
	passwordResolver func(context.Context) (*secret.Secret, error)
	sshArgs          []string
	acceptOnceMode   string

	ptyFile *os.File  // PTY master from creack/pty.Start()
	sshCmd  *exec.Cmd // SSH child process

	ringBuf        *RingBuffer
	passwordSent   bool
	passwordSentAt time.Time
	hostKeyHandled bool
	pinnedHostKey  *pinnedKey // Captured key type + fingerprint from AcceptOnce flow
	hostKeyPrompt  HostKeyPromptFunc

	// Stdin handling
	stdinCh      <-chan stdinResult
	stdinStarted bool

	mu sync.RWMutex
	wg sync.WaitGroup

	timeouts *config.SSHConnectionConfig

	useTemporaryKnownHosts bool   // Set by AcceptOnce, triggers restart with temp file
	tempKnownHosts         string // Path to temp known_hosts, cleaned up on exit

	// Terminal state for restoration
	oldState *term.State

	// Timing instrumentation
	sessionStart time.Time // When relay() started (for relative timing)

	// Resolved endpoint from SSH config (HostName/Port). Used for keyscan in AcceptOnce
	// host-key pinning. NOT used as SSH target - we use hostname (alias) for that so
	// SSH config Host pattern matching works correctly.
	resolvedHost string
	resolvedPort string
}

// NewConnector creates a new PTY connector for SSH connections.
func NewConnector(host, user string, pass *secret.Secret, sshArgs []string) *Connector {
	return &Connector{
		hostname:       host,
		username:       user,
		password:       pass,
		sshArgs:        sshArgs,
		ringBuf:        NewRingBuffer(DefaultRingBufferSize),
		acceptOnceMode: "pin",
	}
}

// SetResolvedEndpoint sets the concrete hostname and port derived from SSH config.
// Used for host-key pinning (keyscan) when the user connects via an alias.
// NOT used as the SSH command target - we use the alias for proper config matching.
func (c *Connector) SetResolvedEndpoint(host, port string) {
	c.resolvedHost = strings.TrimSpace(host)
	c.resolvedPort = strings.TrimSpace(port)
}

// SetAcceptOnceMode configures how AcceptOnce handles host keys: "pin" (default)
// pre-seeds a temp known_hosts with the observed key; "accept-new" uses TOFU.
func (c *Connector) SetAcceptOnceMode(mode string) {
	switch strings.ToLower(mode) {
	case "accept-new":
		c.acceptOnceMode = "accept-new"
	default:
		c.acceptOnceMode = "pin"
	}
}

// SetTimeouts configures connection timeouts from config.
func (c *Connector) SetTimeouts(cfg *config.SSHConnectionConfig) {
	c.timeouts = cfg
}

// SetHostKeyPromptFunc configures the interactive host-key prompt callback.
func (c *Connector) SetHostKeyPromptFunc(fn HostKeyPromptFunc) {
	c.hostKeyPrompt = fn
}

// SetPasswordResolver configures deferred password lookup for prompt-driven auth.
func (c *Connector) SetPasswordResolver(fn func(context.Context) (*secret.Secret, error)) {
	c.passwordResolver = fn
}
