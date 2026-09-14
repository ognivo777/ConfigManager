package main

import (
	"os"

	"github.com/example/cm/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:]))
}
