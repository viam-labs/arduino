package arduino

import (
	"context"
	"fmt"
	"strings"
)

// pwmPins is the set of Arduino Uno Q pins that support PWM.
var pwmPins = map[string]bool{"3": true, "5": true, "6": true, "9": true, "10": true, "11": true}

// gpioPin implements board.GPIOPin for a single Arduino digital pin.
type gpioPin struct {
	pinNum string
	serial sender
}

func (p *gpioPin) send(ctx context.Context, cmd string) (string, error) {
	resp, err := p.serial.send(ctx, cmd)
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(resp, "ERR") {
		return "", fmt.Errorf("firmware error: %s", strings.TrimPrefix(resp, "ERR "))
	}
	return strings.TrimPrefix(resp, "OK "), nil
}

// Get returns the current high/low state of the pin.
func (p *gpioPin) Get(ctx context.Context, _ map[string]interface{}) (bool, error) {
	val, err := p.send(ctx, fmt.Sprintf("GET %s", p.pinNum))
	if err != nil {
		return false, err
	}
	return val == "1", nil
}

// Set drives the pin high or low.
func (p *gpioPin) Set(ctx context.Context, high bool, _ map[string]interface{}) error {
	bit := "0"
	if high {
		bit = "1"
	}
	_, err := p.send(ctx, fmt.Sprintf("SET %s %s", p.pinNum, bit))
	return err
}

// PWM returns the current duty cycle (0.0–1.0).
func (p *gpioPin) PWM(ctx context.Context, _ map[string]interface{}) (float64, error) {
	if !pwmPins[p.pinNum] {
		return 0, fmt.Errorf("pin %s does not support PWM", p.pinNum)
	}
	val, err := p.send(ctx, fmt.Sprintf("PWMGET %s", p.pinNum))
	if err != nil {
		return 0, err
	}
	var duty float64
	if _, err := fmt.Sscanf(val, "%f", &duty); err != nil {
		return 0, fmt.Errorf("parsing PWM response %q: %w", val, err)
	}
	return duty, nil
}

// SetPWM sets the duty cycle (0.0–1.0).
func (p *gpioPin) SetPWM(ctx context.Context, dutyCyclePct float64, _ map[string]interface{}) error {
	if !pwmPins[p.pinNum] {
		return fmt.Errorf("pin %s does not support PWM", p.pinNum)
	}
	_, err := p.send(ctx, fmt.Sprintf("PWM %s %.4f", p.pinNum, dutyCyclePct))
	return err
}

// PWMFreq returns the current PWM frequency in Hz.
func (p *gpioPin) PWMFreq(ctx context.Context, _ map[string]interface{}) (uint, error) {
	if !pwmPins[p.pinNum] {
		return 0, fmt.Errorf("pin %s does not support PWM", p.pinNum)
	}
	val, err := p.send(ctx, fmt.Sprintf("FREQGET %s", p.pinNum))
	if err != nil {
		return 0, err
	}
	var freq uint
	if _, err := fmt.Sscanf(val, "%d", &freq); err != nil {
		return 0, fmt.Errorf("parsing freq response %q: %w", val, err)
	}
	return freq, nil
}

// SetPWMFreq sets the PWM frequency in Hz.
func (p *gpioPin) SetPWMFreq(ctx context.Context, freqHz uint, _ map[string]interface{}) error {
	if !pwmPins[p.pinNum] {
		return fmt.Errorf("pin %s does not support PWM", p.pinNum)
	}
	_, err := p.send(ctx, fmt.Sprintf("FREQ %s %d", p.pinNum, freqHz))
	return err
}
