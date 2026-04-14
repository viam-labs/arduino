//go:build linux

package arduino

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// linuxRawPort implements rawReadWriter using direct syscall.Read/Write,
// bypassing Go's internal epoll scheduler (os.File). The Qualcomm MSM UART
// reports itself as "always readable" in poll(), causing epoll to immediately
// unpark goroutines — then syscall.Read blocks forever. syscall.Read with
// O_NONBLOCK always returns EAGAIN immediately when no data is available,
// making the goroutine+select pattern in readLine work correctly.
type linuxRawPort struct {
	f  *os.File
	fd int
}

func (p *linuxRawPort) readChunk(buf []byte) (int, error) {
	n, err := syscall.Read(p.fd, buf)
	if n < 0 {
		n = 0
	}
	return n, err
}

func (p *linuxRawPort) write(b []byte) (int, error) {
	return syscall.Write(p.fd, b)
}

func (p *linuxRawPort) closeRW() error {
	return p.f.Close()
}

// openSerialLinux opens the UART with O_NONBLOCK kept permanently set.
func openSerialLinux(path string, baud int) (*serialConn, error) {
	// Pulse GPIO 37 on gpiochip1 to wake the STM32 coprocessor.
	// Mirrors arduino-router's ExecStartPre:
	//   ExecStartPre=-/usr/bin/gpioset -c /dev/gpiochip1 -t0 37=0
	_ = exec.Command("gpioset", "-c", "/dev/gpiochip1", "-t0", "37=0").Run()
	time.Sleep(500 * time.Millisecond)

	f, err := os.OpenFile(path, os.O_RDWR|syscall.O_NOCTTY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}

	fd := int(f.Fd())
	if err := configureTermios(fd, baud); err != nil {
		f.Close()
		return nil, fmt.Errorf("configuring termios on %s: %w", path, err)
	}

	return &serialConn{rw: &linuxRawPort{f: f, fd: fd}}, nil
}

// configureTermios reads the current termios settings and modifies them
// for 8N1 raw mode at the given baud rate, then applies them.
// Reading first (TCGETS before TCSETS) avoids corrupting driver state.
func configureTermios(fd int, baud int) error {
	baudConst, err := linuxBaudConst(baud)
	if err != nil {
		return err
	}

	// Start from current settings rather than a zeroed struct — some UART
	// drivers require certain fields to be preserved.
	t, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		return fmt.Errorf("TCGETS: %w", err)
	}

	// Raw input: disable canonical mode, echo, signals, extended processing.
	t.Iflag &^= unix.IGNBRK | unix.BRKINT | unix.PARMRK | unix.ISTRIP |
		unix.INLCR | unix.IGNCR | unix.ICRNL | unix.IXON
	t.Oflag &^= unix.OPOST
	t.Lflag &^= unix.ECHO | unix.ECHONL | unix.ICANON | unix.ISIG | unix.IEXTEN

	// 8 data bits, no parity, enable receiver, ignore modem control lines.
	t.Cflag &^= unix.CSIZE | unix.PARENB
	t.Cflag |= unix.CS8 | unix.CREAD | unix.CLOCAL

	// Set baud rate: clear CBAUD bits then set new value, also set Ispeed/Ospeed.
	t.Cflag &^= unix.CBAUD
	t.Cflag |= baudConst
	t.Ispeed = baudConst
	t.Ospeed = baudConst

	// VMIN=0, VTIME=0: with O_NONBLOCK, return whatever bytes are available.
	t.Cc[unix.VMIN] = 0
	t.Cc[unix.VTIME] = 0

	return unix.IoctlSetTermios(fd, unix.TCSETS, t)
}

func linuxBaudConst(baud int) (uint32, error) {
	m := map[int]uint32{
		9600:   syscall.B9600,
		19200:  syscall.B19200,
		38400:  syscall.B38400,
		57600:  syscall.B57600,
		115200: syscall.B115200,
		230400: syscall.B230400,
	}
	c, ok := m[baud]
	if !ok {
		return 0, fmt.Errorf("unsupported baud rate %d", baud)
	}
	return c, nil
}
