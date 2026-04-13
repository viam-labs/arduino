package arduino

import (
	"context"
	"math"
	"testing"
)

func TestAnalogPin_Read(t *testing.T) {
	b, mock := newTestBoard(t, mockResponse{"OK 2048", nil})
	pin := &analogPin{channel: "0", serial: b.serial}

	val, err := pin.Read(context.Background(), nil)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if val.Value != 2048 {
		t.Fatalf("expected Value=2048, got %d", val.Value)
	}
	if val.Min != 0 {
		t.Fatalf("expected Min=0, got %f", val.Min)
	}
	// float32 comparison: use tolerance instead of == to avoid precision issues
	if math.Abs(float64(val.Max)-3.3) > 1e-4 {
		t.Fatalf("expected Max≈3.3, got %f", val.Max)
	}
	if mock.sent[1] != "ADC 0" {
		t.Fatalf("expected ADC 0, got %q", mock.sent[1])
	}
}

func TestAnalogPin_Read_Max(t *testing.T) {
	b, _ := newTestBoard(t, mockResponse{"OK 4095", nil})
	pin := &analogPin{channel: "3", serial: b.serial}

	val, err := pin.Read(context.Background(), nil)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if val.Value != 4095 {
		t.Fatalf("expected Value=4095, got %d", val.Value)
	}
}

func TestAnalogPin_Write_NotSupported(t *testing.T) {
	b, _ := newTestBoard(t)
	pin := &analogPin{channel: "0", serial: b.serial}
	if err := pin.Write(context.Background(), 0, nil); err == nil {
		t.Fatal("expected error for Write on analog input")
	}
}

func TestAnalogPin_Read_Zero(t *testing.T) {
	b, _ := newTestBoard(t, mockResponse{"OK 0", nil})
	pin := &analogPin{channel: "0", serial: b.serial}

	val, err := pin.Read(context.Background(), nil)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if val.Value != 0 {
		t.Fatalf("expected Value=0, got %d", val.Value)
	}
}

func TestAnalogPin_Read_FirmwareErr(t *testing.T) {
	b, _ := newTestBoard(t, mockResponse{"ERR invalid ADC channel", nil})
	pin := &analogPin{channel: "9", serial: b.serial}

	_, err := pin.Read(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error from firmware ERR response")
	}
}

func TestAnalogPin_Read_ParseErr(t *testing.T) {
	b, _ := newTestBoard(t, mockResponse{"OK notanumber", nil})
	pin := &analogPin{channel: "0", serial: b.serial}

	_, err := pin.Read(context.Background(), nil)
	if err == nil {
		t.Fatal("expected parse error for non-integer ADC response")
	}
}

func TestAnalogPin_Read_StepSize(t *testing.T) {
	b, _ := newTestBoard(t, mockResponse{"OK 1000", nil})
	pin := &analogPin{channel: "0", serial: b.serial}

	val, err := pin.Read(context.Background(), nil)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	// adcStepSize = 3.3 / 4095 ≈ 0.000806
	want := float32(3.3 / 4095.0)
	if math.Abs(float64(val.StepSize)-float64(want)) > 1e-6 {
		t.Fatalf("expected StepSize≈%f, got %f", want, val.StepSize)
	}
}
