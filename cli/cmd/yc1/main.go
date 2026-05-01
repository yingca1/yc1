package main

import (
	"os"

	"github.com/yingca1/yc1/cli/internal/yc1"
)

var version = "dev"

func main() {
	app := yc1.NewApp(version, os.Stdout, os.Stderr)
	os.Exit(app.Run(os.Args[1:]))
}
