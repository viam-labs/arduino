# Digital Interrupts Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement `DigitalInterruptByName` and `StreamTicks` so Viam can detect pin-change events on the Arduino Uno Q's STM32 coprocessor.

**Architecture:** The STM32 firmware handles hardware interrupts and pushes unsolicited `TICK <pin> <high> <micros>` lines over Serial1. Because the serial link is shared with synchronous command/response traffic, `serialConn`'s readLoop is upgraded from a byte-forwarder into a line-level router that separates `TICK` lines (pushed to `tickRecv`) from all other lines (pushed to `cmdRecv`). A background goroutine on `arduinoUnoQ` fans `tickRecv` out to all active `StreamTicks` subscribers.

**Tech Stack:** Go 1.23, `go.viam.com/rdk v0.64.0` board interfaces, Arduino C++ / Zephyr RTOS on STM32U585, ASCII line protocol over UART.

---

## Reference files (read before starting)

- `serial.go` — `serialConn`, `readLoop`, `recv chan byte` (changes in Task 1)
- `module.go` — `arduinoUnoQ`, `newBoardWithSender`, `Config` (changes in Tasks 3–4)
- `mock_sender_test.go` — `mockSender` (no changes needed)
- `firmware/uno-q-firmware/uno-q-firmware.ino` — STM32 firmware (changes in Task 5)
- `~/viam/rdk/components/board/board.go` — `DigitalInterrupt`, `Tick`, `StreamTicks` interfaces

---

## Task 1: Upgrade serialConn to a line-level router

`recv chan byte` is replaced with two string channels so `readLine` and `StreamTicks` never race for the same bytes.

**Files:**
- Modify: `serial.go` (full rewrite of channel fields + readLoop + readLine + drain)

### Step 1: Write the failing tests

`serial_router_test.go` (new file, `package arduino`):

```go
package arduino

import (
    "context"
    "strings"
    "testing"
    "time"
)

// fakePipe lets tests inject bytes into serialConn.
type fakePipe struct {
    data chan []byte
}

func (f *fakePipe) readChunk(buf []byte) (int, error) {
    select {
    case b := <-f.data:
        n := copy(buf, b)
        return n, nil
    case <-time.After(200 * time.Millisecond):
        return 0, nil // VTIME-style timeout
    }
}
func (f *fakePipe) write(b []byte) (int, error) { return len(b), nil }
func (f *fakePipe) closeRW() error              { return nil }

func TestReadLine_RoutesCommandResponse(t *testing.T) {
    pipe := &fakePipe{data: make(chan []byte, 8)}
    conn := newSerialConn(pipe)
    defer conn.close()

    pipe.data <- []byte("OK 1\r\n")

    ctx, cancel := context.WithTimeout(context.Background(), time.Second)
    defer cancel()
    conn.mu.Lock()
    line, err := conn.readLine(ctx)
    conn.mu.Unlock()
    if err != nil {
        t.Fatal(err)
    }
    if line != "OK 1" {
        t.Fatalf("want %q got %q", "OK 1", line)
    }
}

func TestReadLoop_RoutesTick(t *testing.T) {
    pipe := &fakePipe{data: make(chan []byte, 8)}
    conn := newSerialConn(pipe)
    defer conn.close()

    pipe.data <- []byte("TICK 2 1 123456\r\n")

    select {
    case line := <-conn.tickRecv:
        if !strings.HasPrefix(line, "TICK ") {
            t.Fatalf("expected TICK prefix, got %q", line)
        }
    case <-time.After(time.Second):
        t.Fatal("timeout waiting for TICK")
    }
}

func TestDrain_ClearsCommandRecv(t *testing.T) {
    pipe := &fakePipe{data: make(chan []byte, 8)}
    conn := newSerialConn(pipe)
    defer conn.close()

    pipe.data <- []byte("OK UNO-Q v1\r\n")
    time.Sleep(50 * time.Millisecond) // let readLoop process
    conn.drain()

    select {
    case <-conn.cmdRecv:
        t.Fatal("cmdRecv should be empty after drain")
    default:
    }
}
```

### Step 2: Run tests to verify they fail

```bash
cd ~/viam/arduino
go test ./... -run TestReadLine_RoutesCommandResponse -v
```

