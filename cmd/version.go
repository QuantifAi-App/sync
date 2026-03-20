// Package cmd provides CLI subcommands for the quantifai-sync binary.

package cmd

import "fmt"

// Version is the binary version, set at build time via -ldflags or
// defaulting to the value below for development builds.
var Version = "0.1.0"

// RunVersion prints the binary version to stdout and returns exit code 0.
func RunVersion() int {
	fmt.Printf("quantifai-sync %s\n", Version)
	return 0
}
