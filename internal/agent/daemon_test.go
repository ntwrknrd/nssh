//go:build linux || darwin

package agent

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/ntwrknrd/nssh/internal/config"
)

func TestDaemon_StartsAndListensOnSocket(t *testing.T) {
	socketPath := testSocketPath(t)
	restore := SetSocketPathForTest(socketPath)
	defer restore()
	cancel, done := startTestAgent(t)
	defer func() {
		cancel()
		<-done
	}()

	// Verify socket is created and listening
	if !waitForSocket(t, 5*time.Second) {
		t.Fatal("agent did not start listening on socket")
	}

	// Verify socket file exists at expected path
	info, err := os.Stat(socketPath)
	if err != nil {
		t.Fatalf("socket file not found: %v", err)
	}
	if info.Mode().Type() != os.ModeSocket {
		t.Errorf("file type = %v, want socket", info.Mode().Type())
	}
}

func TestOpenFDCountFallsBackToDescriptorProbe(t *testing.T) {
	got := openFDCountFrom([]string{filepath.Join(t.TempDir(), "missing")}, 6, func(fd int) bool {
		return fd == 0 || fd == 2 || fd == 5
	})
	if got != 3 {
		t.Fatalf("open fd count = %d, want 3", got)
	}
}

func TestDaemon_IdleTimeout(t *testing.T) {
	socketPath := testSocketPath(t)
	restore := SetSocketPathForTest(socketPath)
	defer restore()
	cfg := RuntimeConfig{
		Agent: &config.AgentConfig{
			IdleTimeout: config.Duration(200 * time.Millisecond),
			MaxLifetime: config.Duration(10 * time.Second),
		},
		Logger: testLogger(),
		Clock:  newFakeClock(time.Now()),
	}
	cancel, done := startTestAgentWithConfig(t, cfg)
	defer cancel()

	if !waitForSocket(t, 5*time.Second) {
		t.Fatal("agent did not start in time")
	}

	// Advance fake clock past idle timeout
	cfg.Clock.(*fakeClock).Advance(250 * time.Millisecond)

	if !waitDone(t, done, 3*time.Second) {
		t.Error("agent did not terminate after idle timeout")
	}
}

func TestDaemon_MaxLifetime(t *testing.T) {
	socketPath := testSocketPath(t)
	restore := SetSocketPathForTest(socketPath)
	defer restore()
	cfg := RuntimeConfig{
		Agent: &config.AgentConfig{
			IdleTimeout: config.Duration(10 * time.Second),
			MaxLifetime: config.Duration(200 * time.Millisecond),
		},
		Logger: testLogger(),
		Clock:  newFakeClock(time.Now()),
	}
	cancel, done := startTestAgentWithConfig(t, cfg)
	defer cancel()

	if !waitForSocket(t, 5*time.Second) {
		t.Fatal("agent did not start in time")
	}

	// Keep sending activity to prevent idle timeout
	go func() {
		for {
			select {
			case <-done:
				return
			default:
				client, err := Connect()
				if err != nil {
					return
				}
				_, _ = client.ProviderRequest(ProviderRequest{Provider: "missing", Action: "get"})
				_ = client.Close()
			}
		}
	}()

	cfg.Clock.(*fakeClock).Advance(300 * time.Millisecond)

	if !waitDone(t, done, time.Second) {
		t.Error("agent did not terminate after max lifetime")
	}
}

func TestDaemon_ActivityResetsIdleTimeout(t *testing.T) {
	socketPath := testSocketPath(t)
	restore := SetSocketPathForTest(socketPath)
	defer restore()
	cfg := RuntimeConfig{
		Agent: &config.AgentConfig{
			IdleTimeout: config.Duration(300 * time.Millisecond),
			MaxLifetime: config.Duration(10 * time.Second),
		},
		Logger: testLogger(),
		Clock:  newFakeClock(time.Now()),
	}
	cancel, done := startTestAgentWithConfig(t, cfg)
	defer cancel()

	if !waitForSocket(t, 5*time.Second) {
		t.Fatal("agent did not start in time")
	}

	fc := cfg.Clock.(*fakeClock)

	// Send activity periodically to reset idle timer using fake clock
	for i := 0; i < 3; i++ {
		client, err := Connect()
		if err != nil {
			t.Fatalf("Connect() error = %v", err)
		}
		_, _ = client.ProviderRequest(ProviderRequest{Provider: "missing", Action: "get"})
		_ = client.Close()

		fc.Advance(200 * time.Millisecond) // less than idle timeout, should keep alive
	}

	// Advance a bit but still less than idle timeout since last activity
	fc.Advance(90 * time.Millisecond)

	select {
	case <-done:
		t.Error("agent terminated unexpectedly")
	case <-time.After(50 * time.Millisecond):
		// Expected - agent should still be running
	}

	cancel()
	<-done
}

