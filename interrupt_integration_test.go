package arduino

import (
	"context"
	"testing"
	"time"

	board "go.viam.com/rdk/components/board"
	"go.viam.com/rdk/logging"
)

func TestDigitalInterruptByName_Found(t *testing.T) {
	mock := &mockSender{}
	mock.queue("OK UNO-Q v1", nil) // HELLO
	mock.queue("OK", nil)           // INT 2 CHANGE

	conf := &Config{
		SerialPath: "/dev/ttyHS1",
		DigitalInterrupts: []InterruptConfig{
			{Name: "enc-a", Pin: "2", Mode: "CHANGE"},
		},
	}
	b, err := newBoardWithSender(context.Background(), board.Named("test"), conf, mock, logging.NewTestLogger(t))
	if err != nil {
		t.Fatal(err)
	}

	di, err := b.DigitalInterruptByName("enc-a")
	if err != nil {
		t.Fatalf("DigitalInterruptByName: %v", err)
	}
	if di.Name() != "enc-a" {
		t.Fatalf("wrong name: %s", di.Name())
	}
}

func TestDigitalInterruptByName_NotFound(t *testing.T) {
	mock := &mockSender{}
	mock.queue("OK UNO-Q v1", nil)
	conf := &Config{SerialPath: "/dev/ttyHS1"}
	b, err := newBoardWithSender(context.Background(), board.Named("test"), conf, mock, logging.NewTestLogger(t))
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close(context.Background())
	_, err = b.DigitalInterruptByName("missing")
	if err == nil {
		t.Fatal("expected error for unconfigured interrupt")
	}
}

func TestStreamTicks_ReceivesTick(t *testing.T) {
	mock := &mockSender{}
	mock.queue("OK UNO-Q v1", nil)
	mock.queue("OK", nil) // INT 2 CHANGE

	conf := &Config{
		SerialPath: "/dev/ttyHS1",
		DigitalInterrupts: []InterruptConfig{
			{Name: "btn", Pin: "2", Mode: "RISING"},
		},
	}
	b, err := newBoardWithSender(context.Background(), board.Named("test"), conf, mock, logging.NewTestLogger(t))
	if err != nil {
		t.Fatal(err)
	}

	di, _ := b.DigitalInterruptByName("btn")
	ch := make(chan board.Tick, 4)

	ctx, cancel := context.WithCancel(context.Background())
	go b.StreamTicks(ctx, []board.DigitalInterrupt{di}, ch, nil) //nolint:errcheck
	time.Sleep(20 * time.Millisecond)

	// Simulate an incoming TICK from the STM32
	b.dispatchTick("TICK 2 1 9000")

	cancel()

	select {
	case tick := <-ch:
		if tick.Name != "btn" || !tick.High {
			t.Fatalf("unexpected tick: %+v", tick)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for tick")
	}
}
