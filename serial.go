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
// On Linux this is backed by an O_NONBLOCK file descriptor via syscall.Read.
type rawReadWriter interface {
	readChunk(buf []byte) (int, error)
	write(b []byte) (int, error)
	closeRW() error
}

// serialConn is the real UART implementation of sender.
type serialConn struct {
	mu sync.Mutex
	rw rawReadWriter
}

// openSerial opens the UART port at the given path and baud rate.
// Implemented in serial_linux.go for Linux; returns an error on other platforms.
func openSerial(path string, baud int) (*serialConn, error) {
	if baud == 0 {
		baud = 115200
	}
	return openSerialLinux(path, baud)
}

// send writes cmd\n to the port and reads back one response line.
func (s *serialConn) send(ctx context.Context, cmd string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := s.rw.write([]byte(cmd + "\n")); err != nil {
		return "", fmt.Errorf("write %q: %w", cmd, err)
	}
	return s.readLine(ctx)
}

// readLine reads from the port until '\n', returning the trimmed line.
// The read runs in a goroutine so ctx cancellation is always honoured even
// when the underlying syscall.Read blocks in a kernel call.
//
// The goroutine exits when it finds '\n', receives a real error, or when the
// port is closed (which causes the next read to return an error).
func (s *serialConn) readLine(ctx context.Context) (string, error) {
	type result struct {
		line string
		err  error
	}
	ch := make(chan result, 1)

	go func() {
		buf := make([]byte, 256)
		var line []byte
		for {
			// Exit early if the context is already done so we don't spin
			// after the outer select has returned via ctx.Done().
			if ctx.Err() != nil {
				return
			}
			n, err := s.rw.readChunk(buf)
			if err != nil {
				switch {
				case errors.Is(err, syscall.EAGAIN), errors.Is(err, syscall.EWOULDBLOCK):
					// O_NONBLOCK: no data yet — yield briefly and retry.
					time.Sleep(5 * time.Millisecond)
					continue
				case errors.Is(err, io.EOF):
					// MSM UART driver returned 0 bytes without EAGAIN; Go
					// converts read()=0 to io.EOF. Transient during STM32
					// wake-up — treat as "no data yet" and retry.
					time.Sleep(5 * time.Millisecond)
					continue
				default:
					ch <- result{"", err}
					return
				}
			}
			for i := 0; i < n; i++ {
				if buf[i] == '\n' {
					// Arduino Serial.println sends CR+LF; TrimSpace strips \r.
					ch <- result{trimLine(line), nil}
					return
				}
				line = append(line, buf[i])
			}
		}
	}()

	select {
	case r := <-ch:
		return r.line, r.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (s *serialConn) close() error {
	return s.rw.closeRW()
}

// trimLine strips trailing whitespace and carriage returns from a line buffer.
func trimLine(b []byte) string {
	s := string(b)
	for len(s) > 0 && (s[len(s)-1] == '\r' || s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}
