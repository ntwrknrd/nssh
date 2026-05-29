//go:build linux || darwin

package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/ntwrknrd/nssh/internal/config"
	"golang.org/x/sync/semaphore"
)

// Default timeout values for the agent.
const (
	DefaultIdleTimeout = 1 * time.Hour   // No activity timeout
	DefaultMaxLifetime = 24 * time.Hour  // Hard cap regardless of activity
	DefaultMaxSleep    = 5 * time.Minute // Max sleep between deadline checks

	// Maximum concurrent connections to prevent resource exhaustion
	maxConcurrentConnections = 10

	// Maximum request size to prevent memory exhaustion (1MB should be plenty
	// for age-encrypted credentials which are typically a few KB)
	maxRequestSize = 1 << 20 // 1 MiB

	// Timeout for writing responses to clients
	writeTimeout = 30 * time.Second
)

// RuntimeConfig holds runtime configuration for the agent daemon.
// Agent settings come from config.AgentConfig; runtime-only fields are separate.
type RuntimeConfig struct {
	Agent        *config.AgentConfig          // Timeout settings from config file
	Archive      *config.SessionArchiveConfig // Archive settings from config file
	Logger       *slog.Logger
	ReadyPipe    *os.File      // Optional: pipe to signal readiness after socket creation
	MaxSleep     time.Duration // Max sleep between deadline checks (0 = default 5m)
	Clock        clock         // Optional clock (tests can inject fake); defaults to realClock
	RecordingDir string        // Directory containing live .cast recordings
}

// DefaultRuntimeConfig returns a RuntimeConfig with default values.
func DefaultRuntimeConfig() RuntimeConfig {
	paths := config.DefaultPaths()
	return RuntimeConfig{
		Agent: &config.AgentConfig{
			IdleTimeout:       config.Duration(DefaultIdleTimeout),
			ActivityIncrement: config.Duration(15 * time.Minute),
			MaxLifetime:       config.Duration(DefaultMaxLifetime),
		},
		Archive: &config.SessionArchiveConfig{
			Dir:        filepath.Join(paths.StateDir, "archives"),
			Enabled:    false,
			Jitter:     config.Duration(30 * time.Minute),
			MaxBundles: 12,
			MinAge:     config.Duration(30 * 24 * time.Hour),
		},
		Logger:       slog.Default(),
		Clock:        realClock{},
		RecordingDir: paths.RecordingsDir,
	}
}

// sessionState tracks timing information for the session.
// Uses wall clock time (not monotonic) to properly handle system sleep.
type sessionState struct {
	startTime         time.Time
	idleDeadline      time.Time     // Absolute deadline for idle timeout
	idleTimeout       time.Duration // Max idle extension from now
	activityIncrement time.Duration // How much to extend on each activity
	maxLifetime       time.Duration
	clock             clock
	mu                sync.RWMutex
}

type secretCache struct {
	mu   sync.RWMutex
	data map[string][]byte
}

func newSecretCache() *secretCache {
	return &secretCache{data: make(map[string][]byte)}
}

func (c *secretCache) get(key string) ([]byte, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	value, ok := c.data[key]
	if !ok {
		return nil, false
	}
	return append([]byte(nil), value...), true
}

func (c *secretCache) put(key string, value []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.data[key] = append([]byte(nil), value...)
}

// stripMonotonic removes the monotonic component from a time.Time value.
// This ensures comparisons use wall clock time so that system sleep or clock
// adjustments are accounted for.
func stripMonotonic(t time.Time) time.Time {
	return time.Unix(t.Unix(), int64(t.Nanosecond()))
}

// extendIdleDeadline extends the idle deadline by activityIncrement,
// capped at idleTimeout from now. Never reduces the deadline.
func (s *sessionState) extendIdleDeadline() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.clock.Now()
	increment := s.activityIncrement
	if increment == 0 {
		increment = s.idleTimeout // Backward compatibility
	}

	// Extend current deadline by increment
	proposed := s.idleDeadline.Add(increment)

	// Cap at idleTimeout from now
	maxDeadline := now.Add(s.idleTimeout)
	if proposed.After(maxDeadline) {
		proposed = maxDeadline
	}

	// Only extend, never reduce
	if proposed.After(s.idleDeadline) {
		s.idleDeadline = proposed
	}
}

