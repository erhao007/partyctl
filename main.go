package main

import (
	"os"

	"github.com/partyctl/partyctl/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
