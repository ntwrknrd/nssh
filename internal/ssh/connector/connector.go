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
	sshOptions       config.SSHHostConfig
	sshVerbosity     int
	acceptOnceMode   string

	passwordMu sync.Mutex

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

	// Resolved endpoint from nssh config. The port is rendered into argv unless
	// the caller supplied an explicit SSH port, and both values are used for
	// AcceptOnce host-key pinning.
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

// SetResolvedEndpoint sets the concrete hostname and port derived from nssh config.
func (c *Connector) SetResolvedEndpoint(host, port string) {
	c.resolvedHost = strings.TrimSpace(host)
	c.resolvedPort = strings.TrimSpace(port)
}

func (c *Connector) SetSSHOptions(opts config.SSHHostConfig) {
	c.sshOptions = opts
}

func (c *Connector) SetSSHVerbosity(level int) {
	c.sshVerbosity = level
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
