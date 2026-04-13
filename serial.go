package arduino

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"time"

	goserial "go.bug.st/serial"
)

// sender is the interface for sending commands to the STM32.
// The real implementation uses a UART serial port; tests use a mock.
type sender interface {
	send(ctx context.Context, cmd string) (string, error)
	close() error
}

// serialConn is the real UART implementation of sender.
//
// bufio.Reader is intentionally NOT used here. go.bug.st/serial returns (0, nil)
// when a read timeout fires on macOS (serial_unix.go:93 uses select(); on timeout
// it returns zero bytes with no error). bufio.Reader.fill() treats (0, nil) as
// "keep trying" and loops up to 100 times before raising io.ErrNoProgress
// ("multiple Read calls return no data or error"). readLine avoids this by
// polling byte-by-byte and explicitly handling the (0, nil) case.
type serialConn struct {
	mu   sync.Mutex
	port goserial.Port
}

// openSerial opens the UART port at the given path and baud rate.
//
// On macOS, USB serial devices must be opened via the /dev/cu.* path, not
// /dev/tty.*. The tty.* variant uses carrier-detect (DCD) semantics: because
// Arduino boards never assert DCD, reads stall indefinitely. openSerial
// silently rewrites /dev/tty. → /dev/cu. on darwin so a user-supplied tty.*
// path works without requiring manual correction.
func openSerial(path string, baud int) (*serialConn, error) {
	if runtime.GOOS == "darwin" {
		path = strings.Replace(path, "/dev/tty.", "/dev/cu.", 1)
	}
	port, err := goserial.Open(path, &goserial.Mode{
		BaudRate: baud,
		DataBits: 8,
		Parity:   goserial.NoParity,
		StopBits: goserial.OneStopBit,
	})
	if err != nil {
		return nil, fmt.Errorf("opening %s at %d baud: %w", path, baud, err)
	}
	return &serialConn{port: port}, nil
}

// send writes cmd\n to the port and reads back one response line.
func (s *serialConn) send(ctx context.Context, cmd string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := fmt.Fprintf(s.port, "%s\n", cmd); err != nil {
		return "", fmt.Errorf("write %q: %w", cmd, err)
	}
	return s.readLine(ctx)
}

// serialPollInterval is the per-read timeout used inside readLine.
// Short enough to check ctx.Err() frequently; long enough not to spin.
const serialPollInterval = 50 * time.Millisecond

// readLine reads from the port one byte at a time until it finds '\n',
// then returns the trimmed line. It polls at serialPollInterval so that
// the caller's context deadline is respected even when no data arrives.
//
// go.bug.st/serial returns (0, nil) on a read timeout (no error, no bytes).
// We treat that as "no data yet" and loop, checking ctx.Err() each iteration.
// On timeout, the error includes how many bytes were received so far, which
// helps distinguish "no response at all" from "partial/truncated response".
func (s *serialConn) readLine(ctx context.Context) (string, error) {
	if err := s.port.SetReadTimeout(serialPollInterval); err != nil {
		return "", fmt.Errorf("set read timeout: %w", err)
	}
	buf := make([]byte, 1)
	var line []byte
	for {
		if err := ctx.Err(); err != nil {
			// Include any partial bytes so callers can tell "silence" from
			// "truncated response" in error messages and logs.
			if len(line) > 0 {
				return "", fmt.Errorf("%w (partial %d bytes: %q)", err, len(line), string(line))
			}
			return "", fmt.Errorf("%w (0 bytes received)", err)
		}
		n, err := s.port.Read(buf)
		if err != nil {
			return "", fmt.Errorf("read: %w", err)
		}
		if n == 0 {
			// (0, nil) — serial poll interval fired, no data yet; retry.
			continue
		}
		if buf[0] == '\n' {
			// Arduino's Serial.println sends CR+LF; TrimSpace handles the \r.
			return strings.TrimSpace(string(line)), nil
		}
		line = append(line, buf[0])
	}
}

func (s *serialConn) close() error {
	return s.port.Close()
}
