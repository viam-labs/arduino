package main

import (
	"context"
	"arduino"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
	board "go.viam.com/rdk/components/board"
)

func main() {
	err := realMain()
	if err != nil {
		panic(err)
	}
}

func realMain() error {
	ctx := context.Background()
	logger := logging.NewLogger("cli")

	deps := resource.Dependencies{}
	// can load these from a remote machine if you need

	cfg := arduino.Config{}

	thing, err := arduino.NewUnoQ(ctx, deps, board.Named("foo"), &cfg, logger)
	if err != nil {
		return err
	}
	defer thing.Close(ctx)

	return nil
}
