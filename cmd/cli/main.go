package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"arduino"
	board "go.viam.com/rdk/components/board"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
)

func main() {
	serial := flag.String("serial", "/dev/ttyHS1", "serial port path (e.g. /dev/ttyHS1)")
	baud := flag.Int("baud", 115200, "baud rate")
	pin := flag.String("pin", "", "GPIO pin to read (e.g. 13)")
	flag.Parse()

	if *serial == "" {
		fmt.Fprintln(os.Stderr, "usage: arduino-cli -serial /dev/ttyHS1 [-pin 13]")
		os.Exit(1)
	}

	ctx := context.Background()
	logger := logging.NewLogger("cli")
	deps := resource.Dependencies{}

	cfg := arduino.Config{
		SerialPath: *serial,
		BaudRate:   *baud,
	}

	b, err := arduino.NewUnoQ(ctx, deps, board.Named("board"), &cfg, logger)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer b.Close(ctx)

	log.Printf("connected to Arduino Uno Q on %s", *serial)

	if *pin != "" {
		p, err := b.GPIOPinByName(*pin)
		if err != nil {
			log.Fatalf("gpio: %v", err)
		}
		high, err := p.Get(ctx, nil)
		if err != nil {
			log.Fatalf("get pin %s: %v", *pin, err)
		}
		log.Printf("pin %s = %v", *pin, high)
	}
}
