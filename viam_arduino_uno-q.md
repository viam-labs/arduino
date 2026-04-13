# `viam:arduino:uno-q`

A [Viam board component](https://docs.viam.com/components/board/) for the **Arduino Uno Q**. Runs on the Qualcomm Linux SoC and controls the STM32U585 coprocessor's GPIO, PWM, and ADC over a UART serial connection.

## Requirements

Before configuring this component, flash `firmware/uno-q-firmware.ino` to the STM32 coprocessor:

1. Open `firmware/uno-q-firmware.ino` in the Arduino IDE
2. Select **Arduino Uno Q** as the target board (requires STM32 board support installed)
3. Upload — the firmware responds `OK UNO-Q v1` on startup

## Configuration

```json
{
  "serial_path": "/dev/ttyACM0",
  "baud_rate": 115200,
  "analogs": [
    { "name": "light_sensor", "pin": "0" },
    { "name": "battery_voltage", "pin": "1" }
  ]
}
```

### Attributes

| Name | Type | Inclusion | Default | Description |
|------|------|-----------|---------|-------------|
| `serial_path` | string | **Required** | — | Path to the UART serial device that connects to the STM32. Linux: `/dev/ttyACM0`. macOS: use the **`/dev/cu.`** prefix (e.g. `/dev/cu.usbmodem30985457372`) — the `tty.` variant stalls on reads because Arduino never asserts Carrier Detect. |
| `baud_rate` | int | Optional | `115200` | Serial baud rate. Must match the firmware (default 115200). |
| `analogs` | array | Optional | `[]` | List of analog input channels to expose by name (see below) |

#### `analogs` items

| Name | Type | Description |
|------|------|-------------|
| `name` | string | Logical name used to retrieve this reader via `AnalogByName` |
| `pin` | string | ADC channel: `"0"` through `"5"` (maps to A0–A5) |

### Minimal configuration

```json
{
  "serial_path": "/dev/ttyACM0"
}
```

### Full configuration example

```json
{
  "serial_path": "/dev/ttyACM0",
  "baud_rate": 115200,
  "analogs": [
    { "name": "a0", "pin": "0" },
    { "name": "a1", "pin": "1" },
    { "name": "a2", "pin": "2" },
    { "name": "a3", "pin": "3" },
    { "name": "a4", "pin": "4" },
    { "name": "a5", "pin": "5" }
  ]
}
```

## Capabilities

### GPIO (digital I/O)

All Arduino digital pins are available by number (`"0"` – `"13"` and beyond). Pins are created lazily on first access.

```python
board = Board.from_robot(robot, "my-board")
pin = await board.gpio_pin_by_name("13")   # onboard LED
await pin.set(True)                         # drive high
high = await pin.get()                      # read back
```

### PWM

PWM is supported on pins **3, 5, 6, 9, 10, 11** (mapped to STM32 TIM1/TIM2/TIM3). Calling PWM methods on any other pin returns an error.

```python
pin = await board.gpio_pin_by_name("9")
await pin.set_pwm(0.5)           # 50 % duty cycle
await pin.set_pwm_freq(1000)     # 1 kHz
```

> **Note:** `PWM()` (read duty cycle) and `PWMFreq()` (read frequency) are not supported by the STM32 firmware and return an error. Use `SetPWM` / `SetPWMFreq` to write, and track state in your application if needed.

### Analog input (ADC)

Channels A0–A5 provide 12-bit reads (0–4095) with a 3.3 V reference. Channels must be declared in the `analogs` config array before use.

```python
reader = await board.analog_by_name("light_sensor")
reading = await reader.read()
print(reading.value)      # 0–4095
print(reading.max)        # 3.3 (volts)
print(reading.step_size)  # ~0.000806 V/LSB
```

Analog **write** is not supported — A0–A5 are input-only pins.

## Not supported in v1

| Feature | Status |
|---------|--------|
| Digital interrupts | Not supported — `DigitalInterruptByName` returns an error |
| `SetPowerMode` | Not supported |
| `StreamTicks` | Not supported |
| Analog write | Not supported (A0–A5 are input-only) |
| `PWM()` / `PWMFreq()` (read back) | Not supported by STM32 firmware |

## Serial protocol

The module communicates with the STM32 using a simple ASCII line protocol at 115200 baud. Each command is a newline-terminated string; each response is `OK [value]` or `ERR message`.

| Command | Response | Description |
|---------|----------|-------------|
| `HELLO` | `OK UNO-Q v1` | Firmware version handshake (sent on connect and reconfigure) |
| `GET <pin>` | `OK 0` or `OK 1` | Read digital pin state |
| `SET <pin> <0\|1>` | `OK` | Drive pin high or low |
| `PWM <pin> <duty>` | `OK` | Set PWM duty cycle (0.0000–1.0000) |
| `PWMGET <pin>` | `ERR …` | Not supported by firmware |
| `FREQ <pin> <hz>` | `OK` | Set PWM frequency in Hz |
| `FREQGET <pin>` | `ERR …` | Not supported by firmware |
| `ADC <channel>` | `OK <0-4095>` | Read 12-bit ADC value |
