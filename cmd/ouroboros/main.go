package main

import (
	"fmt"
	"os"
)

// Version is set at build time via -ldflags.
// Defaults to "dev" when built without ldflags.
var Version = "dev"

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
