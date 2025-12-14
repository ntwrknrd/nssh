//go:build !(darwin && secureenclave && cgo)

package agent

import "errors"

// ErrSecureEnclaveNotAvailable is returned when Secure Enclave provider
// functions are called but the binary was not built with the required tags.
var ErrSecureEnclaveNotAvailable = errors.New("secure enclave not available; requires macOS with -tags secureenclave")

// NewSecureEnclaveProvider returns an error indicating Secure Enclave support
// is not available. To use macOS Secure Enclave, build on macOS with:
// go build -tags secureenclave
func NewSecureEnclaveProvider() (Provider, error) {
	return nil, ErrSecureEnclaveNotAvailable
}