Expected: FAIL — `conn.tickRecv` undefined, `conn.cmdRecv` undefined

### Step 3: Rewrite serial.go channels + readLoop + readLine + drain

Replace `recv chan byte` with `cmdRecv chan string` and `tickRecv chan string`. The readLoop now assembles complete lines before routing:

```go
type serialConn struct {
    mu       sync.Mutex
    rw       rawReadWriter
    cmdRecv  chan string // OK / ERR lines — responses to commands
    tickRecv chan string // TICK lines    — unsolicited interrupt events
    done     chan struct{}
}

func newSerialConn(rw rawReadWriter) *serialConn {
    s := &serialConn{
        rw:       rw,
        cmdRecv:  make(chan string, 64),
        tickRecv: make(chan string, 256),
        done:     make(chan struct{}),
    }
    go s.readLoop()
    return s
}

func (s *serialConn) readLoop() {
    buf := make([]byte, 256)
    var line []byte
    for {
        select {
        case <-s.done:
            return
        default:
        }
        n, err := s.rw.readChunk(buf)
        if err != nil {
            switch {
            case errors.Is(err, syscall.EBADF), errors.Is(err, io.EOF):
                return
            case errors.Is(err, syscall.EAGAIN), errors.Is(err, syscall.EWOULDBLOCK):
                time.Sleep(5 * time.Millisecond)
                continue
            default:
                time.Sleep(10 * time.Millisecond)
                continue
            }
        }
        for i := 0; i < n; i++ {
            b := buf[i]
            if b == '\r' {
                continue
            }
            if b == '\n' {
                s.routeLine(string(line))
                line = line[:0]
                continue
            }
            line = append(line, b)
        }
    }
}

func (s *serialConn) routeLine(line string) {
    if line == "" {
        return
    }
    var ch chan string
    if strings.HasPrefix(line, "TICK ") {
        ch = s.tickRecv
    } else {
        ch = s.cmdRecv
    }
    select {
    case ch <- line:
    case <-s.done:
    }
}

func (s *serialConn) readLine(ctx context.Context) (string, error) {
    select {
    case <-ctx.Done():
        return "", fmt.Errorf("%w (0 bytes received)", ctx.Err())
    case line := <-s.cmdRecv:
        return line, nil
    }
}

func (s *serialConn) drain() {
    for {
        select {
        case <-s.cmdRecv:
        case <-s.tickRecv:
        default:
            return
        }
    }
}
```

Add `"strings"` to imports.

Remove `trimLine` — it's no longer needed (readLoop strips `\r` inline).

### Step 4: Run tests

```bash
go test ./... -run "TestReadLine|TestReadLoop|TestDrain" -v
```

Expected: all PASS

### Step 5: Run full test suite

```bash
go test -race ./...
```

Expected: all existing tests PASS (mockSender is unaffected)

### Step 6: Commit

```bash
git add serial.go serial_router_test.go
git commit -m "feat: upgrade serialConn to line-level router with cmdRecv/tickRecv"
```

---

## Task 2: Add DigitalInterruptConfig to Config

**Files:**
- Modify: `module.go` (Config struct + Validate)
- Modify: `config_test.go`

### Step 1: Write failing test

In `config_test.go`, add:

```go
func TestConfigValidate_DigitalInterrupts(t *testing.T) {
    cfg := &Config{
        SerialPath: "/dev/ttyHS1",
        DigitalInterrupts: []InterruptConfig{
            {Name: "enc-a", Pin: "2", Mode: "CHANGE"},
        },
    }
    _, _, err := cfg.Validate("test")
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
}

func TestConfigValidate_InterruptModeDefault(t *testing.T) {
    cfg := &Config{
        SerialPath: "/dev/ttyHS1",
        DigitalInterrupts: []InterruptConfig{
            {Name: "btn", Pin: "3"}, // no Mode
        },
    }
    cfg.Validate("test") //nolint:errcheck
    if cfg.DigitalInterrupts[0].Mode != "CHANGE" {
        t.Fatalf("expected default mode CHANGE, got %q", cfg.DigitalInterrupts[0].Mode)
    }
}
```

### Step 2: Run to verify fail

```bash
go test ./... -run TestConfigValidate_DigitalInterrupts -v
```

