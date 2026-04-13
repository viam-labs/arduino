package arduino

import (
	"context"
	"fmt"
	"strings"

	"go.viam.com/rdk/components/board"
)

const (
	adcMax      = 4095.0
	adcVoltage  = 3.3
	adcStepSize = adcVoltage / adcMax
)

// analogPin implements board.Analog for one of A0–A5.
type analogPin struct {
	channel string // "0" through "5"
	serial  sender
}

// Read sends an ADC command and converts the raw 12-bit result to AnalogValue.
func (a *analogPin) Read(ctx context.Context, _ map[string]interface{}) (board.AnalogValue, error) {
	resp, err := a.serial.send(ctx, fmt.Sprintf("ADC %s", a.channel))
	if err != nil {
		return board.AnalogValue{}, err
	}
	if strings.HasPrefix(resp, "ERR") {
		return board.AnalogValue{}, fmt.Errorf("firmware error: %s", strings.TrimPrefix(resp, "ERR "))
	}
	raw := strings.TrimPrefix(resp, "OK ")
	var value int
	if _, err := fmt.Sscanf(raw, "%d", &value); err != nil {
		return board.AnalogValue{}, fmt.Errorf("parsing ADC response %q: %w", resp, err)
	}
	return board.AnalogValue{
		Value:    value,
		Min:      0,
		Max:      adcVoltage,
		StepSize: adcStepSize,
	}, nil
}

// Write is not supported — A0–A5 are input-only analog pins.
func (a *analogPin) Write(_ context.Context, _ int, _ map[string]interface{}) error {
	return fmt.Errorf("analog write not supported on Arduino Uno Q (pins A0-A5 are input only)")
}
