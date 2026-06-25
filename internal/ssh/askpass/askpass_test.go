package askpass

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

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
