//go:build (linux || darwin) && !hardware

package agent

import "testing"

func TestHardwareStubReturnsError(t *testing.T) {
	_, err := NewPIVProvider("", nil)
	if err == nil {
		t.Error("NewPIVProvider() expected error (stub), got nil")
	}
	if err != ErrHardwareNotCompiled {
		t.Errorf("NewPIVProvider() error = %v, want %v", err, ErrHardwareNotCompiled)
	}

	_, err = NewFIDO2Provider()
	if err == nil {
		t.Error("NewFIDO2Provider() expected error (stub), got nil")
	}
	if err != ErrHardwareNotCompiled {
		t.Errorf("NewFIDO2Provider() error = %v, want %v", err, ErrHardwareNotCompiled)
	}
}
