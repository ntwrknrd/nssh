// Package cp provides the SCP file copy command.
package cp

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/ntwrknrd/nssh/internal/connect"
	"github.com/ntwrknrd/nssh/internal/ssh/connector"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/spf13/cobra"
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
			return runCp(cmd.Context(), args[0], args[1], recursive, preserve, quiet, verbose)
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

func runCp(ctx context.Context, source, dest string, recursive, preserve, quiet, verbose bool) error {
	// Parse direction and paths
	user, hostSearch, remotePath, localPath, direction, err := detectDirection(source, dest)
	if err != nil {
		ui.Error("%s", err)
		return err
	}

	resolved, err := resolveHostForConnect(hostSearch, user)
	if err != nil {
		ui.Error("Failed to resolve host: %s", err)
		return err
	}

	defer destroyResolvedPasswords(resolved)
	scpUser := resolved.Username
	askpassEnv, err := startResolvedAskpass(ctx, resolved)
	if err != nil {
		return err
	}
	if askpassEnv != nil {
		defer askpassEnv.Cleanup()
	}

	// Build remote spec using the resolved endpoint; nssh renders SSH options
	// directly because OpenSSH config is disabled.
	var remoteSpec string
	if scpUser != "" {
		remoteSpec = fmt.Sprintf("%s@%s:%s", scpUser, resolved.Hostname, remotePath)
	} else {
		remoteSpec = fmt.Sprintf("%s:%s", resolved.Hostname, remotePath)
	}

	// Build SCP command
	args := connector.RenderSSHOptions(resolved.SSH, 0)
	if resolved.Port != 0 && resolved.Port != 22 {
		args = append(args, "-P", fmt.Sprintf("%d", resolved.Port))
	}
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
	var proxyEnv []string
	if askpassEnv != nil {
		proxyEnv = askpassEnv.ProxyEnv
	}
	hostKeyPrep, err := prepareResolvedHostKey(ctx, resolved, nil, proxyEnv)
	if err != nil {
		return err
	}
	if hostKeyPrep != nil {
		defer hostKeyPrep.Cleanup()
		args = append(args, hostKeyPrep.SSHArgs()...)
	}

	if direction == "pull" {
		args = append(args, remoteSpec, localPath)
	} else {
		args = append(args, localPath, remoteSpec)
	}

	var env []string
	if askpassEnv != nil {
		env = askpassEnv.Env
	}
	return runScp(args, env)
}

var resolveHostForConnect = connect.ResolveHostForConnect
var startResolvedAskpass = connect.StartResolvedAskpassEnvironment
var prepareResolvedHostKey = connect.PrepareResolvedHostKey

var runScp = runScpCommand

func runScpCommand(args, env []string) error {
	cmd := exec.Command("scp", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	return cmd.Run()
}

func destroyResolvedPasswords(resolved *connect.ResolvedHost) {
	if resolved == nil {
		return
	}
	if resolved.Credential != nil && resolved.Credential.Password != nil {
		resolved.Credential.Password.Destroy()
		resolved.Credential.Password = nil
	}
	if resolved.Proxy != nil && resolved.Proxy.Credential != nil && resolved.Proxy.Credential.Password != nil {
		resolved.Proxy.Credential.Password.Destroy()
		resolved.Proxy.Credential.Password = nil
	}
}
