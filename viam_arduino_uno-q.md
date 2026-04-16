# `viam:arduino:uno-q`

A [Viam board component](https://docs.viam.com/components/board/) for the **Arduino Uno Q**. Runs on the Qualcomm Linux SoC and controls the STM32U585 coprocessor's GPIO, PWM, and ADC over the internal UART (`/dev/ttyHS1`).

## How it works

```
viam-server (Qualcomm Linux)
    └── viam:arduino module
            │ /dev/ttyHS1 @ 115200 baud
            ▼
        STM32U585 (uno-q-firmware.ino)
            └── GPIO / PWM / ADC headers
```

On startup the module opens `/dev/ttyHS1` and performs a `HELLO` handshake to confirm firmware version. The STM32 wake signal (GPIO 37) is pulsed once by `setup.sh` during first-run — not on each module start. A **board reset is required** after first-run completes before the module can connect.

## Requirements

- `arduino-router` service must be **stopped and disabled** — it owns `/dev/ttyHS1` by default and blocks the module from opening it.
- The STM32 must be running `firmware/uno-q-firmware.ino`.

Both are handled automatically by the `setup.sh` first-run script when the module is installed via the Viam Registry. For manual setup see below.

## Manual setup (if not using registry)

```bash
# 1. Stop the router
sudo systemctl stop arduino-router
sudo systemctl disable arduino-router

# 2. Flash firmware via Arduino IDE
#    - Install the arduino:zephyr platform in Boards Manager
#    - Select "Arduino Uno Q" as the target board
#    - Open firmware/uno-q-firmware/uno-q-firmware.ino and upload
#    NOTE: The router must be running during the flash (it provides JTAG access).
#    Start it, flash, then stop it again.
```

The firmware uses `Serial1` (D0/D1 — the hardware UART to the Qualcomm SoC). Do not use `Serial` (USB CDC to the host Mac/PC).

## Configuration

```json
{
  "serial_path": "/dev/ttyHS1",
  "analogs": [
    { "name": "light_sensor", "pin": "0" },
    { "name": "battery_voltage", "pin": "1" }
  ]
}
```

### Attributes

| Name | Type | Inclusion | Default | Description |
|------|------|-----------|---------|-------------|
| `serial_path` | string | **Required** | — | UART device path. On the Uno Q this is `/dev/ttyHS1`. On macOS (development only) use the `/dev/cu.` prefix — the module rewrites `/dev/tty.` automatically. |
| `baud_rate` | int | Optional | `115200` | Serial baud rate. Must match the firmware. |
| `analogs` | array | Optional | `[]` | Analog input channels to expose by name (see below). |
| `digital_interrupts` | array | Optional | `[]` | Digital interrupt channels to expose by name (see below). |

#### `analogs` items

| Name | Type | Description |
|------|------|-------------|
| `name` | string | Logical name used to retrieve this reader via `AnalogByName` |
| `pin` | string | ADC channel: `"0"` through `"5"` (maps to A0–A5) |

#### `digital_interrupts` items

| Name | Type | Description |
|------|------|-------------|
| `name` | string | Logical name used to retrieve this interrupt via `DigitalInterruptByName` |
| `pin` | string | Arduino pin number, e.g. `"2"` |
| `mode` | string | `"RISING"`, `"FALLING"`, or `"CHANGE"` (default) |

### Minimal configuration

```json
{
  "serial_path": "/dev/ttyHS1"
}
```

### Full configuration example

```json
{
  "serial_path": "/dev/ttyHS1",
  "baud_rate": 115200,
  "analogs": [
    { "name": "a0", "pin": "0" },
    { "name": "a1", "pin": "1" },
    { "name": "a2", "pin": "2" },
    { "name": "a3", "pin": "3" },
    { "name": "a4", "pin": "4" },
    { "name": "a5", "pin": "5" }
  ],
  "digital_interrupts": [
    { "name": "encoder-a", "pin": "2", "mode": "CHANGE" },
    { "name": "button",    "pin": "3", "mode": "RISING"  }
  ]
}
```

## Capabilities

### GPIO (digital I/O)

All STM32 digital pins are available by Arduino pin number. Pins are created lazily on first access.

```python
board = Board.from_robot(robot, "my-board")
pin = await board.gpio_pin_by_name("2")
await pin.set(True)           # drive high (3.3 V)
high = await pin.get()        # read back
```

> **Note:** Pin 13 is the SPI clock (SCK) on the Uno Q and does **not** have an onboard LED unlike classic Arduino boards.

### PWM

PWM is supported on pins **3, 5, 6, 9, 10, 11** (mapped to STM32 TIM1/TIM2/TIM3). Calling PWM methods on any other pin returns an error.

```python
pin = await board.gpio_pin_by_name("9")
await pin.set_pwm(0.5)        # 50% duty cycle
await pin.set_pwm_freq(1000)  # 1 kHz
```

> `PWM()` (read duty cycle) and `PWMFreq()` (read frequency) are not supported by the STM32 firmware and return an error. Track state in your application if needed.

### Analog input (ADC)

Channels A0–A5 provide 12-bit reads (0–4095) with a 3.3 V reference. Channels must be declared in the `analogs` config array before use.

```python
reader = await board.analog_by_name("light_sensor")
reading = await reader.read()
print(reading.value)      # 0–4095
print(reading.max)        # 3.3 (volts)
print(reading.step_size)  # ~0.000806 V/LSB
```

Analog write is not supported — A0–A5 are input-only pins.

### Digital interrupts

Pins must be declared in the `digital_interrupts` config array before use. The cumulative tick count increments on every interrupt edge since the module started.

```python
di = await board.digital_interrupt_by_name("encoder-a")
count = await di.value()  # cumulative tick count since module start

async def watch_ticks():
    async for tick in board.stream_ticks([di], {}):
        print(tick.name, "high=" + str(tick.high), tick.timestamp_nanosec)
```

## Not supported in v1

| Feature | Status |
|---------|--------|
| `SetPowerMode` | Not supported |
| Analog write | A0–A5 are input-only |
| `PWM()` / `PWMFreq()` (read back) | Not supported by STM32 firmware |

## Serial protocol

The module communicates with the STM32 using a simple ASCII line protocol at 115200 baud. Each command is newline-terminated; each response is `OK [value]` or `ERR message`. Arduino's `Serial.println` sends CR+LF — the module strips the `\r` automatically.

| Command | Response | Description |
|---------|----------|-------------|
| `HELLO` | `OK UNO-Q v1` | Firmware version handshake — sent on connect and reconfigure |
| `GET <pin>` | `OK 0` or `OK 1` | Read digital pin state |
| `SET <pin> <0\|1>` | `OK` | Drive pin high or low |
| `PWM <pin> <duty>` | `OK` | Set PWM duty cycle (0.0000–1.0000) |
| `PWMGET <pin>` | `ERR …` | Not supported |
| `FREQ <pin> <hz>` | `OK` | Set PWM frequency in Hz |
| `FREQGET <pin>` | `ERR …` | Not supported |
| `ADC <channel>` | `OK <0-4095>` | Read 12-bit ADC value |
| `INT <pin> <mode>` | `OK` or `ERR …` | Configure hardware interrupt (RISING / FALLING / CHANGE). NONE detaches. |
| `TICK <pin> <high> <micros>` | — (unsolicited) | Emitted on each interrupt edge. `high` is 0 or 1; `micros` is the STM32 microsecond timestamp. |

## Platform notes

### Linux (Qualcomm MSM UART)

The MSM UART driver on this SoC reports itself as "always readable" in `poll()`, which causes Go's standard `bufio` and `select()`-based reads to block forever in a kernel syscall. The module works around this by opening `/dev/ttyHS1` with `O_NONBLOCK` (kept permanently set), using `syscall.Read` directly, and running reads in a goroutine with `select { case <-ctx.Done() }` for cancellation.

### macOS (development / testing)

`/dev/tty.usbmodem*` paths stall on reads because Arduino never asserts Carrier Detect. The module automatically rewrites `/dev/tty.` to `/dev/cu.` on macOS. Use the `cu.` path in your config or let the rewrite handle it.
