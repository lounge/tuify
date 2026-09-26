package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"runtime/debug"

	"github.com/lounge/tuify/internal/bootstrap"
)

// version is injected at build time via -ldflags "-X main.version=…".
// Defaults to "dev"; resolveVersion then falls back to the module version
// (go install …@vX) or the VCS revision embedded by go build.
var version = "dev"

func main() {
	for _, arg := range os.Args[1:] {
		switch arg {
		case "--version", "-v":
			fmt.Println(resolveVersion())
			return
		case "--help", "-h":
			printUsage(os.Stdout)
			return
		default:
			fmt.Fprintf(os.Stderr, "tuify: unknown argument %q\n\n", arg)
			printUsage(os.Stderr)
			os.Exit(2)
		}
	}
	if err := bootstrap.Run(); err != nil {
		if errors.Is(err, bootstrap.ErrInterrupted) {
			os.Exit(130) // 128 + SIGINT, as shells report it
		}
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func resolveVersion() string {
	if version != "dev" {
		return version
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return version
	}
	if v := info.Main.Version; v != "" && v != "(devel)" {
		return v
	}
	var rev string
	var dirty bool
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if rev == "" {
		return version
	}
	if len(rev) > 12 {
		rev = rev[:12]
	}
	if dirty {
		rev += "-dirty"
	}
	return "dev-" + rev
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: tuify [flags]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Flags:")
	fmt.Fprintln(w, "  -v, --version   Print version and exit")
	fmt.Fprintln(w, "  -h, --help      Show this help")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Run with no flags to launch the TUI.")
}
