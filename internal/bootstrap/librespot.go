package bootstrap

import (
	"context"
	"fmt"
	"io"
	"log"
	"path/filepath"
	"time"

	"github.com/lounge/tuify/internal/audio"
	"github.com/lounge/tuify/internal/config"
	"github.com/lounge/tuify/internal/librespot"
	"github.com/lounge/tuify/internal/spotify"
	"github.com/lounge/tuify/internal/ui"
)

// LibrespotServices holds the result of librespot/audio startup.
type LibrespotServices struct {
	Options []ui.ModelOption
	Cleanup func()
}

// StartLibrespot starts the librespot process and audio pipe reader if enabled
// by the config. Returns UI model options and a cleanup function, or an error
// if librespot was enabled but failed to start. If librespot is not enabled,
// returns (nil, nil). ctx is the app's root context, used so the reconnect
// handler's transfer requests are cancellable at shutdown.
func StartLibrespot(ctx context.Context, rc RuntimeConfig, client *spotify.Client) (*LibrespotServices, error) {
	if !rc.EnableLibrespot {
		return nil, nil
	}

	client.PreferredDevice = rc.ResolvedDeviceName

	backend := rc.AudioBackend
	if backend == "" {
		backend = librespot.DefaultBackend
	}

	dir, err := config.Dir()
	if err != nil {
		return nil, fmt.Errorf("resolve config dir: %w", err)
	}
	lsCfg := librespot.Config{
		BinaryPath: rc.LibrespotPath,
		DeviceName: rc.ResolvedDeviceName,
		Bitrate:    rc.Bitrate,
		Backend:    backend,
		Username:   rc.SpotifyUsername,
		CacheDir:   filepath.Join(dir, "librespot"),
	}

	var cleanups []func()
	var opts []ui.ModelOption

	var pipeRdr *audio.PipeReader
	if backend == "pipe" {
		pipeRdr = audio.NewPipeReader()
		cleanups = append(cleanups, pipeRdr.Stop)
	}

	librespotProc := librespot.NewProcess(lsCfg)
	librespotProc.OnReconnect = reconnectHandler(ctx, client, rc.ResolvedDeviceName)

	if pipeRdr != nil {
		librespotProc.OnStdout = func(pipe io.ReadCloser) {
			pipeRdr.Start(pipe)
		}
	}

	inactiveCh := make(chan struct{}, 1)
	librespotProc.OnInactive = func() {
		select {
		case inactiveCh <- struct{}{}:
		default:
		}
	}

	if err := librespotProc.Start(); err != nil {
		// Run cleanups we've queued (pipe reader) so the partial startup
		// doesn't leak resources, then surface the failure to the caller.
		for i := len(cleanups) - 1; i >= 0; i-- {
			cleanups[i]()
		}
		return nil, fmt.Errorf("librespot failed to start: %w", err)
	}
	cleanups = append(cleanups, func() { _ = librespotProc.Stop() })

	// Only expose the audio source and inactive channel to the UI once we
	// know librespot is actually running; otherwise the UI would poll a
	// dead pipe and listen for inactive signals that never arrive.
	if pipeRdr != nil {
		opts = append(opts, ui.WithAudioSource(pipeRdr))
	}
	opts = append(opts, ui.WithLibrespotInactive(inactiveCh))

	return &LibrespotServices{
		Options: opts,
		Cleanup: func() {
			// Cleanup in reverse order (librespot before pipe reader).
			for i := len(cleanups) - 1; i >= 0; i-- {
				cleanups[i]()
			}
		},
	}, nil
}

// reconnectHandler returns a callback for librespot reconnection that
// transfers playback back to the preferred device (unless overridden).
// parent is the app's root context; cancelling it aborts any in-flight
// reconnect transfer instead of letting it linger past shutdown.
func reconnectHandler(parent context.Context, client *spotify.Client, deviceName string) func() {
	return func() {
		select {
		case <-time.After(2 * time.Second):
		case <-parent.Done():
			return
		}
		if client.DeviceOverridden.Load() {
			log.Printf("[librespot] reconnect: device was manually switched, skipping transfer")
			return
		}
		ctx, cancel := context.WithTimeout(parent, 10*time.Second)
		defer cancel()
		devID, _, _, err := client.FindDevice(ctx, false)
		if err != nil {
			log.Printf("[librespot] reconnect: could not find device: %v", err)
			return
		}
		if err := client.TransferPlayback(ctx, devID, true); err != nil {
			log.Printf("[librespot] reconnect: transfer playback failed: %v", err)
		} else {
			log.Printf("[librespot] reconnect: playback transferred to %s", deviceName)
		}
	}
}
