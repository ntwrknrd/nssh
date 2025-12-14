//go:build linux || darwin

package session

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"

	"filippo.io/age"
	"github.com/ntwrknrd/nssh/internal/agent"
	"github.com/ntwrknrd/nssh/internal/secret"
	"github.com/ntwrknrd/nssh/internal/session/mode"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/ntwrknrd/nssh/internal/vault"
	"golang.org/x/term"
)

// Unlock spawns an agent and unlocks the vault.
// Handles mode detection, passphrase/PIN collection, and agent spawning.
// If useStdin is true, reads passphrase from stdin (for automation).
func Unlock(mgr *vault.Manager, useStdin bool) error {
	// Already unlocked?
	if !mgr.NeedsUnlock() {
		return nil
	}

	// Detect mode
	detectedMode, err := vault.DetectSecurityMode(mgr.ConfigDir())
	if err != nil {
		return err
	}

	switch mode.Mode(detectedMode) {
	case mode.Software:
		return unlockSoftware(mgr, useStdin)
	case mode.PIV:
		return unlockPIV(useStdin)
	default:
		return fmt.Errorf("unsupported mode: %s", detectedMode)
	}
}

// unlockSoftware handles software mode unlock with passphrase.
func unlockSoftware(mgr *vault.Manager, useStdin bool) error {
	// Collect passphrase
	passphrase, err := collectPassphrase(useStdin, "Unlock nssh credentials")
	if err != nil {
		return err
	}
	defer clearBytes(passphrase)

	// Get software store and unlock
	store := mgr.SoftwareStore()
	if store == nil {
		return errors.New("no software store available")
	}

	if err := store.UnlockWithPassphrase(passphrase); err != nil {
		return err
	}

	// Get identity from unlocked store
	identity, err := store.Identity()
	if err != nil {
		return fmt.Errorf("get identity: %w", err)
	}

	x25519, ok := identity.(*age.X25519Identity)
	if !ok {
		return errors.New("unexpected identity type")
	}

	// Spawn agent with identity
	identitySecret := secret.NewFromString(x25519.String())
	defer identitySecret.Destroy()

	return agent.Spawn(identitySecret)
}

// unlockPIV handles PIV/YubiKey mode unlock with PIN.
func unlockPIV(useStdin bool) error {
	// Collect PIN
	pin, err := collectPassphrase(useStdin, "YubiKey PIN")
	if err != nil {
		return err
	}

	pinSecret := secret.New(pin)
	defer pinSecret.Destroy()

	return agent.SpawnPIV(pinSecret)
}

// collectPassphrase collects a passphrase/PIN from stdin or TTY prompt.
func collectPassphrase(useStdin bool, prompt string) ([]byte, error) {
	if useStdin {
		return readPassphraseFromReader(os.Stdin)
	}

	if term.IsTerminal(int(os.Stdin.Fd())) {
		buf, err := ui.PasswordSecure(prompt)
		if err != nil {
			return nil, err
		}
		// Copy bytes before destroying buffer
		result := append([]byte(nil), buf.Bytes()...)
		buf.Destroy()
		return result, nil
	}

	return nil, errors.New("no TTY available (use --stdin for automation)")
}

// readPassphraseFromReader reads a single line passphrase from a reader.
func readPassphraseFromReader(r io.Reader) ([]byte, error) {
	reader := bufio.NewReader(r)
	line, err := reader.ReadBytes('\n')
	if err != nil && err != io.EOF {
		return nil, err
	}
	// Trim trailing newline
	if len(line) > 0 && line[len(line)-1] == '\n' {
		line = line[:len(line)-1]
	}
	if len(line) > 0 && line[len(line)-1] == '\r' {
		line = line[:len(line)-1]
	}
	return line, nil
}

// clearBytes overwrites a byte slice with zeros.
func clearBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
