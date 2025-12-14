package connector

import (
	"io"
)

// channelReader adapts a stdinResult channel to io.Reader.
type channelReader struct {
	ch  <-chan stdinResult
	buf []byte
}

func (r *channelReader) Read(p []byte) (n int, err error) {
	if len(r.buf) > 0 {
		n = copy(p, r.buf)
		r.buf = r.buf[n:]
		return n, nil
	}

	result, ok := <-r.ch
	if !ok {
		return 0, io.EOF
	}
	if result.err != nil {
		return 0, result.err
	}

	n = copy(p, result.data)
	if n < len(result.data) {
		r.buf = result.data[n:]
	}
	return n, nil
}
