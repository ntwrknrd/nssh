package askpass

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ntwrknrd/nssh/internal/secret"
)

func TestServerEnvDoesNotExposePasswordAndUsesPrivateDirectory(t *testing.T) {
	password := secret.NewFromString("super-secret")
	defer password.Destroy()

	server, err := NewServer(password)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer server.Close()

	info, err := os.Stat(server.Dir())
	if err != nil {
		t.Fatalf("stat server dir: %v", err)
	}
	if got := info.Mode().Perm(); got != 0700 {
		t.Fatalf("dir mode = %o, want 0700", got)
	}

	env := strings.Join(server.Env("/tmp/nssh-askpass"), "\n")
	if strings.Contains(env, "super-secret") {
		t.Fatalf("askpass env leaked password: %s", env)
	}
	if !strings.Contains(env, "SSH_ASKPASS=/tmp/nssh-askpass") {
		t.Fatalf("askpass env missing helper: %s", env)
	}
	if !strings.Contains(env, "SSH_ASKPASS_REQUIRE=force") {
		t.Fatalf("askpass env missing force mode: %s", env)
	}
}

func TestServerSendsPasswordOnceForMatchingNonce(t *testing.T) {
	password := secret.NewFromString("super-secret")
	defer password.Destroy()

	server, err := NewServer(password)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer server.Close()

	done := make(chan error, 1)
	go func() {
		done <- server.ServeOnce(context.Background())
	}()

	got, err := RequestPassword(context.Background(), server.SocketPath(), server.Nonce())
	if err != nil {
		t.Fatalf("RequestPassword: %v", err)
	}
	if !bytes.Equal(got, []byte("super-secret")) {
		t.Fatalf("password = %q", got)
	}
	if err := <-done; err != nil {
		t.Fatalf("ServeOnce: %v", err)
	}

	if _, err := RequestPassword(context.Background(), server.SocketPath(), server.Nonce()); err == nil {
		t.Fatal("second RequestPassword succeeded after one-shot server closed")
	}
}

func TestServerServesMultiplePasswordRequestsUntilCanceled(t *testing.T) {
	password := secret.NewFromString("super-secret")
	defer password.Destroy()

	server, err := NewServer(password)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- server.Serve(ctx)
	}()

	for i := 0; i < 2; i++ {
		got, err := RequestPassword(context.Background(), server.SocketPath(), server.Nonce())
		if err != nil {
			t.Fatalf("RequestPassword %d: %v", i+1, err)
		}
		if !bytes.Equal(got, []byte("super-secret")) {
			t.Fatalf("password %d = %q", i+1, got)
		}
	}

	cancel()
	if err := <-done; err != context.Canceled {
		t.Fatalf("Serve = %v, want context canceled", err)
	}
}

func TestServerRejectsWrongNonce(t *testing.T) {
	password := secret.NewFromString("super-secret")
	defer password.Destroy()

	server, err := NewServer(password)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer server.Close()

	done := make(chan error, 1)
	go func() {
		done <- server.ServeOnce(context.Background())
	}()

	got, err := RequestPassword(context.Background(), server.SocketPath(), "wrong")
	if err == nil {
		t.Fatalf("RequestPassword succeeded with wrong nonce: %q", got)
	}
	if err := <-done; err == nil {
		t.Fatal("ServeOnce accepted wrong nonce")
	}
}

func TestCloseRemovesPrivateDirectory(t *testing.T) {
	password := secret.NewFromString("super-secret")
	defer password.Destroy()

	server, err := NewServer(password)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	dir := server.Dir()

	if err := server.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("server dir still exists or unexpected error: %v", err)
	}
}

func TestServerWithResolverWaitsForPassword(t *testing.T) {
	release := make(chan struct{})
	server, err := NewServerWithResolver(func(ctx context.Context) (*secret.Secret, error) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-release:
		}
		return secret.NewFromString("delayed-secret"), nil
	})
	if err != nil {
		t.Fatalf("NewServerWithResolver: %v", err)
	}
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- server.ServeOnce(ctx)
	}()

	passwordCh := make(chan []byte, 1)
	errCh := make(chan error, 1)
	go func() {
		password, err := RequestPassword(ctx, server.SocketPath(), server.Nonce())
		if err != nil {
			errCh <- err
			return
		}
		passwordCh <- password
	}()

	select {
	case <-passwordCh:
		t.Fatal("askpass request returned before resolver completed")
	case err := <-errCh:
		t.Fatalf("RequestPassword: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	close(release)

	select {
	case password := <-passwordCh:
		if string(password) != "delayed-secret" {
			t.Fatalf("password = %q, want delayed-secret", password)
		}
	case err := <-errCh:
		t.Fatalf("RequestPassword: %v", err)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for askpass password")
	}

	if err := <-done; err != nil {
		t.Fatalf("ServeOnce: %v", err)
	}
}

func TestServerRoutesDecodedPromptToResolver(t *testing.T) {
	var gotPrompt string
	password := secret.NewFromString("proxy-secret")
	defer password.Destroy()
	server, err := NewServerWithPromptResolver(func(_ context.Context, prompt string) (*secret.Secret, error) {
		gotPrompt = prompt
		return password, nil
	})
	if err != nil {
		t.Fatalf("NewServerWithPromptResolver: %v", err)
	}
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- server.ServeOnce(ctx) }()

	prompt := "(netops@jump01.example) Password:"
	got, err := RequestPassword(ctx, server.SocketPath(), server.Nonce(), prompt)
	if err != nil {
		t.Fatalf("RequestPassword: %v", err)
	}
	if string(got) != "proxy-secret" {
		t.Fatalf("password = %q", got)
	}
	if gotPrompt != prompt {
		t.Fatalf("prompt = %q, want %q", gotPrompt, prompt)
	}
	if err := <-done; err != nil {
		t.Fatalf("ServeOnce: %v", err)
	}
}
