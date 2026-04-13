package main

import (
	"arduino"
	"go.viam.com/rdk/module"
	"go.viam.com/rdk/resource"
	board "go.viam.com/rdk/components/board"
)

func main() {
	// ModularMain can take multiple APIModel arguments, if your module implements multiple models.
	module.ModularMain(resource.APIModel{API: board.API, Model: arduino.UnoQ})
}
