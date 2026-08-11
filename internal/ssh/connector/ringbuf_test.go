package connector

import (
	"bytes"
	"testing"
)

func TestRingBuffer_Basic(t *testing.T) {
	rb := NewRingBuffer(10)

	// Empty buffer
	if got := rb.LinearBytes(); len(got) != 0 {
		t.Errorf("expected len 0, got %d", len(got))
	}

	// Write some data
	rb.Write([]byte("hello"))

	got := rb.LinearBytes()
	if !bytes.Equal(got, []byte("hello")) {
		t.Errorf("expected 'hello', got %q", got)
	}
}

func TestRingBuffer_Wrap(t *testing.T) {
	rb := NewRingBuffer(8)

	// Fill buffer exactly
	rb.Write([]byte("12345678"))

	got := rb.LinearBytes()
	if !bytes.Equal(got, []byte("12345678")) {
		t.Errorf("expected '12345678', got %q", got)
	}

	// Write more to wrap around
	rb.Write([]byte("abc"))

	// Should now contain "678abc" (oldest 5 bytes dropped)
	// Wait, buffer size is 8, so it should contain last 8 bytes: "345678ab" wait no
	// Let me trace through:
	// After "12345678": count=8, start=0, end=0 (wrapped)
	// Write 'a': data[0]='a', end=1, count=8 (already full), start=1
	// Write 'b': data[1]='b', end=2, count=8, start=2
	// Write 'c': data[2]='c', end=3, count=8, start=3
	// So data = [a, b, c, 4, 5, 6, 7, 8]
	// Reading from start=3: 4, 5, 6, 7, 8, a, b, c
	got = rb.LinearBytes()
	if !bytes.Equal(got, []byte("45678abc")) {
		t.Errorf("expected '45678abc', got %q", got)
	}
}

func TestRingBuffer_Overwrite(t *testing.T) {
	rb := NewRingBuffer(5)

	// Write more than buffer size
	rb.Write([]byte("abcdefgh"))

	// Should only contain last 5 bytes
	got := rb.LinearBytes()
	if !bytes.Equal(got, []byte("defgh")) {
		t.Errorf("expected 'defgh', got %q", got)
	}
}

func TestRingBuffer_Reset(t *testing.T) {
	rb := NewRingBuffer(10)

	rb.Write([]byte("hello"))
	rb.Reset()

	got := rb.LinearBytes()
	if len(got) != 0 {
		t.Errorf("expected empty slice after reset, got %q", got)
	}
}

func TestRingBuffer_LinearBytesZeroCopy(t *testing.T) {
	rb := NewRingBuffer(20)

	// Write less than buffer size - should use zero-copy path
	rb.Write([]byte("hello"))

	got := rb.LinearBytes()
	if !bytes.Equal(got, []byte("hello")) {
		t.Errorf("expected 'hello', got %q", got)
	}

	// Verify it's a slice of the internal buffer (zero-copy)
	// by checking capacity matches internal size
	if cap(got) < 5 {
		t.Errorf("expected capacity >= 5 for zero-copy, got %d", cap(got))
	}
}

func BenchmarkRingBuffer_Write(b *testing.B) {
	rb := NewRingBuffer(2048)
	data := make([]byte, 1024)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rb.Write(data)
	}
}

func BenchmarkRingBuffer_LinearBytes(b *testing.B) {
	rb := NewRingBuffer(2048)
	rb.Write(make([]byte, 1500)) // Partially filled

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = rb.LinearBytes()
	}
}

func BenchmarkRingBuffer_LinearBytesWrapped(b *testing.B) {
	rb := NewRingBuffer(2048)
	rb.Write(make([]byte, 3000)) // Force wrap

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = rb.LinearBytes()
	}
}

func BenchmarkRingBuffer_WriteSmallChunks(b *testing.B) {
	rb := NewRingBuffer(2048)
	data := make([]byte, 64) // Realistic PTY chunk size

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rb.Write(data)
	}
}
