package arduino

import (
	"testing"
)

func TestConfigValidate(t *testing.T) {
	t.Run("missing serial_path returns error", func(t *testing.T) {
		cfg := &Config{}
		_, _, err := cfg.Validate("test")
		if err == nil {
			t.Fatal("expected error for missing serial_path")
		}
	})

	t.Run("valid config returns no error", func(t *testing.T) {
		cfg := &Config{SerialPath: "/dev/ttyUSB0"}
		_, _, err := cfg.Validate("test")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("baud_rate defaults to 115200 after Validate", func(t *testing.T) {
		cfg := &Config{SerialPath: "/dev/ttyUSB0"}
		cfg.Validate("test") //nolint:errcheck
		if cfg.BaudRate != 115200 {
			t.Fatalf("expected 115200, got %d", cfg.BaudRate)
		}
	})

	t.Run("custom baud_rate is preserved after Validate", func(t *testing.T) {
		cfg := &Config{SerialPath: "/dev/ttyUSB0", BaudRate: 9600}
		cfg.Validate("test") //nolint:errcheck
		if cfg.BaudRate != 9600 {
			t.Fatalf("expected 9600 preserved, got %d", cfg.BaudRate)
		}
	})

	t.Run("analog readers config validates cleanly", func(t *testing.T) {
		cfg := &Config{
			SerialPath: "/dev/ttyUSB0",
			AnalogReaders: []AnalogConfig{
				{Name: "a0", Pin: "0"},
				{Name: "a1", Pin: "1"},
			},
		}
		_, _, err := cfg.Validate("test")
		if err != nil {
			t.Fatalf("unexpected error with analog readers: %v", err)
		}
	})
}