Expected: FAIL — `InterruptConfig` undefined

### Step 3: Add InterruptConfig + update Config and Validate

In `module.go`:

```go
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
```

In `Validate`, after the BaudRate default:

```go
for i := range cfg.DigitalInterrupts {
    if cfg.DigitalInterrupts[i].Mode == "" {
        cfg.DigitalInterrupts[i].Mode = "CHANGE"
    }
}
```

### Step 4: Run tests

```bash
go test -race ./...
```

Expected: all PASS

### Step 5: Commit

```bash
git add module.go config_test.go
git commit -m "feat: add InterruptConfig to Config with CHANGE default mode"
```

---

## Task 3: Implement digitalInterrupt type

**Files:**
- Create: `digitalinterrupt.go`
- Create: `digitalinterrupt_test.go`

### Step 1: Write failing tests

`digitalinterrupt_test.go`:

```go
package arduino

import (
    "context"
    "testing"
)

func TestDigitalInterrupt_Name(t *testing.T) {
    di := &digitalInterrupt{name: "enc-a"}
    if di.Name() != "enc-a" {
        t.Fatalf("want enc-a got %s", di.Name())
    }
}

func TestDigitalInterrupt_Value_StartsZero(t *testing.T) {
    di := &digitalInterrupt{name: "btn"}
    v, err := di.Value(context.Background(), nil)
    if err != nil {
        t.Fatal(err)
    }
    if v != 0 {
        t.Fatalf("want 0 got %d", v)
    }
}

func TestDigitalInterrupt_Value_CountsTicks(t *testing.T) {
    di := &digitalInterrupt{name: "enc-a"}
    di.recordTick()
    di.recordTick()
    v, _ := di.Value(context.Background(), nil)
    if v != 2 {
        t.Fatalf("want 2 got %d", v)
    }
}
```

### Step 2: Run to verify fail

```bash
go test ./... -run TestDigitalInterrupt -v
```

Expected: FAIL — `digitalInterrupt` undefined

### Step 3: Implement digitalinterrupt.go

```go
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
```

### Step 4: Run tests

```bash
go test -race ./... -run TestDigitalInterrupt -v
```

Expected: all PASS

### Step 5: Commit

```bash
git add digitalinterrupt.go digitalinterrupt_test.go
git commit -m "feat: add digitalInterrupt type with atomic tick counter"
```

---

## Task 4: Wire interrupts into arduinoUnoQ

**Files:**
- Modify: `module.go` — struct, newBoardWithSender, DigitalInterruptByName, StreamTicks, Close, reconfigureWithSender

### Step 1: Write failing tests

`interrupt_integration_test.go` (new file, `package arduino`):

```go
package arduino

import (
    "context"
    "testing"
    "time"

    board "go.viam.com/rdk/components/board"
    "go.viam.com/rdk/logging"
)

func TestDigitalInterruptByName_Found(t *testing.T) {
    mock := &mockSender{}
    mock.queue("OK UNO-Q v1", nil) // HELLO
    mock.queue("OK", nil)           // INT 2 CHANGE

    conf := &Config{
        SerialPath: "/dev/ttyHS1",
        DigitalInterrupts: []InterruptConfig{
            {Name: "enc-a", Pin: "2", Mode: "CHANGE"},
        },
    }
    b, err := newBoardWithSender(context.Background(), board.Named("test"), conf, mock, logging.NewTestLogger(t))
    if err != nil {
        t.Fatal(err)
    }

    di, err := b.DigitalInterruptByName("enc-a")
    if err != nil {
        t.Fatalf("DigitalInterruptByName: %v", err)
    }
    if di.Name() != "enc-a" {
        t.Fatalf("wrong name: %s", di.Name())
    }
}

func TestDigitalInterruptByName_NotFound(t *testing.T) {
    mock := &mockSender{}
    mock.queue("OK UNO-Q v1", nil)
    conf := &Config{SerialPath: "/dev/ttyHS1"}
    b, _ := newBoardWithSender(context.Background(), board.Named("test"), conf, mock, logging.NewTestLogger(t))
    _, err := b.DigitalInterruptByName("missing")
    if err == nil {
        t.Fatal("expected error for unconfigured interrupt")
    }
}

func TestStreamTicks_ReceivesTick(t *testing.T) {
    mock := &mockSender{}
    mock.queue("OK UNO-Q v1", nil)
    mock.queue("OK", nil) // INT 2 CHANGE

    conf := &Config{
        SerialPath: "/dev/ttyHS1",
        DigitalInterrupts: []InterruptConfig{
            {Name: "btn", Pin: "2", Mode: "RISING"},
        },
    }
    b, err := newBoardWithSender(context.Background(), board.Named("test"), conf, mock, logging.NewTestLogger(t))
    if err != nil {
        t.Fatal(err)
    }

    di, _ := b.DigitalInterruptByName("btn")
    ch := make(chan board.Tick, 4)

    ctx, cancel := context.WithCancel(context.Background())
    go b.StreamTicks(ctx, []board.DigitalInterrupt{di}, ch, nil) //nolint:errcheck
    time.Sleep(20 * time.Millisecond)

    // Simulate an incoming TICK from the STM32
    b.dispatchTick("TICK 2 1 9000")

    cancel()

    select {
    case tick := <-ch:
        if tick.Name != "btn" || !tick.High {
            t.Fatalf("unexpected tick: %+v", tick)
        }
    case <-time.After(time.Second):
        t.Fatal("timeout waiting for tick")
    }
}
```

