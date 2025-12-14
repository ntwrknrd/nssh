package vault

import (
	"fmt"

	"github.com/ntwrknrd/nssh/internal/secret"
)

// extractPassword safely extracts a password string from a secret.Secret.
// This uses UseString to avoid accidentally logging the password.
func extractPassword(password *secret.Secret) (string, error) {
	var pw string
	if err := password.UseString(func(s string) error {
		pw = s
		return nil
	}); err != nil {
		return "", fmt.Errorf("access password: %w", err)
	}
	return pw, nil
}
