package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// The page's last background, which the window opens with next time: before
// the page draws, a dark page's window showed white (2026-10-16).
const windowBackgroundFile = "window-background"

// The window shows its own color around the page — on iOS behind the status
// bar and the home indicator, which the page stays clear of — so it takes the
// page's background whenever the page's theme changes, and the status bar's
// text stays readable on it.

// Appearance takes the page's colors for the window.
type Appearance struct{}

func newAppearance() *Appearance {
	return &Appearance{}
}

// Set takes the page's background, a CSS rgb(), rgba() or #rrggbb color, and
// whether the page is dark.
func (*Appearance) Set(background string, dark bool) error {
	colour, ok := parseCSSColour(background)
	if !ok {
		return errors.New("not a color")
	}
	if app := application.Get(); app != nil {
		for _, window := range app.Window.GetAll() {
			window.SetBackgroundColour(colour)
		}
	}
	setStatusBarText(dark)
	if dir, err := localDataDir(); err == nil {
		_ = os.WriteFile(filepath.Join(dir, windowBackgroundFile), []byte(fmt.Sprintf("#%02x%02x%02x", colour.Red, colour.Green, colour.Blue)), 0o600)
	}
	return nil
}

// lastWindowBackground is the page's background when the app last ran, or
// none before the page first said it.
func lastWindowBackground() application.RGBA {
	dir, err := localDataDir()
	if err != nil {
		return application.RGBA{}
	}
	saved, err := os.ReadFile(filepath.Join(dir, windowBackgroundFile))
	if err != nil {
		return application.RGBA{}
	}
	colour, _ := parseCSSColour(string(saved))
	return colour
}

func parseCSSColour(value string) (application.RGBA, bool) {
	value = strings.TrimSpace(strings.ToLower(value))
	if strings.HasPrefix(value, "#") && len(value) == 7 {
		number, err := strconv.ParseUint(value[1:], 16, 32)
		if err != nil {
			return application.RGBA{}, false
		}
		return application.RGBA{Red: uint8(number >> 16), Green: uint8(number >> 8), Blue: uint8(number), Alpha: 255}, true
	}
	for _, prefix := range []string{"rgba(", "rgb("} {
		if !strings.HasPrefix(value, prefix) || !strings.HasSuffix(value, ")") {
			continue
		}
		parts := strings.Split(strings.TrimSuffix(strings.TrimPrefix(value, prefix), ")"), ",")
		if len(parts) != 3 && len(parts) != 4 {
			return application.RGBA{}, false
		}
		channels := [3]uint8{}
		for index := 0; index < 3; index++ {
			number, err := strconv.Atoi(strings.TrimSpace(parts[index]))
			if err != nil || number < 0 || number > 255 {
				return application.RGBA{}, false
			}
			channels[index] = uint8(number)
		}
		// A see-through background isn't one to paint the window with.
		if len(parts) == 4 && strings.TrimSpace(parts[3]) != "1" {
			return application.RGBA{}, false
		}
		return application.RGBA{Red: channels[0], Green: channels[1], Blue: channels[2], Alpha: 255}, true
	}
	return application.RGBA{}, false
}