func (s *sessionState) status(mode string) StatusInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := s.clock.Now()
	remainingLife := s.maxLifetime - now.Sub(s.startTime)
	remainingIdle := s.idleDeadline.Sub(now)

	if remainingLife < 0 {
		remainingLife = 0
	}
	if remainingIdle < 0 {
		remainingIdle = 0
	}

	return StatusInfo{
		Mode:          mode,
		IdleTimeout:   int64(s.idleTimeout.Seconds()),
		MaxLifetime:   int64(s.maxLifetime.Seconds()),
		RemainingLife: int64(remainingLife.Seconds()),
		RemainingIdle: int64(remainingIdle.Seconds()),
	}
}

// expired reports whether idle or lifetime limits have passed at now and the reason.
func (s *sessionState) expired(now time.Time) (bool, string) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if now.Sub(s.startTime) >= s.maxLifetime {
		return true, "max_lifetime_exceeded"
	}
	if now.After(s.idleDeadline) || now.Equal(s.idleDeadline) {
		return true, "idle_timeout"
	}
	return false, ""
}

// getIdleDeadline returns the current idle deadline.
func (s *sessionState) getIdleDeadline() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.idleDeadline
}

// Run starts the agent daemon with the given provider and configuration.
// The agent listens on a Unix socket and handles decrypt requests until:
// - Idle timeout expires (no activity for IdleTimeout duration)
// - Max lifetime expires (regardless of activity)
// - Lock command received from client
// - SIGTERM/SIGINT/SIGHUP signal received
//
// This function blocks until the agent shuts down.
func Run(provider Provider, cfg RuntimeConfig) error {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	defer signal.Stop(sigChan)

	reason, err := runAgent(context.Background(), provider, cfg, func(shutdown chan<- string) {
		go func() {
			sig := <-sigChan
			shutdown <- fmt.Sprintf("signal_%s", sig)
		}()
	})
	if err == nil {
		cfg.Logger.Debug("agent exited", "reason", reason)
	}
	return err
}

// handleConnection processes client requests on a connection.
// Handles multiple requests per connection until client disconnects or lock.
// The activityCh is used to signal the main loop that activity occurred,
// allowing it to reset the idle timer for long-lived connections.
func handleConnection(conn *net.UnixConn, provider Provider, logger *slog.Logger, shutdown chan<- string, state *sessionState, activityCh chan<- struct{}, cache *secretCache) {
	defer func() { _ = conn.Close() }()

	// Verify peer credentials
	if err := VerifyPeer(conn); err != nil {
		logger.Warn("connection rejected", "reason", "peer_verification_failed", "err", err)
		return
	}

	// Limit request size to prevent memory exhaustion from malicious/buggy clients
	decoder := json.NewDecoder(io.LimitReader(conn, maxRequestSize))
	encoder := json.NewEncoder(conn)

	// signalActivity notifies the main loop that activity occurred
	signalActivity := func() {
		state.extendIdleDeadline()
		select {
		case activityCh <- struct{}{}:
		default:
			// Non-blocking: if channel is full, activity was recently signaled
		}
	}

	// Handle multiple requests per connection
	for {
		// Set read deadline for each request (idle timeout per request)
		if err := conn.SetReadDeadline(time.Now().Add(30 * time.Second)); err != nil {
			logger.Debug("failed to set read deadline", "err", err)
		}

		var req Request
		if err := decoder.Decode(&req); err != nil {
			// EOF means client closed connection cleanly
			if !errors.Is(err, io.EOF) {
				logger.Debug("request decode failed", "err", err)
			}
			return
		}

		// Detect expired timeouts immediately (e.g., after system sleep) and
		// trigger shutdown without waiting for the main loop timer to fire.
		if expired, reason := state.expired(state.clock.Now()); expired {
			select {
			case shutdown <- reason:
			default:
			}
			logger.Info("agent stopping", "reason", reason)
			return
		}

		// Validate protocol version
		if req.Version != ProtocolVersion {
			logger.Warn("protocol version mismatch",
				"client_version", req.Version,
				"agent_version", ProtocolVersion)
			_ = conn.SetWriteDeadline(time.Now().Add(writeTimeout))
			_ = encoder.Encode(Response{
				ID:  req.ID,
				OK:  false,
				Err: fmt.Sprintf("protocol version mismatch: client=%d agent=%d", req.Version, ProtocolVersion),
			})
			return
		}

		var resp Response
		resp.ID = req.ID

		switch req.Op {
		case OpHello:
			resp = Response{ID: req.ID, OK: true, Data: []byte(provider.Mode())}

		case OpStatus:
			// Status checks don't count as activity - they're just observing state
			statusData, _ := json.Marshal(state.status(provider.Mode()))
			resp = Response{ID: req.ID, OK: true, Data: statusData}

		case OpDecrypt:
			// Signal activity for decrypt operations to reset idle timer
			signalActivity()
			plaintext, err := provider.Decrypt(req.Data)
			if err != nil {
				logger.Warn("decrypt failed", "err", err)
				resp = Response{ID: req.ID, OK: false, Err: err.Error()}
			} else {
				logger.Debug("decrypt success", "ciphertext_len", len(req.Data))
				resp = Response{ID: req.ID, OK: true, Data: plaintext}
			}

		case OpRecipient:
			// Signal activity for recipient requests (used during encryption)
			signalActivity()
			resp = Response{ID: req.ID, OK: true, Data: []byte(provider.Recipient())}

		case OpCacheGet:
			signalActivity()
			data, found := cache.get(req.Key)
			resp = Response{ID: req.ID, OK: true, Found: found, Data: data}

		case OpCachePut:
			signalActivity()
			cache.put(req.Key, req.Data)
			resp = Response{ID: req.ID, OK: true}

		case OpLock:
			logger.Info("agent stopping", "reason", "lock_command")
			resp = Response{ID: req.ID, OK: true}
			_ = conn.SetWriteDeadline(time.Now().Add(writeTimeout))
			_ = encoder.Encode(resp)
			// Signal main loop to shutdown gracefully
			select {
			case shutdown <- "lock_command":
			default:
				// Shutdown already in progress
			}
			return

		default:
			logger.Warn("unknown operation", "op", req.Op)
			resp = Response{ID: req.ID, OK: false, Err: "unknown operation: " + req.Op}
		}

		// Set write deadline to prevent blocking on unresponsive clients
		if err := conn.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil {
			logger.Debug("failed to set write deadline", "err", err)
		}

		if err := encoder.Encode(resp); err != nil {
			logger.Debug("response encode failed", "err", err)
			return
		}
	} // end for loop
}