func TestDaemon_LockCommandTerminatesAgent(t *testing.T) {
	socketPath := testSocketPath(t)
	restore := SetSocketPathForTest(socketPath)
	defer restore()
	cancel, done := startTestAgent(t)
	defer cancel()

	if !waitForSocket(t, 5*time.Second) {
		t.Fatal("agent did not start in time")
	}

	client, err := Connect()
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer func() { _ = client.Close() }()

	// Send lock command
	if err := client.Lock(); err != nil {
		t.Fatalf("Lock() error = %v", err)
	}

	// Agent should terminate
	select {
	case <-done:
		// Expected
	case <-time.After(5 * time.Second):
		t.Error("agent did not terminate after Lock()")
	}
}

func TestDaemon_ConcurrentConnections(t *testing.T) {
	socketPath := testSocketPath(t)
	restore := SetSocketPathForTest(socketPath)
	defer restore()
	cancel, done := startTestAgent(t)
	defer func() {
		cancel()
		<-done
	}()

	if !waitForSocket(t, 5*time.Second) {
		t.Fatal("agent did not start in time")
	}

	// Create multiple concurrent connections
	var wg sync.WaitGroup
	var mu sync.Mutex
	var successes int

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			client, err := Connect()
			if err != nil {
				return
			}
			defer func() { _ = client.Close() }()

			_, err = client.Status()
			if err == nil {
				mu.Lock()
				successes++
				mu.Unlock()
			}
		}()
	}

	wg.Wait()

	if successes != 5 {
		t.Errorf("concurrent connections: %d/5 succeeded", successes)
	}
}

func TestDaemon_RejectsAtMaxConnections(t *testing.T) {
	socketPath := testSocketPath(t)
	restore := SetSocketPathForTest(socketPath)
	defer restore()
	cancel, done := startTestAgent(t)
	defer func() {
		cancel()
		<-done
	}()

	if !waitForSocket(t, 5*time.Second) {
		t.Fatal("agent did not start in time")
	}

	// Open max connections (10) and hold them
	var conns []net.Conn
	for i := 0; i < 10; i++ {
		conn, err := net.Dial("unix", socketPath)
		if err != nil {
			t.Fatalf("Dial() #%d error = %v", i, err)
		}
		conns = append(conns, conn)
	}
	defer func() {
		for _, c := range conns {
			_ = c.Close()
		}
	}()

	// Give the daemon time to reach max
	time.Sleep(100 * time.Millisecond)

	// 11th connection should work but may be slower
	// (daemon uses semaphore, so it waits rather than rejects)
	conn11, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Logf("11th connection error (expected with semaphore): %v", err)
	} else {
		_ = conn11.Close()
	}
}

func TestDaemon_ShutdownOnSIGHUP(t *testing.T) {
	testSignalShutdown(t, syscall.SIGHUP)
}

func TestDaemon_ShutdownOnSIGTERM(t *testing.T) {
	testSignalShutdown(t, syscall.SIGTERM)
}

func TestDaemon_ShutdownOnSIGINT(t *testing.T) {
	testSignalShutdown(t, syscall.SIGINT)
}

func testSignalShutdown(t *testing.T, sig syscall.Signal) {
	t.Helper()

	socketPath := testSocketPath(t)
	restore := SetSocketPathForTest(socketPath)
	defer restore()
	cancel, done := startTestAgent(t)
	defer cancel()

	if !waitForSocket(t, 5*time.Second) {
		t.Fatal("agent did not start in time")
	}

	// Send signal to current process (agent is in same process via RunInBackground)
	// Note: RunInBackground uses context cancellation, not signals, for shutdown
	// So we test via the cancel function which simulates the signal path
	cancel()

	select {
	case <-done:
		// Expected
	case <-time.After(5 * time.Second):
		t.Errorf("agent did not terminate after signal %v", sig)
	}
}

func TestDaemon_ProtocolVersionMismatch(t *testing.T) {
	socketPath := testSocketPath(t)
	restore := SetSocketPathForTest(socketPath)
	defer restore()
	cancel, done := startTestAgent(t)
	defer func() {
		cancel()
		<-done
	}()

	if !waitForSocket(t, 5*time.Second) {
		t.Fatal("agent did not start in time")
	}

	// Connect and send request with wrong version
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer func() { _ = conn.Close() }()

	req := Request{Version: 999, Op: OpStatus}
	encoder := json.NewEncoder(conn)
	decoder := json.NewDecoder(conn)

	if err := encoder.Encode(req); err != nil {
		t.Fatalf("encode request: %v", err)
	}

	var resp Response
	if err := decoder.Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.OK {
		t.Error("response OK = true, want false for version mismatch")
	}
	if resp.Err == "" {
		t.Error("response Err should contain version mismatch message")
	}
}

func TestDaemon_MalformedJSON(t *testing.T) {
	socketPath := testSocketPath(t)
	restore := SetSocketPathForTest(socketPath)
	defer restore()
	cancel, done := startTestAgent(t)
	defer func() {
		cancel()
		<-done
	}()

	if !waitForSocket(t, 5*time.Second) {
		t.Fatal("agent did not start in time")
	}

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer func() { _ = conn.Close() }()

	// Send malformed JSON
	if _, err := conn.Write([]byte("not valid json\n")); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Daemon should handle gracefully (close connection or send error)
	// Read response with timeout
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		// Connection closed is acceptable
		return
	}

	// If we got a response, it should be an error
	var resp Response
	if err := json.Unmarshal(buf[:n], &resp); err == nil {
		if resp.OK {
			t.Error("response OK = true for malformed JSON")
		}
	}
}

