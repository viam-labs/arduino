package arduino

import (
	"context"
	"strings"
	"testing"
	"time"
)

// fakePipe lets tests inject bytes into serialConn.
type fakePipe struct {
	data chan []byte
}

func (f *fakePipe) readChunk(buf []byte) (int, error) {
	select {
	case b := <-f.data:
		n := copy(buf, b)
		return n, nil
	case <-time.After(200 * time.Millisecond):
		return 0, nil // VTIME-style timeout
	}
}
func (f *fakePipe) write(b []byte) (int, error) { return len(b), nil }
func (f *fakePipe) closeRW() error              { return nil }

func TestReadLine_RoutesCommandResponse(t *testing.T) {
	pipe := &fakePipe{data: make(chan []byte, 8)}
	conn := newSerialConn(pipe)
	defer conn.close()

	pipe.data <- []byte("OK 1\r\n")

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	line, err := conn.readLine(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if line != "OK 1" {
		t.Fatalf("want %q got %q", "OK 1", line)
	}
}

func TestReadLoop_RoutesTick(t *testing.T) {
	pipe := &fakePipe{data: make(chan []byte, 8)}
	conn := newSerialConn(pipe)
	defer conn.close()

	pipe.data <- []byte("TICK 2 1 123456\r\n")

	select {
	case line := <-conn.tickRecv:
		if !strings.HasPrefix(line, "TICK ") {
			t.Fatalf("expected TICK prefix, got %q", line)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for TICK")
	}
}

func TestDrain_ClearsTickRecv(t *testing.T) {
	pipe := &fakePipe{data: make(chan []byte, 8)}
	conn := newSerialConn(pipe)
	defer conn.close()

	pipe.data <- []byte("TICK 2 1 99999\r\n")
	time.Sleep(50 * time.Millisecond) // let readLoop process
	conn.drain()

	select {
	case <-conn.tickRecv:
		t.Fatal("tickRecv should be empty after drain")
	default:
	}
}

func TestDrain_ClearsCommandRecv(t *testing.T) {
	pipe := &fakePipe{data: make(chan []byte, 8)}
	conn := newSerialConn(pipe)
	defer conn.close()

	pipe.data <- []byte("OK UNO-Q v1\r\n")
	time.Sleep(50 * time.Millisecond) // let readLoop process
	conn.drain()

	select {
	case <-conn.cmdRecv:
		t.Fatal("cmdRecv should be empty after drain")
	default:
	}
}
