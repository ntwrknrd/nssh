// Package cp provides the SCP file copy command.
package cp

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"

	"github.com/creack/pty"
	clisession "github.com/ntwrknrd/nssh/internal/cli/session"
	"github.com/ntwrknrd/nssh/internal/secret"
	"github.com/ntwrknrd/nssh/internal/ssh/sshconfig"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/ntwrknrd/nssh/internal/vault"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// NewCmd creates the 'cp' command.
func NewCmd() *cobra.Command {
	var (
		recursive bool
		preserve  bool
		quiet     bool
		verbose   bool
	)

	cmd := &cobra.Command{
		Use:   "cp <source> <dest>",
		Short: "Copy files via SCP",
		Long: `Copy files to/from SSH hosts.

Direction is auto-detected based on which argument contains ':'

Examples:
  nssh cp myhost:~/file.txt ./           # pull from remote
  nssh cp ./file.txt myhost:~/           # push to remote
  nssh cp -r myhost:~/dir ./local/       # recursive pull`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return nil
			}
			return cobra.ExactArgs(2)(cmd, args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			return runCp(args[0], args[1], recursive, preserve, quiet, verbose)
		},
	}

	cmd.Flags().BoolVarP(&recursive, "recursive", "r", false, "Copy directories")
	cmd.Flags().BoolVarP(&preserve, "preserve", "p", false, "Preserve times/modes")
	cmd.Flags().BoolVarP(&quiet, "quiet", "q", false, "Disable progress")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Verbose output")

	ui.ApplyStyledHelp(cmd)
	return cmd
}

// findColonSeparator finds the host:path separator colon, matching OpenSSH scp behavior.
// Returns the index of the separator colon, or -1 if this is a local path.
// Rules (from OpenSSH misc.c colon()):
// - Leading colon (:file) -> local file
// - Slash before colon (/path:file or ./file:name) -> local path
// - Bracketed IPv6 [addr]:path -> colon after ] is separator
// - Otherwise first colon is the separator
func findColonSeparator(spec string) int {
	if len(spec) == 0 || spec[0] == ':' {
		return -1
	}

	inBracket := spec[0] == '['

	for i := 0; i < len(spec); i++ {
		ch := spec[i]

		// user@[ipv6] pattern
		if ch == '@' && i+1 < len(spec) && spec[i+1] == '[' {
			inBracket = true
		}
		// End of bracketed IPv6, check for following colon
		if ch == ']' && i+1 < len(spec) && spec[i+1] == ':' && inBracket {
			return i + 1
		}
		// First colon outside brackets
		if ch == ':' && !inBracket {
			return i
		}
		// Slash before colon means local path
		if ch == '/' {
			return -1
		}
	}
	return -1
}

// parseRemoteSpec parses "[user@]host:path" into components.
// Handles IPv6 addresses in brackets, e.g., "[2001:db8::1]:/tmp/file" or "user@[::1]:~/file"
func parseRemoteSpec(spec string) (user, host, path string, err error) {
	colonIdx := findColonSeparator(spec)
	if colonIdx < 0 {
		return "", "", "", fmt.Errorf("not a remote spec")
	}

	hostPart := spec[:colonIdx]
	path = spec[colonIdx+1:]

	// Parse user@host with bracket awareness
	atIdx := -1
	inBracket := false
	for i := 0; i < len(hostPart); i++ {
		ch := hostPart[i]
		switch ch {
		case '[':
			inBracket = true
		case ']':
			inBracket = false
		case '@':
			if !inBracket {
				atIdx = i
			}
		}
	}

	if atIdx > 0 {
		user = hostPart[:atIdx]
		host = hostPart[atIdx+1:]
	} else {
		host = hostPart
	}

	return user, host, path, nil
}

// detectDirection determines transfer direction and parses paths.
func detectDirection(source, dest string) (user, host, remotePath, localPath, direction string, err error) {
	sourceIsRemote := findColonSeparator(source) >= 0
	destIsRemote := findColonSeparator(dest) >= 0

	if sourceIsRemote && destIsRemote {
		return "", "", "", "", "", fmt.Errorf("cannot copy between two remote hosts")
	}
	if !sourceIsRemote && !destIsRemote {
		return "", "", "", "", "", fmt.Errorf("one path must be remote (host:path format)")
	}

	if sourceIsRemote {
		user, host, remotePath, err = parseRemoteSpec(source)
		if err != nil {
			return "", "", "", "", "", err
		}
		localPath = dest
		direction = "pull"
	} else {
		user, host, remotePath, err = parseRemoteSpec(dest)
		if err != nil {
			return "", "", "", "", "", err
		}
		localPath = source
		direction = "push"
	}

	return user, host, remotePath, localPath, direction, nil
}

