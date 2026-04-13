//go:build hardware

// Hardware integration tests for the Arduino Uno Q board component.
//
// Prerequisites:
//
//  1. Arduino Uno Q connected via USB at the path given by -serial flag (default /dev/ttyACM0)
//  2. Firmware flashed: firmware/uno-q-firmware.ino compiled for the Arduino Uno Q target
//  3. No other process has the serial port open (close Arduino IDE's Serial Monitor)
//  4. For ADC tests: nothing needs to be connected to A0–A5 (floating pins read noise, that's fine)
//  5. For GPIO tests: pin 13 drives the onboard LED — visual confirmation is available
//
// Run with:
//
//	go test -tags hardware -v -serial /dev/ttyACM0 ./...
//
// Skip gracefully when hardware is absent (t.Skipf) so CI always passes.
package arduino

import (
	"context"
	"flag"
	"sync"
	"testing"

	board "go.viam.com/rdk/components/board"
	"go.viam.com/rdk/logging"
)

var hwSerialPath = flag.String("serial", "/dev/ttyACM0", "serial port for Arduino Uno Q hardware tests")

// requireHardware opens a real board connection. If the port is unavailable the
// test is skipped (not failed), so CI passes without hardware attached.
func requireHardware(t *testing.T) *arduinoUnoQ {
	t.Helper()
	cfg := &Config{SerialPath: *hwSerialPath, BaudRate: 115200}
	b, err := NewUnoQ(context.Background(), nil, board.Named("hw-test"), cfg, logging.NewTestLogger(t))
	if err != nil {
		t.Skipf("hardware not available at %s (%v) — skipping", *hwSerialPath, err)
	}
	t.Cleanup(func() { b.Close(context.Background()) }) //nolint:errcheck
	return b.(*arduinoUnoQ)
}

// requireHardwareWithAnalogs opens a board with all six analog channels configured.
func requireHardwareWithAnalogs(t *testing.T) *arduinoUnoQ {
	t.Helper()
	cfg := &Config{
		SerialPath: *hwSerialPath,
		BaudRate:   115200,
		AnalogReaders: []AnalogConfig{
			{Name: "a0", Pin: "0"},
			{Name: "a1", Pin: "1"},
			{Name: "a2", Pin: "2"},
			{Name: "a3", Pin: "3"},
			{Name: "a4", Pin: "4"},
			{Name: "a5", Pin: "5"},
		},
	}
	b, err := NewUnoQ(context.Background(), nil, board.Named("hw-test-analog"), cfg, logging.NewTestLogger(t))
	if err != nil {
		t.Skipf("hardware not available at %s (%v) — skipping", *hwSerialPath, err)
	}
	t.Cleanup(func() { b.Close(context.Background()) }) //nolint:errcheck
	return b.(*arduinoUnoQ)
}

// --- Handshake ---

func TestHW_HelloHandshake(t *testing.T) {
	// requireHardware itself performs the HELLO handshake; success means the test passed.
	_ = requireHardware(t)
}

// --- GPIO ---

func TestHW_GPIO_SetHigh_Pin13(t *testing.T) {
	b := requireHardware(t)
	ctx := context.Background()

	pin, err := b.GPIOPinByName("13")
	if err != nil {
		t.Fatalf("GPIOPinByName: %v", err)
	}
	if err := pin.Set(ctx, true, nil); err != nil {
		t.Fatalf("Set high: %v", err)
	}
	high, err := pin.Get(ctx, nil)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !high {
		t.Fatal("expected pin 13 to read high after Set(true)")
	}
}

func TestHW_GPIO_SetLow_Pin13(t *testing.T) {
	b := requireHardware(t)
	ctx := context.Background()

	pin, err := b.GPIOPinByName("13")
	if err != nil {
		t.Fatalf("GPIOPinByName: %v", err)
	}
	if err := pin.Set(ctx, false, nil); err != nil {
		t.Fatalf("Set low: %v", err)
	}
	high, err := pin.Get(ctx, nil)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if high {
		t.Fatal("expected pin 13 to read low after Set(false)")
	}
}

func TestHW_GPIO_Pin2_Input(t *testing.T) {
	b := requireHardware(t)
	pin, err := b.GPIOPinByName("2")
	if err != nil {
		t.Fatalf("GPIOPinByName: %v", err)
	}
	// Just verify read doesn't error; value depends on what's connected.
	_, err = pin.Get(context.Background(), nil)
	if err != nil {
		t.Fatalf("Get pin 2: %v", err)
	}
}

// --- PWM ---

