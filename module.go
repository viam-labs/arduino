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

// Config holds the configuration for the Arduino Uno Q board component.
type Config struct {
	SerialPath    string         `json:"serial_path"`
	BaudRate      int            `json:"baud_rate,omitempty"`
	AnalogReaders []AnalogConfig `json:"analogs,omitempty"`
}

// Validate ensures all parts of the config are valid and important fields exist.
func (cfg *Config) Validate(path string) ([]string, []string, error) {
	if cfg.SerialPath == "" {
		return nil, nil, fmt.Errorf("%s: serial_path is required", path)
	}
	if cfg.BaudRate == 0 {
		cfg.BaudRate = 115200
	}
	return nil, nil, nil
}

type arduinoUnoQ struct {
	name resource.Name

	mu      sync.Mutex
	serial  sender
	gpios   map[string]*gpioPin
	analogs map[string]*analogPin

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
	if err := b.serial.close(); err != nil {
		b.logger.Warnw("closing serial port during reconfigure", "err", err)
	}
	b.serial = s
	b.gpios = map[string]*gpioPin{}
	b.analogs = map[string]*analogPin{}
	b.cfg = conf
	if err := b.hello(ctx); err != nil {
		return err
	}
	for _, ar := range conf.AnalogReaders {
		b.analogs[ar.Name] = &analogPin{channel: ar.Pin, serial: b.serial}
	}
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

// DigitalInterruptByName is not supported in v1.
func (b *arduinoUnoQ) DigitalInterruptByName(name string) (board.DigitalInterrupt, error) {
	return nil, fmt.Errorf("digital interrupts not supported on Arduino Uno Q")
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

// StreamTicks is not supported in v1.
func (b *arduinoUnoQ) StreamTicks(_ context.Context, _ []board.DigitalInterrupt, _ chan board.Tick, _ map[string]interface{}) error {
	return fmt.Errorf("StreamTicks not supported on Arduino Uno Q")
}

func (b *arduinoUnoQ) Close(_ context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.cancelFunc()
	return b.serial.close()
}
