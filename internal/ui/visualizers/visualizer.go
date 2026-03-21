package visualizers

import "github.com/lounge/tuify/internal/audio"

type Visualizer interface {
	View(capture *audio.Capture, width, height int) string
}
