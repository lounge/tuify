package ui

import (
	"context"
	"log"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/lounge/tuify/internal/spotify"
)

// Messages

type devicesLoadedMsg struct {
	devices []spotify.Device
	err     error
}

type transferDeviceMsg struct {
	err        error
	deviceName string
}

// Model

type deviceSelectorModel struct {
	devices          []spotify.Device
	activeDeviceID   string
	cursor           int
	loading          bool
	transferring     bool
	transferTarget   string // device name we're switching to
	transferDeadline time.Time
	err              error
}

func (d *deviceSelectorModel) open() {
	d.loading = true
	d.err = nil
	d.cursor = 0
	d.devices = nil
	d.activeDeviceID = ""
}

func (d *deviceSelectorModel) handleLoaded(msg devicesLoadedMsg) {
	d.loading = false
	if msg.err != nil {
		d.err = msg.err
		return
	}
	d.activeDeviceID = ""
	for _, dev := range msg.devices {
		if dev.Active {
			d.activeDeviceID = dev.ID
			break
		}
	}
	d.devices = msg.devices
	// Place cursor on the first non-active device.
	d.cursor = 0
	for i, dev := range d.devices {
		if dev.ID != d.activeDeviceID {
			d.cursor = i
			break
		}
	}
}

func (d *deviceSelectorModel) up() {
	for d.cursor > 0 {
		d.cursor--
		if d.devices[d.cursor].ID != d.activeDeviceID {
			return
		}
	}
}

func (d *deviceSelectorModel) down() {
	for d.cursor < len(d.devices)-1 {
		d.cursor++
		if d.devices[d.cursor].ID != d.activeDeviceID {
			return
		}
	}
}

func (d *deviceSelectorModel) selected() (spotify.Device, bool) {
	if len(d.devices) == 0 {
		return spotify.Device{}, false
	}
	dev := d.devices[d.cursor]
	if dev.ID == d.activeDeviceID {
		return spotify.Device{}, false
	}
	return dev, true
}

// View

var deviceOverlayStyle = overlayBoxStyle.Padding(1, 3)

func (d *deviceSelectorModel) view(width, height int) string {
	var body string
	if d.loading {
		body = loadingStyle.Render("Loading devices…")
	} else if d.err != nil {
		body = errorStyle.Render(d.err.Error())
	} else if len(d.devices) == 0 {
		body = loadingStyle.Render("No devices found")
	} else {
		// Find the longest display label for column alignment.
		maxLabel := 0
		for _, dev := range d.devices {
			n := lipgloss.Width(dev.Name)
			if n > maxLabel {
				maxLabel = n
			}
		}
		var lines []string
		for i, dev := range d.devices {
			nameStyle := lipgloss.NewStyle().Foreground(colorText)
			typeStyle := lipgloss.NewStyle().Foreground(colorMuted)
			if dev.ID == d.activeDeviceID {
				nameStyle = nameStyle.Foreground(colorMuted)
			} else if i == d.cursor {
				nameStyle = nameStyle.Foreground(colorPrimary).Bold(true)
			}
			var icon string
			if dev.ID == d.activeDeviceID {
				icon = lipgloss.NewStyle().Foreground(colorSecondary).Render("◉") + " "
			} else {
				icon = "  "
			}
			name := nameStyle.Render(dev.Name)
			pad := strings.Repeat(" ", maxLabel-lipgloss.Width(dev.Name)+2)
			typ := typeStyle.Render(strings.ToLower(dev.Type))
			lines = append(lines, icon+name+pad+typ)
		}
		body = strings.Join(lines, "\n")
	}

	title := lipgloss.NewStyle().Foreground(colorText).Bold(true).Render("Select Device")
	content := title + "\n\n" + body
	box := deviceOverlayStyle.Render(content)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

// Commands

func fetchDevicesCmd(client *spotify.Client) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		devices, err := client.GetDevices(ctx)
		return devicesLoadedMsg{devices: devices, err: err}
	}
}

func transferDeviceCmd(client *spotify.Client, dev spotify.Device, currentDeviceID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		// Pause librespot before transferring away so it stops local audio.
		// Only needed for the preferred device; other devices stop via Connect.
		if currentDeviceID != "" && dev.Name != client.PreferredDevice {
			if err := client.Pause(ctx, currentDeviceID); err != nil {
				log.Printf("[device] pre-transfer pause failed: %v", err)
			}
		}
		err := client.TransferPlayback(ctx, dev.ID, true)
		return transferDeviceMsg{err: err, deviceName: dev.Name}
	}
}
