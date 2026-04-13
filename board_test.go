package arduino

import (
	"context"
	"fmt"
	"testing"
	"time"

	board "go.viam.com/rdk/components/board"
	"go.viam.com/rdk/logging"
)

func testBoard(t *testing.T, mock *mockSender) *arduinoUnoQ {
	t.Helper()
	conf := &Config{SerialPath: "/dev/ttyUSB0", BaudRate: 115200}
	b, err := newBoardWithSender(context.Background(), board.Named("test-board"), conf, mock, logging.NewTestLogger(t))
	if err != nil {
		t.Fatalf("newBoardWithSender: %v", err)
	}
	return b
}

func TestNewBoard_HelloHandshake(t *testing.T) {
	t.Run("correct firmware version succeeds", func(t *testing.T) {
		mock := &mockSender{}
		mock.queue("OK UNO-Q v1", nil)
		b := testBoard(t, mock)
		if b == nil {
			t.Fatal("expected board")
		}
		if len(mock.sent) != 1 || mock.sent[0] != "HELLO" {
			t.Fatalf("expected HELLO sent, got %v", mock.sent)
		}
	})

	t.Run("wrong firmware version returns error", func(t *testing.T) {
		mock := &mockSender{}
		mock.queue("OK UNO-Q v0", nil)
		conf := &Config{SerialPath: "/dev/ttyUSB0", BaudRate: 115200}
		_, err := newBoardWithSender(context.Background(), board.Named("test-board"), conf, mock, logging.NewTestLogger(t))
		if err == nil {
			t.Fatal("expected error for wrong firmware version")
		}
	})
}

func TestNewBoard_HelloSendErr(t *testing.T) {
	mock := &mockSender{}
	// queue an error on the HELLO send
	mock.queue("", fmt.Errorf("serial port disconnected"))
	conf := &Config{SerialPath: "/dev/ttyUSB0", BaudRate: 115200}
	// Short context so hello()'s retry loop exits quickly rather than running
	// for the full 10 s overall timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err := newBoardWithSender(ctx, board.Named("test-board"), conf, mock, logging.NewTestLogger(t))
	if err == nil {
		t.Fatal("expected error when HELLO send fails")
	}
	// close() must have been called to clean up after failure
	if !mock.closed {
		t.Fatal("expected mock.close() to be called after HELLO failure")
	}
}

func TestBoard_Close(t *testing.T) {
	mock := &mockSender{}
	mock.queue("OK UNO-Q v1", nil)
	b := testBoard(t, mock)

	if err := b.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !mock.closed {
		t.Fatal("expected mock.close() to be called by Board.Close")
	}
}

func TestBoard_AnalogFromConfig(t *testing.T) {
	mock := &mockSender{}
	mock.queue("OK UNO-Q v1", nil)
	conf := &Config{
		SerialPath: "/dev/ttyUSB0",
		BaudRate:   115200,
		AnalogReaders: []AnalogConfig{
			{Name: "a0", Pin: "0"},
			{Name: "a1", Pin: "1"},
		},
	}
	b, err := newBoardWithSender(context.Background(), board.Named("test-board"), conf, mock, logging.NewTestLogger(t))
	if err != nil {
		t.Fatalf("newBoardWithSender: %v", err)
	}

	a, err := b.AnalogByName("a0")
	if err != nil {
		t.Fatalf("AnalogByName(a0): %v", err)
	}
	if a == nil {
		t.Fatal("expected non-nil analog for a0")
	}

	_, err = b.AnalogByName("a1")
	if err != nil {
		t.Fatalf("AnalogByName(a1): %v", err)
	}

	_, err = b.AnalogByName("a2")
	if err == nil {
		t.Fatal("expected error for unconfigured analog a2")
	}
}
