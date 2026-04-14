package arduino

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"syscall"
	"time"
)

// sender is the interface for sending commands to the STM32.
// The real implementation uses a UART serial port; tests use a mock.
type sender interface {
	send(ctx context.Context, cmd string) (string, error)
	close() error
}

// rawReadWriter abstracts the platform-specific read/write path.
type rawReadWriter interface {
	readChunk(buf []byte) (int, error)
	write(b []byte) (int, error)
	closeRW() error
}

// serialConn is the real UART implementation of sender.
// A single background goroutine (readLoop) owns the fd and streams bytes into
// recv; readLine selects on recv and ctx.Done() for deadline-safe line reads.
type serialConn struct {
	mu   sync.Mutex
	rw   rawReadWriter
	recv chan byte
	done chan struct{}
}

// openSerial opens the UART port at the given path and baud rate.
// Implemented in serial_linux.go for Linux; returns an error on other platforms.
func openSerial(path string, baud int) (*serialConn, error) {
	if baud == 0 {
		baud = 115200
	}
	return openSerialLinux(path, baud)
}

// newSerialConn wraps a rawReadWriter, starts the background reader goroutine,
// and returns a ready-to-use serialConn.
func newSerialConn(rw rawReadWriter) *serialConn {
	s := &serialConn{
		rw:   rw,
		recv: make(chan byte, 4096),
		done: make(chan struct{}),
	}
	go s.readLoop()
	return s
}

// readLoop is the single goroutine that reads from the fd for the lifetime of
// this serialConn. It exits when done is closed or the fd is closed (EBADF).
func (s *serialConn) readLoop() {
	buf := make([]byte, 256)
	for {
		select {
		case <-s.done:
			return
		default:
		}

		n, err := s.rw.readChunk(buf)
		if err != nil {
			switch {
			case errors.Is(err, syscall.EBADF):
				return // fd was closed — clean shutdown
			case errors.Is(err, io.EOF):
				return // fd closed or HUP
			case errors.Is(err, syscall.EAGAIN), errors.Is(err, syscall.EWOULDBLOCK):
				time.Sleep(5 * time.Millisecond)
				continue
			default:
				time.Sleep(10 * time.Millisecond)
				continue
			}
		}

		for i := 0; i < n; i++ {
			select {
			case s.recv <- buf[i]:
			case <-s.done:
				return
			}
		}
	}
}

// send writes cmd\n to the port and reads back one response line.
// The mutex ensures only one command is in-flight at a time.
func (s *serialConn) send(ctx context.Context, cmd string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := s.rw.write([]byte(cmd + "\n")); err != nil {
		return "", fmt.Errorf("write %q: %w", cmd, err)
	}
	return s.readLine(ctx)
}

// readLine assembles bytes from recv until '\n', returning the trimmed line.
// ctx cancellation is honoured immediately via select.
func (s *serialConn) readLine(ctx context.Context) (string, error) {
	var line []byte
	for {
		select {
		case <-ctx.Done():
			if len(line) > 0 {
				return "", fmt.Errorf("%w (partial %d bytes: %q)",
					ctx.Err(), len(line), string(line))
			}
			return "", fmt.Errorf("%w (0 bytes received)", ctx.Err())
		case b := <-s.recv:
			if b == '\n' {
				// Arduino Serial.println sends CR+LF; trimLine strips \r.
				return trimLine(line), nil
			}
			line = append(line, b)
		}
	}
}

// drain discards buffered bytes in recv after a successful HELLO, preventing
// the STM32 boot message from shifting subsequent command/response pairs.
func (s *serialConn) drain() {
	for {
		select {
		case <-s.recv:
		default:
			return
		}
	}
}

// close shuts down the reader goroutine and closes the underlying fd.
func (s *serialConn) close() error {
	select {
	case <-s.done:
	default:
		close(s.done)
	}
	return s.rw.closeRW()
}

// trimLine strips trailing carriage returns and whitespace from a line buffer.
func trimLine(b []byte) string {
	s := string(b)
	for len(s) > 0 && (s[len(s)-1] == '\r' || s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}
