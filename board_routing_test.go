package arduino

import (
	"sync"
	"testing"

	"go.viam.com/rdk/components/board"
)

func TestGPIOPinByName(t *testing.T) {
	b, _ := newTestBoard(t)

	pin, err := b.GPIOPinByName("13")
	if err != nil {
		t.Fatalf("GPIOPinByName: %v", err)
	}
	if pin == nil {
		t.Fatal("expected non-nil pin")
	}

	// same name returns same object (cached)
	pin2, _ := b.GPIOPinByName("13")
	if pin != pin2 {
		t.Fatal("expected same pin object for same name")
	}
}

func TestAnalogByName(t *testing.T) {
	b, _ := newTestBoard(t)
	// inject directly (normally populated from config)
	b.analogs["adc0"] = &analogPin{channel: "0", serial: b.serial}

	a, err := b.AnalogByName("adc0")
	if err != nil {
		t.Fatalf("AnalogByName: %v", err)
	}
	if a == nil {
		t.Fatal("expected non-nil analog")
	}
}

func TestAnalogByName_NotFound(t *testing.T) {
	b, _ := newTestBoard(t)
	_, err := b.AnalogByName("missing")
	if err == nil {
		t.Fatal("expected error for missing analog")
	}
}

func TestGPIOPinByName_Concurrent(t *testing.T) {
	b, _ := newTestBoard(t)

	const goroutines = 20
	pins := make([]board.GPIOPin, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		idx := i
		go func() {
			defer wg.Done()
			p, err := b.GPIOPinByName("7")
			if err != nil {
				t.Errorf("GPIOPinByName: %v", err)
				return
			}
			pins[idx] = p
		}()
	}
	wg.Wait()

	// All goroutines must have received the identical cached pin object.
	for i := 1; i < goroutines; i++ {
		if pins[i] != pins[0] {
			t.Fatalf("goroutine %d got different pin object (expected same cached pointer)", i)
		}
	}
}
