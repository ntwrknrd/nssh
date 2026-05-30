package secret

import (
	"fmt"

	"github.com/awnumar/memguard"
)

func init() {
	// Initialize memguard's secure memory handling.
	// This sets up memory locking and secure allocation.
	memguard.CatchInterrupt()
}

// Secret wraps memguard.LockedBuffer with panic-on-misuse semantics.
// The API is designed to prevent accidental leaks:
//   - No String/GoString/Format methods (panic on fmt usage)
//   - Callback-based access via Use() prevents reference retention
type Secret struct {
	buf *memguard.LockedBuffer
}

// NewFromString creates a Secret from a string.
func NewFromString(s string) *Secret {
	return &Secret{buf: memguard.NewBufferFromBytes([]byte(s))}
}

// Use provides temporary access to secret bytes via callback.
// The byte slice is ONLY valid during the callback execution.
// Callers MUST NOT:
//   - Store the slice or any derived references
//   - Convert to string (use UseString for that)
//   - Pass the slice to functions that retain it
//
// Example:
//
//	secret.Use(func(password []byte) error {
//	    _, err := conn.Write(password)
//	    return err
//	})
func (s *Secret) Use(fn func([]byte) error) error {
	if s.buf == nil {
		return fmt.Errorf("secret: already destroyed")
	}
	return fn(s.buf.Bytes())
}

// UseString provides temporary access to secret as string via callback.
// Same restrictions as Use() apply - do not retain the string.
//
// SECURITY TRADE-OFF: This method creates a Go string copy that cannot be
// explicitly wiped (Go strings are immutable, no access to backing array).
// The copy persists until GC collects it - potentially seconds to minutes.
//
// When to use UseString vs Use:
//   - UseString: APIs that require string (e.g., sql.DB.Query placeholders)
//   - Use: Prefer for I/O operations where []byte works (PTY writes, HTTP bodies)
//
// The memguard-protected original remains secure; only the temporary copy
// is vulnerable to memory inspection during its GC lifetime.
func (s *Secret) UseString(fn func(string) error) error {
	if s.buf == nil {
		return fmt.Errorf("secret: already destroyed")
	}
	str := string(s.buf.Bytes())
	return fn(str)
}

// Len returns the length of the secret without exposing contents.
func (s *Secret) Len() int {
	if s.buf == nil {
		return 0
	}
	return s.buf.Size()
}

// IsDestroyed returns true if the secret has been destroyed.
func (s *Secret) IsDestroyed() bool {
	return s.buf == nil
}

// Destroy zeros and releases the secret memory.
func (s *Secret) Destroy() {
	if s.buf != nil {
		s.buf.Destroy()
		s.buf = nil
	}
}

// String panics to prevent accidental logging.
func (s *Secret) String() string {
	panic("secret: attempted to convert secret to string - this is a bug")
}

// GoString panics to prevent fmt.Printf("%#v", secret).
func (s *Secret) GoString() string {
	panic("secret: attempted to format secret - this is a bug")
}

// Format panics on any fmt.Formatter usage.
func (s *Secret) Format(f fmt.State, c rune) {
	panic("secret: attempted to format secret - this is a bug")
}
