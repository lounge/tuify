package main

import (
	"context"
	"fmt"
	"os"

	"github.com/lounge/tuify/internal/app"
	"github.com/lounge/tuify/internal/audio"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--audio-worker" {
		runAudioWorker(os.Args[2:])
		return
	}

	if err := app.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runAudioWorker(args []string) {
	fmt.Fprintf(os.Stderr, "[audio-worker] started with args: %v\n", args)

	var socketPath string
	for i, a := range args {
		if a == "--socket" && i+1 < len(args) {
			socketPath = args[i+1]
		}
	}
	if socketPath == "" {
		fmt.Fprintln(os.Stderr, "[audio-worker] error: --socket <path> required")
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "[audio-worker] connecting to socket: %s\n", socketPath)

	w := &audio.Worker{
		Format:     audio.DefaultFormat,
		SocketPath: socketPath,
	}
	if err := w.Run(context.Background(), os.Stdin); err != nil {
		fmt.Fprintf(os.Stderr, "[audio-worker] error: %v\n", err)
		os.Exit(1)
	}
}