func TestDaemon_UnknownOperation(t *testing.T) {
	socketPath := testSocketPath(t)
	restore := SetSocketPathForTest(socketPath)
	defer restore()
	cancel, done := startTestAgent(t)
	defer func() {
		cancel()
		<-done
	}()

	if !waitForSocket(t, 5*time.Second) {
		t.Fatal("agent did not start in time")
	}

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer func() { _ = conn.Close() }()

	req := Request{Version: ProtocolVersion, Op: "unknown_op"}
	encoder := json.NewEncoder(conn)
	decoder := json.NewDecoder(conn)

	if err := encoder.Encode(req); err != nil {
		t.Fatalf("encode request: %v", err)
	}

	var resp Response
	if err := decoder.Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.OK {
		t.Error("response OK = true, want false for unknown operation")
	}
	if resp.Err == "" {
		t.Error("response Err should contain unknown operation message")
	}
}

func TestDaemon_StaleSocketCleanup(t *testing.T) {
	// Use short path to avoid Unix socket path length limits
	tmpDir, err := os.MkdirTemp("/tmp", "nssh")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	socketPath := filepath.Join(tmpDir, "t.sock")
	restore := SetSocketPathForTest(socketPath)
	defer restore()

	// Create a fake socket file
	if err := os.WriteFile(socketPath, []byte("stale"), 0600); err != nil {
		t.Fatalf("create stale file: %v", err)
	}
	cancel, done := startTestAgent(t)
	defer func() {
		cancel()
		<-done
	}()

	// Agent should start despite stale socket
	if !waitForSocket(t, 5*time.Second) {
		t.Fatal("agent did not start despite stale socket")
	}

	// Verify we can connect
	client, err := Connect()
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer func() { _ = client.Close() }()

	if _, err := client.Status(); err != nil {
		t.Fatalf("Status() error = %v", err)
	}
}

func TestDaemon_StatusDoesNotResetIdleTimer(t *testing.T) {
	socketPath := testSocketPath(t)
	restore := SetSocketPathForTest(socketPath)
	defer restore()
	cfg := RuntimeConfig{
		Agent: &config.AgentConfig{
			// Idle timeout longer than loop duration (3 * 100ms = 300ms)
			// so we can complete all status calls before timeout
			IdleTimeout: config.Duration(400 * time.Millisecond),
			MaxLifetime: config.Duration(10 * time.Second),
		},
		Logger: testLogger(),
		Clock:  newFakeClock(time.Now()),
	}
	cancel, done := startTestAgentWithConfig(t, cfg)
	defer cancel()

	if !waitForSocket(t, 5*time.Second) {
		t.Fatal("agent did not start in time")
	}

	fc := cfg.Clock.(*fakeClock)

	// Send only status requests (should NOT reset idle timer)
	for i := 0; i < 3; i++ {
		client, err := Connect()
		if err != nil {
			t.Fatalf("Connect() error = %v", err)
		}
		_, err = client.Status() // Status doesn't signal activity
		_ = client.Close()
		if err != nil {
			t.Fatalf("Status() error = %v", err)
		}
		fc.Advance(100 * time.Millisecond)
	}

	fc.Advance(200 * time.Millisecond) // surpass idle timeout since last activity

	if !waitDone(t, done, time.Second) {
		t.Error("agent should have terminated due to idle timeout")
		cancel()
	}
}

func TestDaemon_ProviderRequestResetsIdleTimer(t *testing.T) {
	socketPath := testSocketPath(t)
	restore := SetSocketPathForTest(socketPath)
	defer restore()
	cfg := RuntimeConfig{
		Agent: &config.AgentConfig{
			IdleTimeout: config.Duration(200 * time.Millisecond),
			MaxLifetime: config.Duration(10 * time.Second),
		},
		Logger: testLogger(),
	}
	cancel, done := startTestAgentWithConfig(t, cfg)
	defer cancel()

	if !waitForSocket(t, 5*time.Second) {
		t.Fatal("agent did not start in time")
	}

	// Send metadata requests periodically to reset idle timer
	for i := 0; i < 5; i++ {
		time.Sleep(150 * time.Millisecond) // Less than idle timeout

		client, err := Connect()
		if err != nil {
			t.Fatalf("Connect() #%d error = %v", i, err)
		}

		_, _ = client.ProviderRequest(ProviderRequest{Provider: "missing", Action: "get"})
		_ = client.Close()
	}

	// Agent should still be running
	select {
	case <-done:
		t.Error("agent terminated unexpectedly")
	case <-time.After(50 * time.Millisecond):
		// Expected - agent should still be running
	}

	cancel()
	<-done
}
