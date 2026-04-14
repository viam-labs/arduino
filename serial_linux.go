//go:build linux

package arduino

import (
	"fmt"
	"syscall"

	"golang.org/x/sys/unix"
)

// linuxRawPort implements rawReadWriter via direct syscall.Read/Write on a
// raw fd. os.File is avoided because os.File.Fd() calls SetBlocking(), which
// removes O_NONBLOCK — causing reads to block forever on the MSM UART.
type linuxRawPort struct {
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
	n, err := syscall.Write(p.fd, b)
	if n < 0 {
		n = 0
	}
	return n, err
}

func (p *linuxRawPort) closeRW() error {
	return syscall.Close(p.fd)
}

// openSerialLinux opens the UART with VTIME-based read timeouts.
// O_NONBLOCK is cleared after open because it suppresses VTIME on Linux tty
// devices and the MSM UART driver does not return EAGAIN anyway.
// GPIO 37 is not pulsed here; setup.sh handles the one-time STM32 wake signal.
func openSerialLinux(path string, baud int) (*serialConn, error) {
	fd, err := syscall.Open(path, syscall.O_RDWR|syscall.O_NOCTTY|syscall.O_NDELAY, 0)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}

	if err := clearNonblock(fd); err != nil {
		syscall.Close(fd)
		return nil, fmt.Errorf("clearing O_NONBLOCK on %s: %w", path, err)
	}

	if err := configureTermios(fd, baud); err != nil {
		syscall.Close(fd)
		return nil, fmt.Errorf("configuring termios on %s: %w", path, err)
	}

	return newSerialConn(&linuxRawPort{fd: fd}), nil
}

// clearNonblock clears O_NONBLOCK on fd so VTIME takes effect for reads.
func clearNonblock(fd int) error {
	flags, _, errno := syscall.Syscall(syscall.SYS_FCNTL, uintptr(fd), syscall.F_GETFL, 0)
	if errno != 0 {
		return errno
	}
	_, _, errno = syscall.Syscall(syscall.SYS_FCNTL, uintptr(fd), syscall.F_SETFL,
		flags&^uintptr(syscall.O_NONBLOCK))
	if errno != 0 {
		return errno
	}
	return nil
}

// configureTermios sets 8N1 raw mode at the given baud rate.
// VMIN=0, VTIME=2 causes read() to return after 200 ms if no bytes arrive,
// allowing the readLoop goroutine to check context cancellation periodically.
func configureTermios(fd int, baud int) error {
	baudConst, err := linuxBaudConst(baud)
	if err != nil {
		return err
	}

	t, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		return fmt.Errorf("TCGETS: %w", err)
	}

	t.Iflag &^= unix.IGNBRK | unix.BRKINT | unix.PARMRK | unix.ISTRIP |
		unix.INLCR | unix.IGNCR | unix.ICRNL | unix.IXON
	t.Oflag &^= unix.OPOST
	t.Lflag &^= unix.ECHO | unix.ECHONL | unix.ICANON | unix.ISIG | unix.IEXTEN

	t.Cflag &^= unix.CSIZE | unix.PARENB
	t.Cflag |= unix.CS8 | unix.CREAD | unix.CLOCAL

	t.Cflag &^= unix.CBAUD
	t.Cflag |= baudConst
	t.Ispeed = baudConst
	t.Ospeed = baudConst

	t.Cc[unix.VMIN] = 0
	t.Cc[unix.VTIME] = 2

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
