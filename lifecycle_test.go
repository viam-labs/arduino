package arduino

import (
	"context"
	"testing"
)

func TestBoard_Reconfigure(t *testing.T) {
	b, oldMock := newTestBoard(t)

	// Build a new mock to inject via reconfigureWithSender.
	newMock := &mockSender{}
	newMock.queue("OK UNO-Q v1", nil) // HELLO for reconfigure

	conf := &Config{SerialPath: "/dev/ttyUSB0", BaudRate: 115200}
	if err := b.reconfigureWithSender(context.Background(), conf, newMock); err != nil {
		t.Fatalf("reconfigureWithSender: %v", err)
	}

	// After reconfigure, the first command sent to newMock must be HELLO.
	if len(newMock.sent) == 0 || newMock.sent[0] != "HELLO" {
		t.Fatalf("expected HELLO after reconfigure, got %v", newMock.sent)
	}

	// The old sender must have been closed during reconfigure.
	if !oldMock.closed {
		t.Fatal("expected old sender to be closed during reconfigure")
	}
}

func TestBoard_Reconfigure_AnalogReaders(t *testing.T) {
	b, _ := newTestBoard(t)

	newMock := &mockSender{}
	newMock.queue("OK UNO-Q v1", nil) // HELLO

	conf := &Config{
		SerialPath: "/dev/ttyUSB0",
		BaudRate:   115200,
		AnalogReaders: []AnalogConfig{
			{Name: "vbat", Pin: "2"},
		},
	}
	if err := b.reconfigureWithSender(context.Background(), conf, newMock); err != nil {
		t.Fatalf("reconfigureWithSender: %v", err)
	}

	// Analog reader from new config must be present.
	a, err := b.AnalogByName("vbat")
	if err != nil {
		t.Fatalf("AnalogByName after reconfigure: %v", err)
	}
	if a == nil {
		t.Fatal("expected non-nil analog after reconfigure")
	}
}

func TestBoard_Reconfigure_HelloFailure(t *testing.T) {
	b, _ := newTestBoard(t)

	badMock := &mockSender{}
	badMock.queue("OK UNO-Q v0", nil) // wrong version → hello() returns error

	conf := &Config{SerialPath: "/dev/ttyUSB0", BaudRate: 115200}
	if err := b.reconfigureWithSender(context.Background(), conf, badMock); err == nil {
		t.Fatal("expected error when HELLO returns wrong firmware version during reconfigure")
	}
}