### Step 2: Run to verify fail

```bash
go test ./... -run "TestDigitalInterrupt|TestStreamTicks" -v
```

Expected: FAIL — `dispatchTick` undefined, `b.DigitalInterruptByName` still errors

### Step 3: Update arduinoUnoQ struct

```go
type arduinoUnoQ struct {
    name resource.Name

    mu         sync.Mutex
    serial     sender
    gpios      map[string]*gpioPin
    analogs    map[string]*analogPin
    interrupts map[string]*digitalInterrupt // keyed by logical name

    tickSubsMu sync.Mutex
    tickSubs   []chan board.Tick // active StreamTicks subscribers

    logger     logging.Logger
    cfg        *Config
    cancelCtx  context.Context
    cancelFunc func()
}
```

### Step 4: Update newBoardWithSender

```go
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
    go b.tickDispatcher()
    return b, nil
}
```

### Step 5: Add configureInterrupts, tickDispatcher, dispatchTick

```go
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
func (b *arduinoUnoQ) tickDispatcher() {
    sc, ok := b.serial.(*serialConn)
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
```

### Step 6: Implement DigitalInterruptByName and StreamTicks

```go
func (b *arduinoUnoQ) DigitalInterruptByName(name string) (board.DigitalInterrupt, error) {
    b.mu.Lock()
    defer b.mu.Unlock()
    di, ok := b.interrupts[name]
    if !ok {
        return nil, fmt.Errorf("digital interrupt %q not configured", name)
    }
    return di, nil
}

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
```

Also update `reconfigureWithSender` to reset and reconfigure interrupts.

### Step 7: Run tests

```bash
go test -race ./...
```

Expected: all PASS

### Step 8: Commit

```bash
git add module.go digitalinterrupt.go interrupt_integration_test.go
git commit -m "feat: implement DigitalInterruptByName and StreamTicks with tick fan-out"
```

---

## Task 5: Update STM32 firmware

**Files:**
- Modify: `firmware/uno-q-firmware/uno-q-firmware.ino`

### Step 1: Add interrupt slot table and ISRs before setup()

```cpp
// ---- Digital interrupt support ----
#define MAX_INT_SLOTS 8

struct IntSlot {
    int      pin;
    bool     active;
    volatile bool    triggered;
    volatile bool    lastHigh;
};

static IntSlot intSlots[MAX_INT_SLOTS];

// One ISR per slot — ISRs cannot take parameters in Arduino.
#define MAKE_ISR(N) \
static void isr##N() { \
    if (intSlots[N].active) { \
        intSlots[N].triggered = true; \
        intSlots[N].lastHigh = digitalRead(intSlots[N].pin); \
    } \
}
MAKE_ISR(0) MAKE_ISR(1) MAKE_ISR(2) MAKE_ISR(3)
MAKE_ISR(4) MAKE_ISR(5) MAKE_ISR(6) MAKE_ISR(7)

static void (*isrTable[MAX_INT_SLOTS])() =
    {isr0, isr1, isr2, isr3, isr4, isr5, isr6, isr7};
```

