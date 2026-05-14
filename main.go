package main

import (
	"fmt"
	"os"

	"github.com/lounge/tuify/internal/bootstrap"
)

// version is injected at build time via -ldflags "-X main.version=…".
// Defaults to "dev" for local builds.
var version = "dev"

func main() {
	for _, arg := range os.Args[1:] {
		switch arg {
		case "--version", "-v":
			fmt.Println(version)
			return
		case "--help", "-h":
			printUsage()
			return
		}
	}
	if err := bootstrap.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("Usage: tuify [flags]")
	fmt.Println()
	fmt.Println("Flags:")
	fmt.Println("  -v, --version   Print version and exit")
	fmt.Println("  -h, --help      Show this help")
	fmt.Println()
	fmt.Println("Run with no flags to launch the TUI.")
}
