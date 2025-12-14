// Package exit provides process exit codes and typed errors used across the app.
package exit

// ExitError represents an error with a specific exit code.
type ExitError struct {
	Code    int
	Message string
	Cause   error
}

func (e *ExitError) Error() string { return e.Message }
func (e *ExitError) Unwrap() error { return e.Cause }

const (
	ExitSuccess          = 0
	ExitGeneralError     = 1
	ExitConnectionFailed = 2
	ExitAuthFailed       = 3
	ExitHostNotFound     = 4
	ExitVaultError       = 5
	ExitNotExecutable    = 126
	ExitNotFound         = 127
)

// Predefined errors for common failure modes.
var (
	ErrConnectionFailed = &ExitError{Code: ExitConnectionFailed, Message: "connection failed"}
	ErrAuthFailed       = &ExitError{Code: ExitAuthFailed, Message: "authentication failed"}
	ErrHostNotFound     = &ExitError{Code: ExitHostNotFound, Message: "host not found"}
	ErrVault            = &ExitError{Code: ExitVaultError, Message: "vault error"}
)
