package arduino

import (
	"context"
	"fmt"
	"sync"
	"time"

	pb "go.viam.com/api/component/board/v1"
	board "go.viam.com/rdk/components/board"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
)

var UnoQ = resource.NewModel("viam", "arduino", "uno-q")

func init() {
	resource.RegisterComponent(board.API, UnoQ,
		resource.Registration[board.Board, *Config]{
			Constructor: newArduinoUnoQ,
		},
	)
}

// AnalogConfig configures a single analog input channel.
type AnalogConfig struct {
	Name string `json:"name"`
	Pin  string `json:"pin"` // "0" through "5" for A0–A5
}

// InterruptConfig configures a single digital interrupt.
type InterruptConfig struct {
	Name string `json:"name"`
	Pin  string `json:"pin"`
	Mode string `json:"mode,omitempty"` // "RISING", "FALLING", or "CHANGE" (default)
}

// Config holds the configuration for the Arduino Uno Q board component.
type Config struct {
	SerialPath        string            `json:"serial_path"`
	BaudRate          int               `json:"baud_rate,omitempty"`
	AnalogReaders     []AnalogConfig    `json:"analogs,omitempty"`
	DigitalInterrupts []InterruptConfig `json:"digital_interrupts,omitempty"`
}

// Validate ensures all parts of the config are valid and important fields exist.
func (cfg *Config) Validate(path string) ([]string, []string, error) {
	if cfg.SerialPath == "" {
		return nil, nil, fmt.Errorf("%s: serial_path is required", path)
	}
	if cfg.BaudRate == 0 {
		cfg.BaudRate = 115200
	}
	for i := range cfg.DigitalInterrupts {
		if cfg.DigitalInterrupts[i].Mode == "" {
			cfg.DigitalInterrupts[i].Mode = "CHANGE"
		}
	}
	return nil, nil, nil
}

type arduinoUnoQ struct {
	name resource.Name

	mu         sync.Mutex
	serial     sender
	gpios      map[string]*gpioPin
	analogs    map[string]*analogPin
	interrupts map[string]*digitalInterrupt // keyed by logical name

	tickSubsMu sync.Mutex
	tickSubs   []chan board.Tick // active StreamTicks subscribers

	logger logging.Logger
	cfg    *Config

	cancelCtx  context.Context
	cancelFunc func()
}

// NewUnoQ is exported for use by the CLI and testing utilities.
func NewUnoQ(ctx context.Context, _ resource.Dependencies, name resource.Name, conf *Config, logger logging.Logger) (board.Board, error) {
	conn, err := openSerial(conf.SerialPath, conf.BaudRate)
	if err != nil {
		return nil, fmt.Errorf("opening serial port %s: %w", conf.SerialPath, err)
	}
	return newBoardWithSender(ctx, name, conf, conn, logger)
}

func newArduinoUnoQ(ctx context.Context, _ resource.Dependencies, rawConf resource.Config, logger logging.Logger) (board.Board, error) {
	conf, err := resource.NativeConfig[*Config](rawConf)
	if err != nil {
		return nil, err
	}
	conn, err := openSerial(conf.SerialPath, conf.BaudRate)
	if err != nil {
		return nil, fmt.Errorf("opening serial port %s: %w", conf.SerialPath, err)
	}
	return newBoardWithSender(ctx, rawConf.ResourceName(), conf, conn, logger)
}

func newBoardWithSender(ctx context.Context, name resource.Name, conf *Config, s sender, logger logging.Logger) (*arduinoUnoQ, error) {
	cancelCtx, cancelFunc := context.WithCancel(context.Background())
	b := &arduinoUnoQ{
		name:       name,
		serial:     s,
		gpios:      map[string]*gpioPin{},
		analogs:    map[string]*analogPin{},
		interrupts: map[string]*digitalInterrupt{},
		logger:     logger,
		cfg:        conf,
		cancelCtx:  cancelCtx,
		cancelFunc: cancelFunc,
	}
	if err := b.hello(ctx); err != nil {
		s.close()
		cancelFunc()
		return nil, err
	}
	for _, ar := range conf.AnalogReaders {
		b.analogs[ar.Name] = &analogPin{channel: ar.Pin, serial: b.serial}
	}
	if err := b.configureInterrupts(conf.DigitalInterrupts); err != nil {
		s.close()
		cancelFunc()
		return nil, err
	}
	go b.tickDispatcher(s)
	return b, nil
}

