#!/bin/bash
# setup.sh — first-run setup for viam:arduino on the Arduino Uno Q.
# Viam runs this once automatically when the module is first installed.
#
# What this does:
#   1. Downloads arduino-cli if not already installed.
#   2. Installs the arduino:zephyr platform if not already installed.
#   3. Flashes firmware/uno-q-firmware/uno-q-firmware.ino to the STM32 coprocessor via
#      arduino-cli (requires the arduino-router to be running for JTAG access).
#   4. Stops and permanently disables arduino-router so the viam:arduino module
#      can claim /dev/ttyHS1 exclusively.

set -euo pipefail

log() { echo "[viam:arduino setup] $*"; }

# Only run on Linux ARM64 (the Qualcomm SoC side of the Arduino Uno Q).
if [ "$(uname -m)" != "aarch64" ] || [ "$(uname -s)" != "Linux" ]; then
    log "Not running on Linux ARM64 — skipping Arduino Uno Q setup."
    exit 0
fi

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
FIRMWARE="$SCRIPT_DIR/firmware/uno-q-firmware/uno-q-firmware.ino"

# Detect the correct FQBN using arduino-cli rather than hardcoding it.
# Board IDs vary by platform version; querying the CLI is authoritative.
detect_fqbn() {
    "$ARDUINO_CLI" board listall 2>/dev/null \
        | grep -i "Uno Q\|uno.*q" \
        | awk '{print $NF}' \
        | grep "arduino:zephyr" \
        | head -1
}
FQBN=""
ARDUINO_CLI_INSTALL_DIR="/usr/local/bin"
ARDUINO_CLI="$ARDUINO_CLI_INSTALL_DIR/arduino-cli"

# ---------------------------------------------------------------------------
# Step 1: Ensure arduino-cli is installed
# ---------------------------------------------------------------------------
install_arduino_cli() {
    log "arduino-cli not found — downloading for Linux ARM64..."

    # Fetch the latest release tag from GitHub
    LATEST=$(curl -fsSL "https://api.github.com/repos/arduino/arduino-cli/releases/latest" \
        | grep '"tag_name"' | head -1 | sed 's/.*"tag_name": *"//;s/".*//')

    if [ -z "$LATEST" ]; then
        log "ERROR: Could not determine latest arduino-cli version."
        return 1
    fi

    # Strip leading 'v' for the download URL filename
    VERSION="${LATEST#v}"
    URL="https://github.com/arduino/arduino-cli/releases/download/${LATEST}/arduino-cli_${VERSION}_Linux_ARM64.tar.gz"

    log "Downloading arduino-cli ${VERSION}..."
    TMP=$(mktemp -d)
    trap "rm -rf $TMP" EXIT

    curl -fsSL "$URL" -o "$TMP/arduino-cli.tar.gz"
    tar -xzf "$TMP/arduino-cli.tar.gz" -C "$TMP"
    install -m 755 "$TMP/arduino-cli" "$ARDUINO_CLI_INSTALL_DIR/arduino-cli"

    log "arduino-cli ${VERSION} installed to $ARDUINO_CLI_INSTALL_DIR"
}

if ! command -v arduino-cli &>/dev/null; then
    install_arduino_cli || {
        log "WARNING: Could not install arduino-cli automatically."
        log "  Flash firmware/uno-q-firmware/uno-q-firmware.ino manually via Arduino IDE"
        log "  with board: $FQBN"
        log "  Then re-run this script or restart viam-agent."
        # Still proceed to disable arduino-router below
        ARDUINO_CLI=""
    }
fi

# Re-check after attempted install
ARDUINO_CLI=$(command -v arduino-cli 2>/dev/null || true)

# ---------------------------------------------------------------------------
# Step 2: Install the arduino:zephyr platform if needed
# ---------------------------------------------------------------------------
if [ -n "$ARDUINO_CLI" ]; then
    if ! "$ARDUINO_CLI" core list 2>/dev/null | grep -q "arduino:zephyr"; then
        log "Installing arduino:zephyr board platform..."
        "$ARDUINO_CLI" core update-index 2>&1 || true
        "$ARDUINO_CLI" core install arduino:zephyr 2>&1 && \
            log "arduino:zephyr platform installed." || \
            log "WARNING: Platform install failed — firmware flash may not work."
    else
        log "arduino:zephyr platform already installed."
    fi
fi

# ---------------------------------------------------------------------------
# Step 3: Flash STM32 firmware
# The arduino-router provides JTAG/SWD access to the STM32 for flashing.
# It must be running during this step.
# ---------------------------------------------------------------------------
if [ -n "$ARDUINO_CLI" ] && [ -f "$FIRMWARE" ]; then
    FQBN=$(detect_fqbn)
    if [ -z "$FQBN" ]; then
        log "WARNING: Could not detect Arduino Uno Q FQBN from installed platform."
        log "  Flash $FIRMWARE manually via Arduino IDE."
    else
        log "Detected FQBN: $FQBN"
    fi
fi

if [ -n "$ARDUINO_CLI" ] && [ -f "$FIRMWARE" ] && [ -n "$FQBN" ]; then
    log "Ensuring arduino-router is running for JTAG access..."
    systemctl start arduino-router 2>/dev/null || true
    sleep 3  # allow router to initialize

    log "Compiling firmware..."
    if "$ARDUINO_CLI" compile --fqbn "$FQBN" "$FIRMWARE" 2>&1; then
        log "Uploading firmware to STM32..."
        if "$ARDUINO_CLI" upload --fqbn "$FQBN" "$FIRMWARE" 2>&1; then
            log "Firmware flashed successfully."
        else
            log "WARNING: Firmware upload failed."
            log "  Flash firmware/uno-q-firmware/uno-q-firmware.ino manually via Arduino IDE."
        fi
    else
        log "WARNING: Firmware compile failed."
        log "  Flash firmware/uno-q-firmware/uno-q-firmware.ino manually via Arduino IDE."
    fi
elif [ ! -f "$FIRMWARE" ]; then
    log "WARNING: Firmware not found at $FIRMWARE — skipping flash."
fi

# ---------------------------------------------------------------------------
# Step 4: Stop and permanently disable arduino-router
# The viam:arduino module communicates directly over /dev/ttyHS1.
# The router holds that port exclusively and must not run alongside the module.
# ---------------------------------------------------------------------------
log "Stopping and disabling arduino-router (module will own /dev/ttyHS1 directly)..."
systemctl stop    arduino-router 2>/dev/null || true
systemctl disable arduino-router 2>/dev/null || true

log "Setup complete."
log "The viam:arduino module will pulse GPIO 37 to wake the STM32 on each"
log "connect and communicate directly over /dev/ttyHS1 at 115200 baud."
