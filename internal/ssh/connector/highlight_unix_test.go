//go:build unix

package connector

import (
	"strings"
	"testing"
	"time"

	"github.com/ntwrknrd/nssh/internal/secret"
	"github.com/ntwrknrd/nssh/internal/ssh/highlight"
)

func TestPrepareDisplayOutputSuppressesPasswordPromptBeforeHighlighting(t *testing.T) {
	conn := NewConnector("edge01", "admin", nil, nil)
	conn.SetHighlightOptions(highlight.Options{Enabled: true, Profile: highlight.ProfileJunos})

	out := string(conn.prepareDisplayOutput([]byte("Password:\ninterface ge-0/0/0 down\n"), true))

	if strings.Contains(out, "Password") {
		t.Fatalf("output contains password prompt: %q", out)
	}
	if !strings.Contains(out, "\x1b[34mge-0/0/0\x1b[0m") {
		t.Fatalf("output missing highlighted interface: %q", out)
	}
	if !strings.Contains(out, "\x1b[31mdown\x1b[0m") {
		t.Fatalf("output missing highlighted down state: %q", out)
	}
}

func TestPrepareDisplayOutputMasksPasswordEchoBeforeHighlighting(t *testing.T) {
	conn := NewConnector("edge01", "admin", secret.NewFromString("secretpass"), nil)
	conn.passwordSent = true
	conn.passwordSentAt = time.Now()
	conn.SetHighlightOptions(highlight.Options{Enabled: true, Profile: highlight.ProfileJunos})

	out := string(conn.prepareDisplayOutput([]byte("secretpass\ninterface ge-0/0/0 down\n"), false))

	if strings.Contains(out, "secretpass") {
		t.Fatalf("output leaked password: %q", out)
	}
	if !strings.Contains(out, "********") {
		t.Fatalf("output missing password mask: %q", out)
	}
	if !strings.Contains(out, "\x1b[34mge-0/0/0\x1b[0m") || !strings.Contains(out, "\x1b[31mdown\x1b[0m") {
		t.Fatalf("output missing highlighting after password mask: %q", out)
	}
}

func TestHighlightingDoesNotModifyLastOutput(t *testing.T) {
	conn := NewConnector("edge01", "admin", nil, nil)
	conn.SetHighlightOptions(highlight.Options{Enabled: true, Profile: highlight.ProfileJunos})
	raw := []byte("interface ge-0/0/0 down\n")
	conn.ringBuf.Write(raw)

	display := string(conn.prepareDisplayOutput(raw, false))
	if !strings.Contains(display, "\x1b[34mge-0/0/0\x1b[0m") {
		t.Fatalf("display output not highlighted: %q", display)
	}
	if strings.Contains(conn.LastOutput(), "\x1b[") {
		t.Fatalf("LastOutput contains ANSI: %q", conn.LastOutput())
	}
	if conn.LastOutput() != string(raw) {
		t.Fatalf("LastOutput = %q, want raw %q", conn.LastOutput(), raw)
	}
}
