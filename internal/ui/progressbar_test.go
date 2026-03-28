package ui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestRenderMiniBar(t *testing.T) {
	tests := []struct {
		name       string
		barWidth   int
		progressMs int
		durationMs int
	}{
		{"zero progress", 20, 0, 200000},
		{"half progress", 20, 100000, 200000},
		{"full progress", 20, 200000, 200000},
		{"progress exceeds duration", 20, 250000, 200000},
		{"zero duration", 20, 0, 0},
		{"minimal bar width", 4, 50000, 200000},
		{"one ms progress", 20, 1, 200000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := renderMiniBar(tt.barWidth, tt.progressMs, tt.durationMs)
			width := lipgloss.Width(result)
			if width != tt.barWidth {
				t.Errorf("visual width = %d, want %d", width, tt.barWidth)
			}
		})
	}
}
