//go:build linux || darwin

package software

import "time"

// setNowFunc allows tests to inject a mock time function.
// Returns a function to restore the original.
func setNowFunc(fn func() time.Time) func() {
	old := nowFunc
	nowFunc = fn
	return func() { nowFunc = old }
}