func TestHW_PWM_SetDuty_Pin9(t *testing.T) {
	b := requireHardware(t)
	pin, err := b.GPIOPinByName("9")
	if err != nil {
		t.Fatalf("GPIOPinByName: %v", err)
	}
	if err := pin.SetPWM(context.Background(), 0.5, nil); err != nil {
		t.Fatalf("SetPWM 0.5 on pin 9: %v", err)
	}
}

func TestHW_PWM_SetDuty_Zero_Pin9(t *testing.T) {
	b := requireHardware(t)
	pin, err := b.GPIOPinByName("9")
	if err != nil {
		t.Fatalf("GPIOPinByName: %v", err)
	}
	if err := pin.SetPWM(context.Background(), 0.0, nil); err != nil {
		t.Fatalf("SetPWM 0.0 on pin 9: %v", err)
	}
}

func TestHW_PWM_NonPWMPin(t *testing.T) {
	b := requireHardware(t)
	pin, err := b.GPIOPinByName("13") // 13 is not in pwmPins
	if err != nil {
		t.Fatalf("GPIOPinByName: %v", err)
	}
	if err := pin.SetPWM(context.Background(), 0.5, nil); err == nil {
		t.Fatal("expected error when calling SetPWM on non-PWM pin 13")
	}
}

func TestHW_PWMFreq_Set_Pin9(t *testing.T) {
	b := requireHardware(t)
	pin, err := b.GPIOPinByName("9")
	if err != nil {
		t.Fatalf("GPIOPinByName: %v", err)
	}
	if err := pin.SetPWMFreq(context.Background(), 1000, nil); err != nil {
		t.Fatalf("SetPWMFreq 1000 Hz on pin 9: %v", err)
	}
}

// --- ADC ---

func TestHW_ADC_Read_Ch0(t *testing.T) {
	b := requireHardwareWithAnalogs(t)

	a, err := b.AnalogByName("a0")
	if err != nil {
		t.Fatalf("AnalogByName(a0): %v", err)
	}
	val, err := a.Read(context.Background(), nil)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if val.Value < 0 || val.Value > 4095 {
		t.Fatalf("ADC value out of 12-bit range: %d", val.Value)
	}
}

func TestHW_ADC_AllChannels(t *testing.T) {
	b := requireHardwareWithAnalogs(t)
	ctx := context.Background()

	channels := []string{"a0", "a1", "a2", "a3", "a4", "a5"}
	for _, name := range channels {
		a, err := b.AnalogByName(name)
		if err != nil {
			t.Fatalf("AnalogByName(%s): %v", name, err)
		}
		val, err := a.Read(ctx, nil)
		if err != nil {
			t.Fatalf("Read(%s): %v", name, err)
		}
		if val.Value < 0 || val.Value > 4095 {
			t.Fatalf("channel %s: ADC value out of range: %d", name, val.Value)
		}
		t.Logf("channel %s: raw=%d (%.3fV)", name, val.Value, float64(val.Value)*float64(val.StepSize))
	}
}

// --- Lifecycle ---

func TestHW_Reconfigure(t *testing.T) {
	b := requireHardware(t)
	ctx := context.Background()

	conn, err := openSerial(*hwSerialPath, 115200)
	if err != nil {
		t.Skipf("cannot open second serial connection for reconfigure test: %v", err)
	}

	conf := &Config{SerialPath: *hwSerialPath, BaudRate: 115200}
	if err := b.reconfigureWithSender(ctx, conf, conn); err != nil {
		t.Fatalf("reconfigureWithSender: %v", err)
	}
	// If we get here the HELLO handshake succeeded on the new connection.
}

func TestHW_Close_Idempotent(t *testing.T) {
	b := requireHardware(t)
	ctx := context.Background()

	if err := b.Close(ctx); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	// Second close should not panic (may return an error from already-closed port, that's OK).
	_ = b.Close(ctx)
}

// --- Concurrency ---

func TestHW_Concurrent_GPIO(t *testing.T) {
	b := requireHardware(t)
	ctx := context.Background()

	pins := []string{"2", "3", "4", "5", "6", "7", "8"}
	const workers = 10

	var wg sync.WaitGroup
	errs := make(chan error, workers*len(pins))

	for i := 0; i < workers; i++ {
		for _, pinName := range pins {
			wg.Add(1)
			pn := pinName
			go func() {
				defer wg.Done()
				pin, err := b.GPIOPinByName(pn)
				if err != nil {
					errs <- err
					return
				}
				if err := pin.Set(ctx, true, nil); err != nil {
					errs <- err
					return
				}
				if _, err := pin.Get(ctx, nil); err != nil {
					errs <- err
				}
			}()
		}
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent GPIO error: %v", err)
	}
}
