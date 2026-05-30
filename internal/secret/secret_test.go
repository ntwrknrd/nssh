package secret

import (
	"bytes"
	"fmt"
	"testing"
)

func TestSecret_Use(t *testing.T) {
	s := NewFromString("password123")
	defer s.Destroy()

	var called bool
	err := s.Use(func(b []byte) error {
		called = true
		if string(b) != "password123" {
			t.Errorf("Use callback got %q, want %q", b, "password123")
		}
		return nil
	})

	if err != nil {
		t.Errorf("Use() error: %v", err)
	}
	if !called {
		t.Error("Use callback was not called")
	}
}

func TestSecret_UseString(t *testing.T) {
	s := NewFromString("password123")
	defer s.Destroy()

	err := s.UseString(func(str string) error {
		if str != "password123" {
			t.Errorf("UseString callback got %q, want %q", str, "password123")
		}
		return nil
	})

	if err != nil {
		t.Errorf("UseString() error: %v", err)
	}
}

func TestSecret_UseAfterDestroy(t *testing.T) {
	s := NewFromString("password123")
	s.Destroy()

	err := s.Use(func(b []byte) error {
		t.Error("callback should not be called after Destroy")
		return nil
	})

	if err == nil {
		t.Error("Use() after Destroy should return error")
	}
}

func TestSecret_Len(t *testing.T) {
	s := NewFromString("12345")
	defer s.Destroy()

	if s.Len() != 5 {
		t.Errorf("Len() = %d, want 5", s.Len())
	}
}

func TestSecret_LenAfterDestroy(t *testing.T) {
	s := NewFromString("12345")
	s.Destroy()

	if s.Len() != 0 {
		t.Errorf("Len() after Destroy = %d, want 0", s.Len())
	}
}

func TestSecret_IsDestroyed(t *testing.T) {
	s := NewFromString("test")

	if s.IsDestroyed() {
		t.Error("IsDestroyed() = true before Destroy")
	}

	s.Destroy()

	if !s.IsDestroyed() {
		t.Error("IsDestroyed() = false after Destroy")
	}
}

func TestSecret_DoubleDestroy(t *testing.T) {
	s := NewFromString("test")
	s.Destroy()
	// Should not panic
	s.Destroy()
}

func TestSecret_String_Panics(t *testing.T) {
	s := NewFromString("secret")
	defer s.Destroy()

	defer func() {
		if r := recover(); r == nil {
			t.Error("String() should panic")
		}
	}()

	_ = s.String()
}

func TestSecret_GoString_Panics(t *testing.T) {
	s := NewFromString("secret")
	defer s.Destroy()

	defer func() {
		if r := recover(); r == nil {
			t.Error("GoString() should panic")
		}
	}()

	_ = s.GoString()
}

func TestSecret_Format_ShowsError(t *testing.T) {
	s := NewFromString("secret")
	defer s.Destroy()

	// fmt catches panics in Format() and converts to error output
	// The panic IS triggered but caught by fmt, resulting in "%!v(PANIC=...)"
	result := fmt.Sprintf("%v", s)
	if result == "" {
		t.Error("Format() should produce output")
	}
	// The output should indicate a panic occurred, not reveal the secret
	if result == "secret" {
		t.Error("Format() should not reveal secret value")
	}
	// Check that it contains PANIC indicator
	if !bytes.Contains([]byte(result), []byte("PANIC")) {
		t.Errorf("Format() output should contain PANIC indicator, got %q", result)
	}
}

func TestNewFromString(t *testing.T) {
	s := NewFromString("password")
	defer s.Destroy()

	err := s.Use(func(b []byte) error {
		if string(b) != "password" {
			t.Errorf("got %q, want %q", b, "password")
		}
		return nil
	})
	if err != nil {
		t.Errorf("Use() error: %v", err)
	}
}
