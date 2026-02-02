package main

import (
	"os"

	"github.com/vosiander/pgsnap/cmd/pgsnap/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