// sendError sends an error response to a connection.
func sendError(conn net.Conn, msg string) {
	_ = conn.SetWriteDeadline(time.Now().Add(writeTimeout))
	encoder := json.NewEncoder(conn)
	_ = encoder.Encode(Response{OK: false, Err: msg})
}

// RunInBackground starts the agent in a background context.
// Returns a cancel function to stop the agent and a channel that closes when done.
func RunInBackground(ctx context.Context, provider Provider, cfg RuntimeConfig) (context.CancelFunc, <-chan struct{}) {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})

	go func() {
		defer close(done)
		if reason, err := runAgent(ctx, provider, cfg, func(shutdown chan<- string) {
			go func() {
				<-ctx.Done()
				shutdown <- "context_canceled"
			}()
		}); err != nil {
			cfg.Logger.Error("create socket failed", "err", err)
		} else {
			cfg.Logger.Debug("agent exited", "reason", reason)
		}
	}()

	return cancel, done
}

// runAgent hosts the shared event loop used by both Run and RunInBackground.
// It returns the stop reason when the agent exits or an error if startup fails.
func runAgent(ctx context.Context, provider Provider, cfg RuntimeConfig, extendShutdown func(chan<- string)) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	clk := cfg.Clock
	if clk == nil {
		clk = realClock{}
	}

	sockPath := SocketPath()

	ln, err := CreateSocket(sockPath)
	if err != nil {
		return "", fmt.Errorf("create socket: %w", err)
	}
	defer func() { _ = ln.Close() }()
	defer func() { _ = RemoveSocket(sockPath) }()

	if cfg.ReadyPipe != nil {
		if _, err := cfg.ReadyPipe.WriteString("ok\n"); err != nil {
			cfg.Logger.Warn("failed to signal readiness", "err", err)
		}
		if err := cfg.ReadyPipe.Close(); err != nil {
			cfg.Logger.Debug("failed to close ready pipe", "err", err)
		}
		cfg.ReadyPipe = nil
	}

	now := clk.Now()
	state := &sessionState{
		startTime:         now,
		idleDeadline:      now.Add(cfg.Agent.IdleTimeout.Duration()),
		idleTimeout:       cfg.Agent.IdleTimeout.Duration(),
		activityIncrement: cfg.Agent.ActivityIncrement.Duration(),
		maxLifetime:       cfg.Agent.MaxLifetime.Duration(),
		clock:             clk,
	}

	// Compute lifetime deadline using wall clock (no monotonic component).
	// This ensures time.Until() uses wall clock difference, correctly detecting
	// timeouts after system sleep when monotonic clock was paused.
	// Note: idleDeadline is tracked in sessionState and updated by extendIdleDeadline().
	lifeDeadline := now.Add(state.maxLifetime)

	maxSleep := cfg.MaxSleep
	if maxSleep <= 0 {
		maxSleep = DefaultMaxSleep
	}

	archiveSource := cfg.RecordingDir
	if archiveSource == "" {
		archiveSource = config.DefaultPaths().RecordingsDir
	}

	// Use archive config if provided, otherwise use defaults
	archive := cfg.Archive
	if archive == nil {
		archive = &config.SessionArchiveConfig{
			Dir:        filepath.Join(config.DefaultPaths().StateDir, "archives"),
			Enabled:    false,
			Jitter:     config.Duration(30 * time.Minute),
			MaxBundles: 12,
			MinAge:     config.Duration(30 * 24 * time.Hour),
		}
	}

	archiver := newRecordingArchiver(archiveConfig{
		enabled:     archive.Enabled,
		sourceDir:   archiveSource,
		archiveDir:  archive.Dir,
		minAge:      archive.MinAge.Duration(),
		maxBundles:  archive.MaxBundles,
		maxRunBytes: archive.MaxRunBytes,
		jitter:      archive.Jitter.Duration(),
	}, cfg.Logger, clk)

	archCtx, archCancel := context.WithCancel(ctx)
	var archWG sync.WaitGroup
	if archiver.enabled() {
		archWG.Add(1)
		go func() {
			defer archWG.Done()
			archiver.runLoop(archCtx)
		}()
	}
	defer func() {
		archCancel()
		archWG.Wait()
	}()

	connSem := semaphore.NewWeighted(maxConcurrentConnections)
	defer func() { _ = provider.Close() }()

	cache := newSecretCache()
	activityCh := make(chan struct{}, 1)
	shutdown := make(chan string, 1)

	if extendShutdown != nil {
		extendShutdown(shutdown)
	}

	type connResult struct {
		conn *net.UnixConn
		err  error
	}
	connChan := make(chan connResult)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				if errors.Is(err, net.ErrClosed) {
					return
				}
				connChan <- connResult{err: err}
				continue
			}
			unixConn, ok := conn.(*net.UnixConn)
			if !ok {
				cfg.Logger.Error("unexpected connection type, expected *net.UnixConn")
				_ = conn.Close()
				continue
			}
			connChan <- connResult{conn: unixConn}
		}
	}()

	cfg.Logger.Info("agent started",
		"mode", provider.Mode(),
		"idle_timeout", cfg.Agent.IdleTimeout.Duration(),
		"max_lifetime", cfg.Agent.MaxLifetime.Duration(),
		"socket", sockPath)

	// nextSleep calculates how long to sleep until the earlier deadline.
	// Returns sleep duration (capped by maxSleep) and the reason if expired.
	// Uses time.Until which recomputes from wall clock, so after system wake
	// a negative/zero duration triggers immediate timeout detection.
	nextSleep := func() (d time.Duration, expired bool, reason string) {
		// Pick the earlier deadline
		idleDeadline := state.getIdleDeadline()
		next := lifeDeadline
		reason = "max_lifetime_exceeded"
		if idleDeadline.Before(lifeDeadline) {
			next = idleDeadline
			reason = "idle_timeout"
		}

		d = next.Sub(clk.Now())
		if d <= 0 {
			return 0, true, reason
		}
		if d > maxSleep {
			d = maxSleep
		}
		return d, false, ""
	}

	for {
		sleepDur, expired, reason := nextSleep()
		if expired {
			cfg.Logger.Info("agent stopping", "reason", reason)
			return reason, nil
		}

		timer := clk.NewTimer(sleepDur)

		select {
		case reason := <-shutdown:
			timer.Stop()
			cfg.Logger.Info("agent stopping", "reason", reason)
			return reason, nil

		case <-ctx.Done():
			timer.Stop()
			cfg.Logger.Info("agent stopping", "reason", "context_canceled")
			return "context_canceled", nil

		case <-timer.C():
			// Timer fired - loop to recheck deadlines

		case <-activityCh:
			// Activity occurred - deadline already extended by signalActivity()
			timer.Stop()

		case result := <-connChan:
			timer.Stop()
			if result.err != nil {
				cfg.Logger.Error("accept failed", "err", result.err)
				continue
			}

			if !connSem.TryAcquire(1) {
				cfg.Logger.Warn("connection rejected", "reason", "max_connections_exceeded")
				sendError(result.conn, "agent busy: max connections exceeded")
				_ = result.conn.Close()
				continue
			}

			go func(c *net.UnixConn) {
				defer connSem.Release(1)
				handleConnection(c, provider, cfg.Logger, shutdown, state, activityCh, cache)
			}(result.conn)
		}
	}
}