### Step 2: Add INT handler in handleCommand()

```cpp
} else if (op == "INT") {
    int sp = rest.indexOf(' ');
    int pin = rest.substring(0, sp).toInt();
    String mode = rest.substring(sp + 1);
    mode.trim();

    // Detach any existing slot for this pin.
    for (int i = 0; i < MAX_INT_SLOTS; i++) {
        if (intSlots[i].active && intSlots[i].pin == pin) {
            detachInterrupt(digitalPinToInterrupt(pin));
            intSlots[i].active = false;
            break;
        }
    }

    if (mode == "NONE") {
        Serial1.println("OK");
        return;
    }

    // Find a free slot.
    int slot = -1;
    for (int i = 0; i < MAX_INT_SLOTS; i++) {
        if (!intSlots[i].active) { slot = i; break; }
    }
    if (slot < 0) {
        Serial1.println("ERR no interrupt slots");
        return;
    }

    int imode = CHANGE;
    if (mode == "RISING")  imode = RISING;
    if (mode == "FALLING") imode = FALLING;

    intSlots[slot] = { pin, true, false, (bool)digitalRead(pin) };
    attachInterrupt(digitalPinToInterrupt(pin), isrTable[slot], imode);
    Serial1.println("OK");
```

### Step 3: Poll triggered slots in loop() and emit TICK

```cpp
// At the top of loop(), before Serial1.available() check:
for (int i = 0; i < MAX_INT_SLOTS; i++) {
    if (intSlots[i].active && intSlots[i].triggered) {
        intSlots[i].triggered = false;
        Serial1.println(
            "TICK " + String(intSlots[i].pin) +
            " " + String(intSlots[i].lastHigh ? 1 : 0) +
            " " + String(micros())
        );
    }
}
```

### Step 4: Flash and verify manually

```bash
# On the board after flashing:
printf "INT 2 RISING\n" > /dev/ttyHS1   # configure interrupt on pin 2
cat /dev/ttyHS1 &                         # listen
# Tap pin 2 to GND or 3.3V
# Expect: TICK 2 1 <micros>
```

### Step 5: Commit

```bash
git add firmware/
git commit -m "feat: add INT command and TICK notifications to STM32 firmware"
```

---

## Task 6: Update docs and config example

**Files:**
- Modify: `viam_arduino_uno-q.md`
- Modify: `README.md` (if relevant)

### Step 1: Add interrupt config section to viam_arduino_uno-q.md

Add to the Attributes table:

| `digital_interrupts` | array | Optional | `[]` | Digital interrupt channels to expose by name |

Add a `digital_interrupts` items sub-table:

| `name` | string | Logical name for `DigitalInterruptByName` |
| `pin`  | string | Arduino pin number, e.g. `"2"` |
| `mode` | string | `"RISING"`, `"FALLING"`, or `"CHANGE"` (default) |

Add example config:

```json
{
  "serial_path": "/dev/ttyHS1",
  "digital_interrupts": [
    { "name": "encoder-a", "pin": "2", "mode": "CHANGE" },
    { "name": "button",    "pin": "3", "mode": "RISING"  }
  ]
}
```

Add a **Digital interrupts** capability section showing Python SDK usage:

```python
di = await board.digital_interrupt_by_name("encoder-a")
count = await di.value()  # cumulative tick count

ch = await board.stream_ticks([di])
async for tick in ch:
    print(tick.name, tick.high, tick.timestamp_nanosec)
```

### Step 2: Commit

```bash
git add viam_arduino_uno-q.md
git commit -m "docs: add digital interrupt configuration and usage"
```

---

## Verification

```bash
# Unit tests
go test -race ./...

# Build for board
GOARCH=arm64 GOOS=linux go build -o viam-arduino-uno-q ./cmd/module/

# Manual on-board test (after flashing firmware and deploying binary):
# 1. Configure interrupt in app.viam.com:
#    { "digital_interrupts": [{"name": "btn", "pin": "2", "mode": "RISING"}] }
# 2. In Viam Control tab → board → Digital Interrupts → "btn" → Value
# 3. Tap pin 2 to 3.3V — Value should increment
# 4. StreamTicks via SDK script — should receive board.Tick events
```
