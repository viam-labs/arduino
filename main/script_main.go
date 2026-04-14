package main

import (
	"context"

	"go.viam.com/rdk/components/board"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/robot/client"
	"go.viam.com/utils/rpc"
)

func main() {
	logger := logging.NewDebugLogger("client")
	machine, err := client.New(
		context.Background(),
		"arduino-main.165ui2j6us.viam.cloud",
		logger,
		client.WithDialOptions(rpc.WithEntityCredentials(

			"3a42d1be-8070-4522-8aeb-f9df7c52a63a",
			rpc.Credentials{
				Type: rpc.CredentialsTypeAPIKey,

				Payload: "xc1d4zipke0w00mf2789reoch1tvyc9h",
			})),
	)
	if err != nil {
		logger.Fatal(err)
	}

	defer machine.Close(context.Background())
	logger.Info("Resources:")
	logger.Info(machine.ResourceNames())

	// Note that the pin supplied is a placeholder. Please change this to a valid pin.
	// arduino
	arduino, err := board.FromProvider(machine, "arduino")
	if err != nil {
		logger.Error(err)
		return
	}
	arduinoReturnValue, err := arduino.GPIOPinByName("16")
	if err != nil {
		logger.Error(err)
		return
	}
	logger.Infof("arduino GPIOPinByName return value: %+v", arduinoReturnValue)

	err = arduinoReturnValue.Set(context.Background(), true, nil)
	if err != nil {
		logger.Error(err)
		return
	}

	high, err := arduinoReturnValue.Get(context.Background(), nil)
	if err != nil {
		logger.Error(err)
		return
	}

	logger.Info(high)
}
