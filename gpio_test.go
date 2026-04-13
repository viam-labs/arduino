package arduino

import (
	"context"
	"testing"

	board "go.viam.com/rdk/components/board"
	"go.viam.com/rdk/logging"
)

func newTestBoard(t *testing.T, responses ...mockResponse) (*arduinoUnoQ, *mockSender) {
	t.Helper()
	mock := &mockSender{}
	mock.queue("OK UNO-Q v1", nil) // HELLO
	for _, r := range responses {
		mock.responses = append(mock.responses, r)
	}
	conf := &Config{SerialPath: "/dev/ttyUSB0", BaudRate: 115200}
	b, err := newBoardWithSender(context.Background(), board.Named("test-board"), conf, mock, logging.NewTestLogger(t))
	if err != nil {
		t.Fatalf("newBoardWithSender: %v", err)
	}
	return b, mock
}

func TestGPIOPin_Get(t *testing.T) {
	b, mock := newTestBoard(t, mockResponse{"OK 1", nil})
	pin := &gpioPin{pinNum: "13", serial: b.serial}

	high, err := pin.Get(context.Background(), nil)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !high {
		t.Fatal("expected high=true")
	}
	if mock.sent[1] != "GET 13" {
		t.Fatalf("expected GET 13, got %q", mock.sent[1])
	}
}

func TestGPIOPin_Set(t *testing.T) {
	b, mock := newTestBoard(t, mockResponse{"OK", nil})
	pin := &gpioPin{pinNum: "13", serial: b.serial}

	if err := pin.Set(context.Background(), true, nil); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if mock.sent[1] != "SET 13 1" {
		t.Fatalf("expected SET 13 1, got %q", mock.sent[1])
	}
}

func TestGPIOPin_Set_Low(t *testing.T) {
	b, mock := newTestBoard(t, mockResponse{"OK", nil})
	pin := &gpioPin{pinNum: "13", serial: b.serial}

	if err := pin.Set(context.Background(), false, nil); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if mock.sent[1] != "SET 13 0" {
		t.Fatalf("expected SET 13 0, got %q", mock.sent[1])
	}
}

func TestGPIOPin_ErrorResponse(t *testing.T) {
	b, _ := newTestBoard(t, mockResponse{"ERR unknown pin", nil})
	pin := &gpioPin{pinNum: "99", serial: b.serial}

	_, err := pin.Get(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for ERR response")
	}
}

func TestGPIOPin_SetPWM(t *testing.T) {
	b, mock := newTestBoard(t, mockResponse{"OK", nil})
	pin := &gpioPin{pinNum: "9", serial: b.serial}

	if err := pin.SetPWM(context.Background(), 0.5, nil); err != nil {
		t.Fatalf("SetPWM: %v", err)
	}
	if mock.sent[1] != "PWM 9 0.5000" {
		t.Fatalf("expected PWM 9 0.5000, got %q", mock.sent[1])
	}
}

func TestGPIOPin_SetPWM_Zero(t *testing.T) {
	b, mock := newTestBoard(t, mockResponse{"OK", nil})
	pin := &gpioPin{pinNum: "9", serial: b.serial}

	if err := pin.SetPWM(context.Background(), 0.0, nil); err != nil {
		t.Fatalf("SetPWM: %v", err)
	}
	if mock.sent[1] != "PWM 9 0.0000" {
		t.Fatalf("expected PWM 9 0.0000, got %q", mock.sent[1])
	}
}

func TestGPIOPin_SetPWM_Full(t *testing.T) {
	b, mock := newTestBoard(t, mockResponse{"OK", nil})
	pin := &gpioPin{pinNum: "9", serial: b.serial}

	if err := pin.SetPWM(context.Background(), 1.0, nil); err != nil {
		t.Fatalf("SetPWM: %v", err)
	}
	if mock.sent[1] != "PWM 9 1.0000" {
		t.Fatalf("expected PWM 9 1.0000, got %q", mock.sent[1])
	}
}

func TestGPIOPin_PWM(t *testing.T) {
	b, mock := newTestBoard(t, mockResponse{"OK 0.75", nil})
	pin := &gpioPin{pinNum: "9", serial: b.serial}

	duty, err := pin.PWM(context.Background(), nil)
	if err != nil {
		t.Fatalf("PWM: %v", err)
	}
	if duty != 0.75 {
		t.Fatalf("expected duty=0.75, got %f", duty)
	}
	if mock.sent[1] != "PWMGET 9" {
		t.Fatalf("expected PWMGET 9, got %q", mock.sent[1])
	}
}

func TestGPIOPin_SetPWMFreq(t *testing.T) {
	b, mock := newTestBoard(t, mockResponse{"OK", nil})
	pin := &gpioPin{pinNum: "9", serial: b.serial}

	if err := pin.SetPWMFreq(context.Background(), 1000, nil); err != nil {
		t.Fatalf("SetPWMFreq: %v", err)
	}
	if mock.sent[1] != "FREQ 9 1000" {
		t.Fatalf("expected FREQ 9 1000, got %q", mock.sent[1])
	}
}

func TestGPIOPin_PWMFreq(t *testing.T) {
	b, mock := newTestBoard(t, mockResponse{"OK 5000", nil})
	pin := &gpioPin{pinNum: "9", serial: b.serial}

	freq, err := pin.PWMFreq(context.Background(), nil)
	if err != nil {
		t.Fatalf("PWMFreq: %v", err)
	}
	if freq != 5000 {
		t.Fatalf("expected freq=5000, got %d", freq)
	}
	if mock.sent[1] != "FREQGET 9" {
		t.Fatalf("expected FREQGET 9, got %q", mock.sent[1])
	}
}

func TestGPIOPin_PWM_NonPWMPin(t *testing.T) {
	b, mock := newTestBoard(t)
	pin := &gpioPin{pinNum: "13", serial: b.serial} // 13 not in pwmPins

	if err := pin.SetPWM(context.Background(), 0.5, nil); err == nil {
		t.Fatal("expected error for non-PWM pin")
	}
	// Must NOT have sent any command after HELLO
	if len(mock.sent) != 1 {
		t.Fatalf("expected no command sent for non-PWM pin, sent=%v", mock.sent)
	}
}

func TestGPIOPin_SetPWM_FirmwareErr(t *testing.T) {
	b, _ := newTestBoard(t, mockResponse{"ERR PWMGET not supported on STM32", nil})
	pin := &gpioPin{pinNum: "9", serial: b.serial}

	// PWM (read) — firmware returns ERR, gpioPin.send propagates it
	_, err := pin.PWM(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error from firmware ERR response")
	}
}

func TestGPIOPin_PWMFreq_ParseErr(t *testing.T) {
	b, _ := newTestBoard(t, mockResponse{"OK notanumber", nil})
	pin := &gpioPin{pinNum: "9", serial: b.serial}

	_, err := pin.PWMFreq(context.Background(), nil)
	if err == nil {
		t.Fatal("expected parse error for non-integer freq response")
	}
}
