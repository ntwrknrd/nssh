//go:build unix

package connector

import (
	"strings"
	"testing"
	"time"

	"github.com/ntwrknrd/nssh/internal/secret"
)

func TestPrepareDisplayOutputSuppressesPasswordPromptWithoutHighlighting(t *testing.T) {
	conn := NewConnector("edge01", "admin", nil, nil)

	out := string(conn.prepareDisplayOutput([]byte("Password:\ninterface ge-0/0/0 down\n"), true))

	if strings.Contains(out, "Password") {
		t.Fatalf("output contains password prompt: %q", out)
	}
	if strings.Contains(out, "\x1b[") {
		t.Fatalf("interactive display output should not be highlighted: %q", out)
	}
}

func TestPrepareDisplayOutputMasksPasswordEchoWithoutHighlighting(t *testing.T) {
	conn := NewConnector("edge01", "admin", secret.NewFromString("secretpass"), nil)
	conn.passwordSent = true
	conn.passwordSentAt = time.Now()

	out := string(conn.prepareDisplayOutput([]byte("secretpass\ninterface ge-0/0/0 down\n"), false))

	if strings.Contains(out, "secretpass") {
		t.Fatalf("output leaked password: %q", out)
	}
	if !strings.Contains(out, "********") {
		t.Fatalf("output missing password mask: %q", out)
	}
	if strings.Contains(out, "\x1b[") {
		t.Fatalf("interactive display output should not be highlighted: %q", out)
	}
}

func TestPrepareDisplayOutputDoesNotHighlightInteractiveOutput(t *testing.T) {
	conn := NewConnector("edge01", "admin", nil, nil)
	raw := []byte("interface ge-0/0/0 down\n")
	conn.ringBuf.Write(raw)

	display := string(conn.prepareDisplayOutput(raw, false))
	if strings.Contains(display, "\x1b[") {
		t.Fatalf("interactive display output should not be highlighted: %q", display)
	}
	if strings.Contains(conn.LastOutput(), "\x1b[") {
		t.Fatalf("LastOutput contains ANSI: %q", conn.LastOutput())
	}
	if conn.LastOutput() != string(raw) {
		t.Fatalf("LastOutput = %q, want raw %q", conn.LastOutput(), raw)
	}
}
