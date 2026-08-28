package main

import (
	"embed"
	"os"

	"DevBox/internal/config"
	"DevBox/internal/platform"
)

//go:embed all:frontend/dist
var assets embed.FS

// startHidden decides whether the window opens hidden in the tray: always when
// the OS launched DevBox at login (--minimized), or when the user asked for it.
func startHidden() bool {
	for _, arg := range os.Args[1:] {
		if arg == platform.AutoStartFlag {
			return true
		}
	}
	cfg, err := config.Load()
	return err == nil && cfg != nil && cfg.StartMinimized
}