func runCp(source, dest string, recursive, preserve, quiet, verbose bool) error {
	// Parse direction and paths
	user, hostSearch, remotePath, localPath, direction, err := detectDirection(source, dest)
	if err != nil {
		ui.Error("%s", err)
		return err
	}

	// Resolve host from SSH config
	parser := sshconfig.NewParser()
	hostEntry, err := parser.FindHost(hostSearch)
	if err != nil {
		ui.Error("Failed to get host details: %s", err)
		return err
	}
	if hostEntry == nil {
		ui.Error("Host not found: %s", hostSearch)
		return fmt.Errorf("host not found: %s", hostSearch)
	}

	includeFile := filepath.Base(hostEntry.SourceFile)

	// Resolve credentials
	mgr, err := clisession.NewManager(vault.Auto())
	if err != nil {
		ui.Error("Failed to load credentials: %s", err)
		return err
	}

	_ = clisession.TryUnlockIfTTY(mgr)

	var password *secret.Secret
	var scpUser string

	if user != "" {
		scpUser = user
	} else if hostEntry != nil && hostEntry.User() != "" {
		scpUser = hostEntry.User()
	}

	// Resolve credential using the Host identifier (how credentials are indexed in the vault)
	// inventory host credentials are keyed by Host, not by HostName
	cred, _ := mgr.ResolveCredential(hostSearch, filepath.Base(includeFile), scpUser)
	if cred != nil {
		if scpUser == "" {
			scpUser = cred.Username
		}
		password = cred.Password
	}

	// Build remote spec using the Host identifier (hostSearch) so SSH config applies
	// SSH config directives (ProxyJump, Port, IdentityFile, etc.) match on Host
	var remoteSpec string
	if scpUser != "" {
		remoteSpec = fmt.Sprintf("%s@%s:%s", scpUser, hostSearch, remotePath)
	} else {
		remoteSpec = fmt.Sprintf("%s:%s", hostSearch, remotePath)
	}

	// Build SCP command
	args := []string{}
	if recursive {
		args = append(args, "-r")
	}
	if preserve {
		args = append(args, "-p")
	}
	if quiet {
		args = append(args, "-q")
	}
	if verbose {
		args = append(args, "-v")
	}

	if direction == "pull" {
		args = append(args, remoteSpec, localPath)
	} else {
		args = append(args, localPath, remoteSpec)
	}

	// Run SCP with PTY for password injection
	return runScpWithPty(args, password)
}

var passwordPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)password:\s*$`),
	regexp.MustCompile(`(?i)passcode:\s*$`),
}

func matchPasswordPrompt(data []byte) bool {
	for _, re := range passwordPatterns {
		if re.Match(data) {
			return true
		}
	}
	return false
}

func runScpWithPty(args []string, password *secret.Secret) error {
	cmd := exec.Command("scp", args...)

	// Start with PTY
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return fmt.Errorf("failed to start scp: %w", err)
	}
	defer func() { _ = ptmx.Close() }()

	// Save and restore terminal state
	if term.IsTerminal(int(os.Stdin.Fd())) {
		oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
		if err == nil {
			defer func() { _ = term.Restore(int(os.Stdin.Fd()), oldState) }()
		}
	}

	// Copy stdin to PTY in goroutine
	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := os.Stdin.Read(buf)
			if err != nil {
				return
			}
			_, _ = ptmx.Write(buf[:n])
		}
	}()

	// Read from PTY, handle password prompts, write to stdout
	buf := make([]byte, 4096)
	ringBuf := make([]byte, 0, 2048)
	passwordSent := false

	for {
		n, err := ptmx.Read(buf)
		if err != nil {
			break
		}

		// Append to ring buffer for prompt detection
		ringBuf = append(ringBuf, buf[:n]...)
		if len(ringBuf) > 2048 {
			ringBuf = ringBuf[len(ringBuf)-2048:]
		}

		// Check for password prompt
		if password != nil && !passwordSent && matchPasswordPrompt(ringBuf) {
			_ = password.Use(func(pw []byte) error {
				_, _ = ptmx.Write(pw)
				_, _ = ptmx.Write([]byte{'\n'})
				return nil
			})
			passwordSent = true
		}

		_, _ = os.Stdout.Write(buf[:n])
	}

	return cmd.Wait()
}
