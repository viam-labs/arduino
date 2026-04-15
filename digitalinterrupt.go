package arduino

import (
	"context"
	"sync/atomic"
)

// digitalInterrupt implements board.DigitalInterrupt for a single STM32 pin.
// Value returns the running count of ticks since the interrupt was configured.
type digitalInterrupt struct {
	name  string
	count atomic.Int64
	pin   string // Arduino pin number string, e.g. "2"
}

func (d *digitalInterrupt) Name() string { return d.name }

func (d *digitalInterrupt) Value(_ context.Context, _ map[string]interface{}) (int64, error) {
	return d.count.Load(), nil
}

// recordTick increments the counter; called by the tick dispatcher.
func (d *digitalInterrupt) recordTick() {
	d.count.Add(1)
}
