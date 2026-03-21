package audio

import (
	"sync"

	"github.com/gordonklaus/portaudio"
)

const (
	sampleRate = 44100
	bufferSize = 1024
	// Ring buffer holds ~10 seconds of audio.
	ringSize = sampleRate * 10
)

type Capture struct {
	mu       sync.RWMutex
	ring     []float32
	writePos int
	stream   *portaudio.Stream
	running  bool
}

func NewCapture() *Capture {
	return &Capture{
		ring: make([]float32, ringSize),
	}
}

func (c *Capture) Start() error {
	if err := portaudio.Initialize(); err != nil {
		return err
	}
	inputBuf := make([]float32, bufferSize)
	stream, err := portaudio.OpenDefaultStream(1, 0, sampleRate, bufferSize, inputBuf)
	if err != nil {
		portaudio.Terminate()
		return err
	}
	if err := stream.Start(); err != nil {
		stream.Close()
		portaudio.Terminate()
		return err
	}
	c.stream = stream
	c.running = true

	go c.readLoop(inputBuf)
	return nil
}

func (c *Capture) readLoop(buf []float32) {
	for {
		c.mu.RLock()
		running := c.running
		c.mu.RUnlock()
		if !running {
			return
		}
		if err := c.stream.Read(); err != nil {
			continue
		}
		c.mu.Lock()
		for _, s := range buf {
			c.ring[c.writePos] = s
			c.writePos = (c.writePos + 1) % ringSize
		}
		c.mu.Unlock()
	}
}

func (c *Capture) Stop() {
	c.mu.Lock()
	c.running = false
	c.mu.Unlock()
	if c.stream != nil {
		c.stream.Stop()
		c.stream.Close()
	}
	portaudio.Terminate()
}

// Snapshot returns the last n samples from the ring buffer.
func (c *Capture) Snapshot(n int) []float32 {
	if n > ringSize {
		n = ringSize
	}
	out := make([]float32, n)
	c.mu.RLock()
	defer c.mu.RUnlock()
	start := (c.writePos - n + ringSize) % ringSize
	for i := 0; i < n; i++ {
		out[i] = c.ring[(start+i)%ringSize]
	}
	return out
}

func (c *Capture) Running() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.running
}
