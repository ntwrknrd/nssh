// Package exit provides process exit codes and typed errors.
//
// This package defines standard exit codes for nssh and provides [ExitError],
// a typed error that carries both an exit code and message. Commands return
// ExitError to signal specific failure modes to the shell.
//
// # Exit Codes
//
// Standard exit codes follow Unix conventions:
//
//	ExitSuccess          = 0    // Successful completion
//	ExitGeneralError     = 1    // General/unspecified error
//	ExitConnectionFailed = 2    // SSH connection failed
//	ExitAuthFailed       = 3    // Authentication failed
//	ExitHostNotFound     = 4    // Host not in SSH config
//	ExitNotExecutable    = 126  // Command not executable
//	ExitNotFound         = 127  // Command not found
//
// # Usage
//
// Return an ExitError to exit with a specific code:
//
//	if err != nil {
//	    return &exit.ExitError{Code: exit.ExitConnectionFailed, Message: "timeout"}
//	}
//
// Or use predefined errors:
//
//	return exit.ErrAuthFailed
//
// The main function checks for ExitError and calls os.Exit with the code.
package exit
