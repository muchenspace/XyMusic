package processio

import (
	"strings"
	"sync"
)

// HeadBuffer retains the beginning of a stream while continuing to accept
// writes after the configured limit. This lets child processes drain their
// output pipes without allowing unbounded memory growth.
type HeadBuffer struct {
	mu        sync.Mutex
	maximum   int
	buffer    strings.Builder
	truncated bool
}

func NewHeadBuffer(maximum int) *HeadBuffer {
	if maximum < 0 {
		maximum = 0
	}
	return &HeadBuffer{maximum: maximum}
}

func (buffer *HeadBuffer) Write(value []byte) (int, error) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()

	remaining := buffer.maximum - buffer.buffer.Len()
	if remaining > 0 {
		if remaining > len(value) {
			remaining = len(value)
		}
		_, _ = buffer.buffer.Write(value[:remaining])
	}
	if remaining < len(value) {
		buffer.truncated = true
	}
	return len(value), nil
}

func (buffer *HeadBuffer) String() string {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return buffer.buffer.String()
}

func (buffer *HeadBuffer) Truncated() bool {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return buffer.truncated
}

// TailBuffer retains the end of a stream while always reporting successful
// full writes, so verbose stderr cannot block a child process.
type TailBuffer struct {
	mu      sync.Mutex
	maximum int
	buffer  []byte
}

func NewTailBuffer(maximum int) *TailBuffer {
	if maximum < 0 {
		maximum = 0
	}
	return &TailBuffer{maximum: maximum, buffer: make([]byte, 0, maximum)}
}

func (buffer *TailBuffer) Write(value []byte) (int, error) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()

	if buffer.maximum == 0 {
		return len(value), nil
	}
	if len(value) >= buffer.maximum {
		buffer.buffer = append(buffer.buffer[:0], value[len(value)-buffer.maximum:]...)
		return len(value), nil
	}
	overflow := len(buffer.buffer) + len(value) - buffer.maximum
	if overflow > 0 {
		copy(buffer.buffer, buffer.buffer[overflow:])
		buffer.buffer = buffer.buffer[:len(buffer.buffer)-overflow]
	}
	buffer.buffer = append(buffer.buffer, value...)
	return len(value), nil
}

func (buffer *TailBuffer) String() string {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return string(buffer.buffer)
}
