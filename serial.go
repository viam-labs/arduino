package arduino

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
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
// A single background goroutine (readLoop) owns the fd and assembles complete
// lines, routing TICK-prefixed lines to tickRecv and all other lines to cmdRecv.
type serialConn struct {
	mu       sync.Mutex
	rw       rawReadWriter
	cmdRecv  chan string // OK / ERR lines — responses to commands
	tickRecv chan string // TICK lines    — unsolicited interrupt events
	done     chan struct{}
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
		rw:       rw,
		cmdRecv:  make(chan string, 64),
		tickRecv: make(chan string, 256),
		done:     make(chan struct{}),
	}
	go s.readLoop()
	return s
}

// readLoop is the single goroutine that reads from the fd for the lifetime of
// this serialConn. It assembles complete lines, strips \r inline, and routes
// TICK-prefixed lines to tickRecv and everything else to cmdRecv.
func (s *serialConn) readLoop() {
	buf := make([]byte, 256)
	var line []byte
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
			b := buf[i]
			if b == '\r' {
				continue
			}
			if b == '\n' {
				s.routeLine(string(line))
				line = line[:0]
				continue
			}
			line = append(line, b)
		}
	}
}

// routeLine sends a complete line to the appropriate channel.
func (s *serialConn) routeLine(line string) {
	if line == "" {
		return
	}
	var ch chan string
	if strings.HasPrefix(line, "TICK ") {
		ch = s.tickRecv
	} else {
		ch = s.cmdRecv
	}
	select {
	case ch <- line:
	case <-s.done:
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

// readLine reads the next command response line from cmdRecv.
// ctx cancellation is honoured immediately via select.
func (s *serialConn) readLine(ctx context.Context) (string, error) {
	select {
	case <-ctx.Done():
		return "", fmt.Errorf("%w (0 bytes received)", ctx.Err())
	case line := <-s.cmdRecv:
		return line, nil
	}
}

// drain discards buffered lines in both cmdRecv and tickRecv, preventing
// stale data from shifting subsequent command/response pairs.
func (s *serialConn) drain() {
	for {
		select {
		case <-s.cmdRecv:
		case <-s.tickRecv:
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
