package askpass

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ntwrknrd/nssh/internal/secret"
)

const (
	SocketEnv = "NSSH_ASKPASS_SOCKET"
	NonceEnv  = "NSSH_ASKPASS_NONCE"
)

type Server struct {
	dir        string
	socketPath string
	nonce      string
	resolve    func(context.Context) (*secret.Secret, error)
	listener   *net.UnixListener
	closeOnce  sync.Once
	closeErr   error
}

func NewServer(password *secret.Secret) (*Server, error) {
	if password == nil {
		return nil, fmt.Errorf("askpass password is required")
	}
	return NewServerWithResolver(func(context.Context) (*secret.Secret, error) {
		return password, nil
	})
}

func NewServerWithResolver(resolve func(context.Context) (*secret.Secret, error)) (*Server, error) {
	if resolve == nil {
		return nil, fmt.Errorf("askpass password resolver is required")
	}
	dir, err := os.MkdirTemp("", "nssh-askpass-*")
	if err != nil {
		return nil, fmt.Errorf("create askpass dir: %w", err)
	}
	if err := os.Chmod(dir, 0700); err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("chmod askpass dir: %w", err)
	}
	nonce, err := randomNonce()
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	socketPath := filepath.Join(dir, "askpass.sock")
	addr := &net.UnixAddr{Name: socketPath, Net: "unix"}
	listener, err := net.ListenUnix("unix", addr)
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("listen askpass socket: %w", err)
	}
	return &Server{
		dir:        dir,
		socketPath: socketPath,
		nonce:      nonce,
		resolve:    resolve,
		listener:   listener,
	}, nil
}

func (s *Server) Dir() string {
	return s.dir
}

func (s *Server) SocketPath() string {
	return s.socketPath
}

func (s *Server) Nonce() string {
	return s.nonce
}

func (s *Server) Env(helper string) []string {
	return []string{
		"SSH_ASKPASS=" + helper,
		"SSH_ASKPASS_REQUIRE=force",
		"DISPLAY=nssh-askpass",
		SocketEnv + "=" + s.socketPath,
		NonceEnv + "=" + s.nonce,
	}
}

func (s *Server) ServeOnce(ctx context.Context) error {
	if s == nil || s.listener == nil {
		return fmt.Errorf("askpass server is closed")
	}
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = s.listener.Close()
		case <-done:
		}
	}()
	defer close(done)

	conn, err := s.listener.AcceptUnix()
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("accept askpass connection: %w", err)
	}
	defer func() { _ = conn.Close() }()
	_ = s.listener.Close()

	if err := verifyPeer(conn); err != nil {
		return err
	}
	reader := bufio.NewReader(conn)
	nonce, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("read askpass nonce: %w", err)
	}
	if strings.TrimSuffix(nonce, "\n") != s.nonce {
		return fmt.Errorf("askpass nonce mismatch")
	}
	password, err := s.resolve(ctx)
	if err != nil {
		return err
	}
	if password == nil {
		return fmt.Errorf("askpass password is required")
	}
	if err := password.Use(func(password []byte) error {
		_, err := conn.Write(password)
		return err
	}); err != nil {
		return err
	}
	return nil
}

func (s *Server) Close() error {
	if s == nil {
		return nil
	}
	s.closeOnce.Do(func() {
		if s.listener != nil {
			_ = s.listener.Close()
		}
		s.closeErr = os.RemoveAll(s.dir)
	})
	return s.closeErr
}

func RequestPassword(ctx context.Context, socketPath, nonce string) ([]byte, error) {
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("connect askpass socket: %w", err)
	}
	defer func() { _ = conn.Close() }()

	if _, err := io.WriteString(conn, nonce+"\n"); err != nil {
		return nil, fmt.Errorf("write askpass nonce: %w", err)
	}
	password, err := io.ReadAll(conn)
	if err != nil {
		return nil, fmt.Errorf("read askpass password: %w", err)
	}
	if len(password) == 0 {
		return nil, fmt.Errorf("empty askpass password response")
	}
	return password, nil
}

func randomNonce() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate askpass nonce: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}
