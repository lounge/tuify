package ui

import (
	"context"
	"log"
	"strings"

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
	devices         []spotify.Device
	activeDeviceID  string
	cursor          int
	loading         bool
	err             error
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
	// Track the active device and filter it out of the list.
	d.activeDeviceID = ""
	filtered := make([]spotify.Device, 0, len(msg.devices))
	for _, dev := range msg.devices {
		if dev.Active {
			d.activeDeviceID = dev.ID
		} else {
			filtered = append(filtered, dev)
		}
	}
	d.devices = filtered
}

func (d *deviceSelectorModel) up() {
	if len(d.devices) > 0 && d.cursor > 0 {
		d.cursor--
	}
}

func (d *deviceSelectorModel) down() {
	if len(d.devices) > 0 && d.cursor < len(d.devices)-1 {
		d.cursor++
	}
}

func (d *deviceSelectorModel) selected() (spotify.Device, bool) {
	if len(d.devices) == 0 {
		return spotify.Device{}, false
	}
	return d.devices[d.cursor], true
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
		// Find the longest device name for column alignment.
		maxName := 0
		for _, dev := range d.devices {
			if len(dev.Name) > maxName {
				maxName = len(dev.Name)
			}
		}
		var lines []string
		for i, dev := range d.devices {
			nameStyle := lipgloss.NewStyle().Foreground(colorText)
			typeStyle := lipgloss.NewStyle().Foreground(colorMuted)
			if i == d.cursor {
				nameStyle = nameStyle.Foreground(colorPrimary).Bold(true)
			}
			name := nameStyle.Render(dev.Name)
			pad := strings.Repeat(" ", maxName-len(dev.Name)+2)
			typ := typeStyle.Render(dev.Type)
			lines = append(lines, name+pad+typ)
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
		devices, err := client.GetDevices(context.Background())
		return devicesLoadedMsg{devices: devices, err: err}
	}
}

func transferDeviceCmd(client *spotify.Client, dev spotify.Device, currentDeviceID string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
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
