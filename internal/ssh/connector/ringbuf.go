// Package connector provides PTY-based SSH connection handling.
package connector

// RingBuffer provides a fixed-size circular buffer for prompt detection.
// It maintains a sliding window of the most recent N bytes, ensuring
// password prompts are never split across truncation boundaries.
//
// Thread-safety: RingBuffer is NOT thread-safe. Callers must synchronize
// access when used from multiple goroutines.
type RingBuffer struct {
	data      []byte
	size      int
	count     int    // Actual number of bytes in buffer
	start     int    // Read position
	end       int    // Write position
	linearBuf []byte // Pre-allocated buffer for zero-alloc linearization
}

// NewRingBuffer creates a ring buffer with the specified capacity.
func NewRingBuffer(size int) *RingBuffer {
	return &RingBuffer{
		data:      make([]byte, size),
		size:      size,
		linearBuf: make([]byte, size), // Pre-allocate for hot path
	}
}

// Write appends bytes to the buffer, overwriting oldest data if full.
// Uses bulk copy operations for performance in the PTY hot path.
func (r *RingBuffer) Write(p []byte) {
	pLen := len(p)
	if pLen == 0 {
		return
	}

	// Fast path: input >= buffer size - keep only last r.size bytes
	if pLen >= r.size {
		copy(r.data, p[pLen-r.size:])
		r.count = r.size
		r.start = 0
		r.end = 0
		return
	}

	// Calculate space at tail (from end to buffer boundary)
	tailSpace := r.size - r.end

	if pLen <= tailSpace {
		// Fits without wrapping
		copy(r.data[r.end:], p)
	} else {
		// Two-part copy: tail then head
		copy(r.data[r.end:], p[:tailSpace])
		copy(r.data[0:], p[tailSpace:])
	}

	// Update write pointer
	r.end = (r.end + pLen) % r.size

	// Update count and handle overflow
	r.count += pLen
	if r.count > r.size {
		overflow := r.count - r.size
		r.start = (r.start + overflow) % r.size
		r.count = r.size
	}
}

// Len returns the number of bytes currently in the buffer.
func (r *RingBuffer) Len() int {
	return r.count
}

// Reset clears the buffer.
func (r *RingBuffer) Reset() {
	r.count = 0
	r.start = 0
	r.end = 0
}

// LinearBytes returns buffer contents as a contiguous slice.
// The returned slice is only valid until the next Write call.
//
// Performance: Uses zero-copy fast path when data hasn't wrapped.
// When data wraps around, copies to pre-allocated linearBuf.
func (r *RingBuffer) LinearBytes() []byte {
	if r.count == 0 {
		return r.data[:0] // Empty buffer
	}

	// Fast path: data is contiguous (hasn't wrapped) AND buffer isn't full.
	// When buffer is exactly full, end == start due to wrap, so we must use
	// the slow path even though the indices suggest contiguous data.
	if r.end > r.start && r.count < r.size {
		return r.data[r.start:r.end]
	}

	// Slow path: data has wrapped around OR buffer is exactly full.
	// Copy both segments into pre-allocated linear buffer.
	n := r.count
	tailLen := r.size - r.start
	copy(r.linearBuf[:tailLen], r.data[r.start:])
	copy(r.linearBuf[tailLen:n], r.data[:r.end])
	return r.linearBuf[:n]
}
