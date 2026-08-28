//go:build darwin && cgo

package main

import (
	_ "embed"

	"github.com/energye/systray"
)

//go:embed build/tray/icon-32.png
var trayIcon []byte

// setupTray wires the menu-bar icon. The returned start/end funcs hook into
// Wails' OnStartup/OnShutdown so the tray shares the NSApplication run loop.
func (a *App) setupTray() (start func(), end func()) {
	return systray.RunWithExternalLoop(a.trayReady, func() {})
}

func (a *App) trayReady() {
	systray.SetIcon(trayIcon)
	systray.SetTooltip("DevBox")
	a.buildTrayMenu()
	systray.SetOnClick(func(menu systray.IMenu) { menu.ShowMenu() })
}
