package main

import (
	"os"

	"github.com/ril3y/velotool/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
