//go:build !linux

package arduino

import "fmt"

// openSerialLinux is not supported on non-Linux platforms.
// This module targets Linux ARM64 (the Qualcomm SoC on the Arduino Uno Q).
// This stub exists only so the package compiles on macOS for unit-test runs
// (which use mockSender and never call openSerial).
func openSerialLinux(_ string, _ int) (*serialConn, error) {
	return nil, fmt.Errorf("serial port access not supported on this platform (Linux required)")
}
