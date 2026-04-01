package main

import (
	"os"

	"github.com/dreadnode/dreadgoad/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
