# `viam:arduino` — Arduino Uno Q board module

A [Viam modular component](https://docs.viam.com/registry/) that exposes the **Arduino Uno Q**'s GPIO, PWM, and analog I/O to a viam-server machine.

The module runs on the Uno Q's **Qualcomm Linux SoC** and communicates with the onboard **STM32U585** coprocessor over the internal UART (`/dev/ttyHS1`). It implements the [`rdk:component:board`](https://docs.viam.com/components/board/) API.

## Architecture

```
viam-server (Qualcomm Linux SoC)
    └── viam:arduino module
            │ UART /dev/ttyHS1 @ 115200 baud
            ▼
        STM32U585 (firmware/uno-q-firmware/uno-q-firmware.ino)
            └── GPIO / PWM / ADC headers
```

The module opens the UART directly after stopping the `arduino-router` service, which normally owns that port. A GPIO wake signal (gpiochip1 pin 37) is pulsed before opening the port to bring the STM32 into a ready state.

## Models

| Model | Description |
|-------|-------------|
| [`viam:arduino:uno-q`](viam_arduino_uno-q.md) | Board component for the Arduino Uno Q |

## Requirements

- **Hardware:** Arduino Uno Q
- **Firmware:** Flash `firmware/uno-q-firmware/uno-q-firmware.ino` to the STM32 via Arduino IDE with the `arduino:zephyr` platform installed. The firmware uses `Serial1` (D0/D1, the Qualcomm-facing UART) — not `Serial` (USB CDC).
- **arduino-router service:** Must be stopped before the module starts, otherwise it holds `/dev/ttyHS1` exclusively.

## Setup

The `setup.sh` script runs automatically on first install (via Viam's `first_run` mechanism). It:

1. **Flashes the STM32 firmware** via `arduino-cli` (if available on the board)
2. **Stops and permanently disables `arduino-router`** so the module can own `/dev/ttyHS1` directly

If `arduino-cli` is not available, the firmware must be flashed manually:

1. Install the `arduino:zephyr` platform in Arduino IDE via Boards Manager
2. Select **Arduino Uno Q** as the target board  
3. Open `firmware/uno-q-firmware/uno-q-firmware.ino` and upload

The firmware uses `Serial1` (D0/D1 — the Qualcomm-facing hardware UART) at 115200 baud, **not** `Serial` (USB CDC to the host).

## Deployment

The module binary must be compiled for Linux ARM64 and copied to the board:

```bash
# Cross-compile on your Mac
GOARCH=arm64 GOOS=linux go build -o viam-arduino-uno-q ./cmd/module/

# Copy to the board
scp viam-arduino-uno-q arduino@<board-ip>:/home/arduino/viam-arduino-uno-q
```

Configure as a local module in [app.viam.com](https://app.viam.com):

```json
{
  "modules": [
    {
      "type": "local",
      "name": "arduino",
      "executable_path": "/home/arduino/viam-arduino-uno-q"
    }
  ]
}
```

## Development

```bash
# Install dependencies
make setup

# Run unit tests (uses mock serial — no hardware required)
go test -race ./...

# Cross-compile for the board
GOARCH=arm64 GOOS=linux go build -o viam-arduino-uno-q ./cmd/module/
```

See [DEVELOPER_GUIDE.md](DEVELOPER_GUIDE.md) for details on the module lifecycle.