func (b *arduinoUnoQ) hello(ctx context.Context) error {
	const (
		overallTimeout = 30 * time.Second
		attemptTimeout = 5 * time.Second
		retryWait      = 500 * time.Millisecond
		want           = "OK UNO-Q v1"
	)
	ctx, cancel := context.WithTimeout(ctx, overallTimeout)
	defer cancel()

	for {
		// Per-attempt sub-context so a single slow response doesn't consume
		// the entire 10 s budget.
		attemptCtx, attemptCancel := context.WithTimeout(ctx, attemptTimeout)
		resp, err := b.serial.send(attemptCtx, "HELLO")
		attemptCancel()

		if err == nil {
			if resp != want {
				return fmt.Errorf("firmware version mismatch: got %q, want %q", resp, want)
			}
			// Drain any stale bytes (e.g. the STM32 boot message may have
			// arrived before our HELLO, leaving an extra response in the
			// buffer that would shift subsequent commands out of sync).
			if sc, ok := b.serial.(*serialConn); ok {
				sc.drain()
			}
			return nil
		}

		// Overall deadline exceeded — give up.
		if ctx.Err() != nil {
			return fmt.Errorf("HELLO handshake timed out after %v: %w", overallTimeout, err)
		}

		// Brief pause before the next attempt.
		select {
		case <-time.After(retryWait):
		case <-ctx.Done():
			return fmt.Errorf("HELLO handshake timed out after %v: %w", overallTimeout, err)
		}
	}
}

func (b *arduinoUnoQ) Reconfigure(ctx context.Context, _ resource.Dependencies, rawConf resource.Config) error {
	conf, err := resource.NativeConfig[*Config](rawConf)
	if err != nil {
		return err
	}
	conn, err := openSerial(conf.SerialPath, conf.BaudRate)
	if err != nil {
		return fmt.Errorf("reopening serial port %s: %w", conf.SerialPath, err)
	}
	return b.reconfigureWithSender(ctx, conf, conn)
}

// reconfigureWithSender is extracted so tests can inject a mock sender.
func (b *arduinoUnoQ) reconfigureWithSender(ctx context.Context, conf *Config, s sender) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Stop the existing tickDispatcher before draining/hello.
	b.cancelFunc()
	cancelCtx, cancelFunc := context.WithCancel(context.Background())
	b.cancelCtx = cancelCtx
	b.cancelFunc = cancelFunc

	if err := b.serial.close(); err != nil {
		b.logger.Warnw("closing serial port during reconfigure", "err", err)
	}
	b.serial = s
	b.gpios = map[string]*gpioPin{}
	b.analogs = map[string]*analogPin{}
	b.interrupts = map[string]*digitalInterrupt{}
	b.cfg = conf
	if err := b.hello(ctx); err != nil {
		return err
	}
	for _, ar := range conf.AnalogReaders {
		b.analogs[ar.Name] = &analogPin{channel: ar.Pin, serial: b.serial}
	}
	if err := b.configureInterrupts(conf.DigitalInterrupts); err != nil {
		return err
	}
	go b.tickDispatcher(s)
	return nil
}

func (s *arduinoUnoQ) Name() resource.Name {
	return s.name
}

// AnalogByName returns a named analog reader from the config.
func (b *arduinoUnoQ) AnalogByName(name string) (board.Analog, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	a, ok := b.analogs[name]
	if !ok {
		return nil, fmt.Errorf("analog reader %q not found", name)
	}
	return a, nil
}

// configureInterrupts sends an "INT <pin> <mode>" command for each configured
// interrupt and stores a digitalInterrupt keyed by logical name.
func (b *arduinoUnoQ) configureInterrupts(cfgs []InterruptConfig) error {
	for _, ic := range cfgs {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, err := b.serial.send(ctx, fmt.Sprintf("INT %s %s", ic.Pin, ic.Mode))
		cancel()
		if err != nil {
			return fmt.Errorf("configuring interrupt %q on pin %s: %w", ic.Name, ic.Pin, err)
		}
		b.interrupts[ic.Name] = &digitalInterrupt{name: ic.Name, pin: ic.Pin}
	}
	return nil
}

