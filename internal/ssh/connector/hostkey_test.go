//go:build unix

package connector

import (
	"io"
	"os"
	"testing"

	"github.com/creack/pty"
)

func TestHandleHostKeyPromptUsesCallbackAndPinsAcceptOnce(t *testing.T) {
	withTerminalStdin(t)
	conn := NewConnector("edge01", "", nil, nil)
	conn.ptyFile = tempPTYFile(t)
	stdinCh := make(chan stdinResult, 1)
	stdinCh <- stdinResult{data: []byte("buffered")}
	conn.stdinCh = stdinCh

	var got HostKeyPrompt
	conn.SetHostKeyPromptFunc(func(prompt HostKeyPrompt) HostKeyAction {
		got = prompt
		if prompt.Stdin == nil {
			t.Fatal("prompt stdin reader is nil")
		}
		buf := make([]byte, len("buffered"))
		n, err := prompt.Stdin.Read(buf)
		if err != nil && err != io.EOF {
			t.Fatalf("read prompt stdin: %v", err)
		}
		if string(buf[:n]) != "buffered" {
			t.Fatalf("prompt stdin = %q, want buffered", string(buf[:n]))
		}
		return HostKeyAcceptOnce
	})

	output := []byte("The authenticity of host 'edge01 (192.0.2.10)' can't be established.\nED25519 key fingerprint is SHA256:abcdefghijklmnopqrstuvwxyz123456789ABCDEF.\nAre you sure you want to continue connecting (yes/no/[fingerprint])?")
	handled, result := conn.handleHostKeyPrompt(output)

	if !handled {
		t.Fatal("host key prompt was not handled")
	}
	if result != HostKeyResultRestart {
		t.Fatalf("result = %v, want HostKeyResultRestart", result)
	}
	if got.Host != "edge01" || got.KeyType != "ED25519" || got.Fingerprint != "SHA256:abcdefghijklmnopqrstuvwxyz123456789ABCDEF" || got.Changed {
		t.Fatalf("prompt = %+v", got)
	}
	if conn.pinnedHostKey == nil || conn.pinnedHostKey.fingerprint != got.Fingerprint {
		t.Fatalf("pinnedHostKey = %+v, want fingerprint %q", conn.pinnedHostKey, got.Fingerprint)
	}
}

func withTerminalStdin(t *testing.T) {
	t.Helper()
	ptmx, tty, err := pty.Open()
	if err != nil {
		t.Fatalf("open pty: %v", err)
	}
	oldStdin := os.Stdin
	os.Stdin = tty
	t.Cleanup(func() {
		os.Stdin = oldStdin
		_ = tty.Close()
		_ = ptmx.Close()
	})
}

func TestHandleHostKeyPromptRejectsInteractivePromptWithoutCallback(t *testing.T) {
	conn := NewConnector("edge01", "", nil, nil)
	conn.ptyFile = tempPTYFile(t)

	output := []byte("The authenticity of host 'edge01 (192.0.2.10)' can't be established.\nAre you sure you want to continue connecting (yes/no/[fingerprint])?")
	handled, result := conn.handleHostKeyPrompt(output)

	if !handled {
		t.Fatal("host key prompt was not handled")
	}
	if result != HostKeyResultAbort {
		t.Fatalf("result = %v, want HostKeyResultAbort", result)
	}
}

func TestHandleHostKeyPromptAutoAcceptsPermissiveStrictHostKeyChecking(t *testing.T) {
	conn := NewConnector("edge01", "", nil, []string{"-o", "StrictHostKeyChecking=accept-new"})
	conn.ptyFile = tempPTYFile(t)

	output := []byte("The authenticity of host 'edge01 (192.0.2.10)' can't be established.\nAre you sure you want to continue connecting (yes/no/[fingerprint])?")
	handled, result := conn.handleHostKeyPrompt(output)

	if !handled {
		t.Fatal("host key prompt was not handled")
	}
	if result != HostKeyResultContinue {
		t.Fatalf("result = %v, want HostKeyResultContinue", result)
	}
}

func tempPTYFile(t *testing.T) *os.File {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "pty-*")
	if err != nil {
		t.Fatalf("create temp pty file: %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}
