package bench

import (
	"errors"
	"fmt"

	"github.com/ntwrknrd/nssh/internal/connect"
)

func resolveBenchmarkHost(host string) (string, error) {
	resolved, err := connect.ResolveHostname(host)
	if err == nil {
		return resolved, nil
	}

	var notFound *connect.HostNotFoundError
	if errors.As(err, &notFound) {
		return "", fmt.Errorf("host not found: %s", notFound.Hostname)
	}
	return "", err
}
