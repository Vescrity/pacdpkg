package main

import (
	"os"

	"pacdpkg/internal/app"
)

func main() {
	os.Exit(app.Run(os.Args))
}
