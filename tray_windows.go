//go:build windows

package main

import (
	_ "embed"
	goruntime "runtime"

	"github.com/energye/systray"
)

//go:embed build/windows/icon.ico
var trayIcon []byte

// setupTray wires the notification-area icon.
//
// The tray gets its own OS thread: Win32 delivers a window's messages only to
// the thread that created it, so the hidden tray window and its GetMessage
// loop must live together. systray.Run does exactly that when called from a
// goroutine pinned with LockOSThread (RunWithExternalLoop would create the
// window on Wails' main thread and pump messages elsewhere — clicks never
// arrive). Wails runtime calls (WindowShow, Quit) are thread-safe.
func (a *App) setupTray() (start func(), end func()) {
	start = func() {
		go func() {
			goruntime.LockOSThread()
			defer goruntime.UnlockOSThread()
			systray.Run(a.trayReady, func() {})
		}()
	}
	end = func() { systray.Quit() }
	return start, end
}

func (a *App) trayReady() {
	systray.SetIcon(trayIcon)
	systray.SetTooltip("DevBox")
	a.buildTrayMenu()
	systray.SetOnClick(func(menu systray.IMenu) { a.showWindow() })
	systray.SetOnDClick(func(menu systray.IMenu) { a.showWindow() })
	systray.SetOnRClick(func(menu systray.IMenu) { menu.ShowMenu() })
}
