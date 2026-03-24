package audio

import (
	"fmt"
	"log"
	"net"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// Receiver listens on a unix socket for FrequencyData from the audio worker.
type Receiver struct {
	socketPath string
	listener   net.Listener
	latest     atomic.Pointer[FrequencyData]
	lastUpdate atomic.Int64 // unix nanos of last frame
	connected  atomic.Bool
	done       chan struct{}
	stopOnce   sync.Once
}

// NewReceiver creates a Receiver with an auto-generated socket path.
func NewReceiver() *Receiver {
	return &Receiver{
		socketPath: socketPathForPlatform(),
		done:       make(chan struct{}),
	}
}

func socketPathForPlatform() string {
	if runtime.GOOS == "windows" {
		// Windows doesn't support unix domain sockets reliably; use a temp file path
		// that will be replaced with TCP in the listen logic.
		return "localhost:0"
	}
	return fmt.Sprintf("/tmp/tuify-audio-%d.sock", os.Getpid())
}

// Start creates the unix socket and begins accepting connections.
func (r *Receiver) Start() error {
	// Clean up stale socket from a previous crash.
	if runtime.GOOS != "windows" {
		_ = os.Remove(r.socketPath)
	}

	network := "unix"
	if runtime.GOOS == "windows" {
		network = "tcp"
	}

	ln, err := net.Listen(network, r.socketPath)
	if err != nil {
		return fmt.Errorf("audio receiver listen: %w", err)
	}
	r.listener = ln

	// On Windows with port 0, update socketPath to the actual address.
	if runtime.GOOS == "windows" {
		r.socketPath = ln.Addr().String()
	}

	go r.acceptLoop()
	log.Printf("[audio-receiver] listening on %s", r.socketPath)
	return nil
}

func (r *Receiver) acceptLoop() {
	for {
		conn, err := r.listener.Accept()
		if err != nil {
			select {
			case <-r.done:
				return
			default:
				log.Printf("[audio-receiver] accept error: %v", err)
				continue // transient error — keep accepting
			}
		}
		log.Printf("[audio-receiver] worker connected")
		r.connected.Store(true)
		go r.readLoop(conn)
	}
}

func (r *Receiver) readLoop(conn net.Conn) {
	defer func() {
		conn.Close()
		r.connected.Store(false)
		log.Printf("[audio-receiver] worker disconnected")
	}()

	frames := 0
	for {
		var fd FrequencyData
		if err := DecodeFrame(conn, &fd); err != nil {
			select {
			case <-r.done:
				return
			default:
				log.Printf("[audio-receiver] decode error after %d frames: %v", frames, err)
				return
			}
		}
		r.latest.Store(&fd)
		r.lastUpdate.Store(time.Now().UnixNano())
		frames++
		if frames == 1 {
			log.Printf("[audio-receiver] first frame received: bass=%.2f mid=%.2f high=%.2f",
				fd.Bass, fd.Mid, fd.High)
		} else if frames%500 == 0 {
			log.Printf("[audio-receiver] %d frames received", frames)
		}
	}
}

// Stop closes the listener and cleans up the socket file. Safe to call multiple times.
func (r *Receiver) Stop() {
	r.stopOnce.Do(func() {
		close(r.done)
		if r.listener != nil {
			r.listener.Close()
		}
		if runtime.GOOS != "windows" {
			_ = os.Remove(r.socketPath)
		}
		log.Printf("[audio-receiver] stopped")
	})
}

// Latest returns the most recent FrequencyData, or nil if no fresh data.
// Returns nil if the last frame is older than 150ms (e.g., paused).
// Thread-safe; called from the Bubble Tea goroutine.
func (r *Receiver) Latest() *FrequencyData {
	last := r.lastUpdate.Load()
	if last == 0 || time.Since(time.Unix(0, last)) > 150*time.Millisecond {
		return nil
	}
	return r.latest.Load()
}

// SocketPath returns the path for the audio worker to connect to.
func (r *Receiver) SocketPath() string {
	return r.socketPath
}

// Connected returns true if an audio worker is actively sending data.
func (r *Receiver) Connected() bool {
	return r.connected.Load()
}
