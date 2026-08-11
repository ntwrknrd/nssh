package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/ntwrknrd/nssh/internal/ssh/askpass"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "nssh-askpass:", err)
		os.Exit(1)
	}
}

// run keeps the deferred cancel reachable; main only translates the error into
// an exit status.
func run() error {
	socketPath := os.Getenv(askpass.SocketEnv)
	nonce := os.Getenv(askpass.NonceEnv)
	if socketPath == "" || nonce == "" {
		return errors.New("missing askpass environment")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	password, err := askpass.RequestPassword(ctx, socketPath, nonce)
	if err != nil {
		return err
	}
	if _, err := os.Stdout.Write(password); err != nil {
		return err
	}
	return nil
}
