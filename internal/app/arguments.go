package app

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/ntwrknrd/nssh/internal/connect"
	"github.com/ntwrknrd/nssh/internal/ssh/sshargs"
)

// rootInvocation keeps SSH data out of Cobra's subcommand/flag parser.
type rootInvocation struct {
	commandArgs []string
	request     *connect.Request
}

func parseRootArgs(args []string) (rootInvocation, error) {
	var globals, options []string
	var request *connect.Request
	terminated := false
	for i := 0; i < len(args); {
		arg := args[i]
		if !terminated && arg == "--" {
			terminated = true
			i++
			continue
		}
		if !terminated && request == nil && arg == "--select" {
			return rootInvocation{commandArgs: globals, request: &connect.Request{SSHArgs: options}}, nil
		}
		if !terminated && request == nil && (arg == "--target" || strings.HasPrefix(arg, "--target=")) {
			target, found := strings.CutPrefix(arg, "--target=")
			if !found {
				i++
				if i == len(args) {
					return rootInvocation{}, fmt.Errorf("--target requires a destination")
				}
				target = args[i]
			}
			var err error
			request, err = destinationRequest(target, options)
			if err != nil {
				return rootInvocation{}, err
			}
			request.LiteralTarget = true
			options = request.SSHArgs
			i++
			continue
		}
		if !terminated && request == nil && (arg == "-h" || arg == "--help" || arg == "--version" || arg == "--verbose" || arg == "--explain") {
			globals = append(globals, arg)
			i++
			continue
		}
		// Keep the existing standalone explanation shortcut. With a value,
		// -e is always the SSH escape-character option.
		if request == nil && !terminated && arg == "-e" && i == len(args)-1 {
			return rootInvocation{commandArgs: append(globals, "--explain")}, nil
		}
		if !terminated && strings.HasPrefix(arg, "-") && arg != "-" {
			atoms, n, err := sshargs.Next(args[i:])
			if err != nil {
				return rootInvocation{}, err
			}
			// nssh's established verbosity ladder also applies inside clusters.
			// Otherwise retain the original option spelling and value verbatim.
			var word strings.Builder
			word.Grow(len(arg))
			word.WriteByte('-')
			changed := false
			for _, atom := range atoms {
				if atom.Name == 'v' || atom.Name == 'V' {
					globals = append(globals, "-"+string(atom.Name))
					changed = true
				} else {
					word.WriteByte(atom.Name)
					if sshargs.TakesValue(atom.Name) && n == 1 {
						word.WriteString(atom.Value)
					}
				}
			}
			if !changed {
				options = append(options, args[i:i+n]...)
			} else if word.Len() > 1 {
				options = append(options, word.String())
				if n == 2 {
					options = append(options, args[i+1])
				}
			}
			i += n
			continue
		}
		if request == nil {
			if !terminated && subcommands[arg] {
				return rootInvocation{commandArgs: args}, nil
			}
			var err error
			request, err = destinationRequest(arg, options)
			if err != nil {
				return rootInvocation{}, err
			}
			options = request.SSHArgs
			i++
			continue
		}
		// The first command word seals the remaining argv, including flags,
		// commas, empty arguments and literal -- words.
		request.RemoteCommand = args[i:]
		break
	}
	if request == nil {
		if len(options) != 0 {
			return rootInvocation{}, fmt.Errorf("SSH destination is required")
		}
		return rootInvocation{commandArgs: globals}, nil
	}
	request.SSHArgs = options
	return rootInvocation{commandArgs: globals, request: request}, nil
}

// Insert destination-derived overrides at its original position in the option
// stream: OpenSSH uses the first User and Port, including URI/user@host values.
func destinationRequest(target string, options []string) (*connect.Request, error) {
	host, user, port, err := parseDestination(target)
	if err != nil {
		return nil, err
	}
	if user != "" {
		options = append(options, "-l", user)
	}
	if port != "" {
		options = append(options, "-p", port)
	}
	return &connect.Request{Host: host, SSHArgs: options}, nil
}

func parseDestination(target string) (host, user, port string, err error) {
	invalid := func() (string, string, string, error) {
		return "", "", "", fmt.Errorf("invalid SSH destination %q", target)
	}
	if !strings.HasPrefix(target, "ssh://") {
		user, host = parseUserHost(target)
		if host == "" || strings.ContainsRune(target, 0) || strings.HasPrefix(target, "@") {
			return invalid()
		}
		return host, user, "", nil
	}
	authority := strings.TrimPrefix(target, "ssh://")
	// OpenSSH URI userinfo uses the first @; optional semicolon connection
	// parameters are ignored. Match OpenSSH percent and plus decoding.
	if rawUser, rest, ok := strings.Cut(authority, "@"); ok {
		rawUser, _, _ = strings.Cut(rawUser, ";")
		if rawUser == "" {
			return invalid()
		}
		user, err = url.QueryUnescape(rawUser)
		if err != nil || user == "" || strings.ContainsRune(user, 0) {
			return invalid()
		}
		authority = rest
	}
	uri, err := url.Parse("ssh://" + authority)
	if err != nil || uri.User != nil || uri.Host == "" || uri.RawQuery != "" || uri.ForceQuery || uri.Fragment != "" || (uri.Path != "" && uri.Path != "/") {
		return invalid()
	}
	host, port = uri.Hostname(), uri.Port()
	if host == "" || strings.ContainsAny(host, " \t\r\n@,/#?%") {
		return invalid()
	}
	if strings.Contains(host, ":") && !strings.HasPrefix(uri.Host, "[") {
		return invalid()
	}
	if port != "" {
		n, e := strconv.Atoi(port)
		if e != nil || n <= 0 || n > 65535 {
			return invalid()
		}
		port = strconv.Itoa(n)
	}
	return host, user, port, nil
}
