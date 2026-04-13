# `viam:arduino` — Arduino Uno Q board module

A [Viam modular component](https://docs.viam.com/registry/) that exposes the **Arduino Uno Q**'s GPIO, PWM, and analog I/O to a viam-server machine.

The module runs on the Uno Q's Qualcomm Linux SoC and communicates with the onboard STM32U585 microcontroller over UART. It implements the [`rdk:component:board`](https://docs.viam.com/components/board/) API.

## Models

| Model | Description |
|-------|-------------|
| [`viam:arduino:uno-q`](viam_arduino_uno-q.md) | Board component for the Arduino Uno Q |

## Requirements

- **Hardware:** Arduino Uno Q
- **Firmware:** Flash `firmware/uno-q-firmware.ino` to the STM32 coprocessor using the Arduino IDE with Uno Q board support installed. The firmware must report version `UNO-Q v1` or the module will refuse to start.
- **OS:** Linux arm64 (Qualcomm SoC) or macOS arm64 for local development

## Installation

Add the module to your machine via the [Viam Registry](https://app.viam.com/registry):

1. Search for `viam:arduino` in the Viam Registry
2. Click **Add to machine**
3. Configure the component (see [model docs](viam_arduino_uno-q.md))

### Build from source

```bash
git clone https://github.com/viamrobotics/viam-arduino-uno-q
cd viam-arduino-uno-q
make module.tar.gz
```

The resulting `module.tar.gz` can be deployed to your machine as a local module.

## Development

```bash
# Install dependencies
make setup

# Run unit tests (no hardware required)
go test -race ./...

# Run hardware integration tests (Arduino Uno Q connected via USB)
go test -tags hardware -v -serial /dev/ttyACM0 ./...

# Build the module binary
make
```

See [DEVELOPER_GUIDE.md](DEVELOPER_GUIDE.md) for details on the module lifecycle and how to extend this module.
