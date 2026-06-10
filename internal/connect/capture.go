package connect

import (
	"context"
	"errors"

	"github.com/ntwrknrd/nssh/internal/exit"
)

func RunRemoteCommandCapture(ctx context.Context, hostname, command string) ([]byte, int, error) {
	resolved, err := ResolveHostForConnect(hostname, "")
	if err != nil {
		return nil, 0, err
	}
	cfg := resolved.Config
	sshArgs := []string{"-T", "--", command}
	conn := newConnector(resolved, sshArgs, cfg)
	out, err := conn.RunCapture(ctx)
	if err == nil {
		return out, 0, nil
	}
	var exitErr *exit.ExitError
	if errors.As(err, &exitErr) {
		return out, exitErr.Code, err
	}
	return out, 1, err
}
