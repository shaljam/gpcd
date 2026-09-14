package main

import (
	"os"

	"github.com/shaljam/gpcd/internal/cli"
)

var version = ""

func main() {
	cli.Run(version, os.Args)
}
