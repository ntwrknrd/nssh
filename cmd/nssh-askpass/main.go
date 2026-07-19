package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ntwrknrd/nssh/internal/ssh/askpass"
)

func main() {
	socketPath := os.Getenv(askpass.SocketEnv)
	nonce := os.Getenv(askpass.NonceEnv)
	if socketPath == "" || nonce == "" {
		fmt.Fprintln(os.Stderr, "nssh-askpass: missing askpass environment")
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	prompt := strings.Join(os.Args[1:], " ")
	password, err := askpass.RequestPassword(ctx, socketPath, nonce, prompt)
	if err != nil {
		fmt.Fprintln(os.Stderr, "nssh-askpass:", err)
		os.Exit(1)
	}
	if _, err := os.Stdout.Write(password); err != nil {
		fmt.Fprintln(os.Stderr, "nssh-askpass:", err)
		os.Exit(1)
	}
}