// tickDispatcher reads TICK lines from the serial connection and fans them
// out to all active StreamTicks subscribers.
// For non-serialConn senders (mock), it returns immediately.
// s is captured at launch time to avoid a data race with reconfigureWithSender.
func (b *arduinoUnoQ) tickDispatcher(s sender) {
	sc, ok := s.(*serialConn)
	if !ok {
		return // mock sender — no real TICK channel
	}
	for {
		select {
		case <-b.cancelCtx.Done():
			return
		case line := <-sc.tickRecv:
			b.dispatchTick(line)
		}
	}
}

// dispatchTick parses a "TICK <pin> <high> <micros>" line, increments the
// matching interrupt counter, and fans the event to all StreamTicks callers.
func (b *arduinoUnoQ) dispatchTick(line string) {
	var pin int
	var high int
	var micros uint64
	if _, err := fmt.Sscanf(line, "TICK %d %d %d", &pin, &high, &micros); err != nil {
		b.logger.Warnw("malformed TICK", "line", line, "err", err)
		return
	}
	pinStr := fmt.Sprintf("%d", pin)

	// Find interrupt by pin, increment counter.
	b.mu.Lock()
	var matched *digitalInterrupt
	for _, di := range b.interrupts {
		if di.pin == pinStr {
			di.recordTick()
			matched = di
			break
		}
	}
	b.mu.Unlock()

	if matched == nil {
		return
	}

	tick := board.Tick{
		Name:             matched.name,
		High:             high != 0,
		TimestampNanosec: micros * 1000,
	}

	b.tickSubsMu.Lock()
	subs := make([]chan board.Tick, len(b.tickSubs))
	copy(subs, b.tickSubs)
	b.tickSubsMu.Unlock()

	for _, sub := range subs {
		select {
		case sub <- tick:
		default: // never block a slow subscriber
		}
	}
}

// DigitalInterruptByName looks up a configured interrupt by logical name.
func (b *arduinoUnoQ) DigitalInterruptByName(name string) (board.DigitalInterrupt, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	di, ok := b.interrupts[name]
	if !ok {
		return nil, fmt.Errorf("digital interrupt %q not configured", name)
	}
	return di, nil
}

// GPIOPinByName returns (and lazily creates) a GPIO pin by its Arduino pin number.
func (b *arduinoUnoQ) GPIOPinByName(name string) (board.GPIOPin, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if p, ok := b.gpios[name]; ok {
		return p, nil
	}
	p := &gpioPin{pinNum: name, serial: b.serial}
	b.gpios[name] = p
	return p, nil
}

// SetPowerMode is not supported.
func (b *arduinoUnoQ) SetPowerMode(_ context.Context, _ pb.PowerMode, _ *time.Duration, _ map[string]interface{}) error {
	return fmt.Errorf("SetPowerMode not supported on Arduino Uno Q")
}

func (b *arduinoUnoQ) DoCommand(_ context.Context, _ map[string]interface{}) (map[string]interface{}, error) {
	return nil, fmt.Errorf("DoCommand not implemented")
}

func (b *arduinoUnoQ) Status(_ context.Context) (map[string]interface{}, error) {
	return map[string]interface{}{}, nil
}

// StreamTicks subscribes ch to all tick events, blocking until ctx is cancelled.
func (b *arduinoUnoQ) StreamTicks(ctx context.Context, _ []board.DigitalInterrupt, ch chan board.Tick, _ map[string]interface{}) error {
	b.tickSubsMu.Lock()
	b.tickSubs = append(b.tickSubs, ch)
	b.tickSubsMu.Unlock()

	<-ctx.Done()

	b.tickSubsMu.Lock()
	for i, sub := range b.tickSubs {
		if sub == ch {
			b.tickSubs = append(b.tickSubs[:i], b.tickSubs[i+1:]...)
			break
		}
	}
	b.tickSubsMu.Unlock()
	return nil
}

func (b *arduinoUnoQ) Close(_ context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.cancelFunc()
	return b.serial.close()
}
